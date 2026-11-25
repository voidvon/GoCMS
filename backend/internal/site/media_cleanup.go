package site

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const mediaCleanupRoute = "/api/admin/media-storage/cleanup"

const (
	imageCleanupBatchSize = 50
	imageCleanupWorkers   = 8
)

const migratedImagesForCleanup = ` WHERE site_id IN (?, ?) AND kind='image' AND status='active'
	AND storage_provider='r2' AND shared_storage=1 AND storage_path GLOB ? AND public_path GLOB ?
	AND id>? AND id<=? ORDER BY id LIMIT ?`

type imageCleanupBatch struct {
	Removed  int                     `json:"removed"`
	Bytes    int64                   `json:"bytes"`
	Failures []imageMigrationFailure `json:"failures"`
	AfterID  int64                   `json:"after_id"`
	More     bool                    `json:"more"`
}

func (s *Server) adminMediaCleanup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		methodNotAllowed(w)
		return
	}
	if s.database == nil || s.assetsRoot == "" {
		http.Error(w, "附件存储不可用", http.StatusServiceUnavailable)
		return
	}
	siteID, err := s.resolveSiteID(r, s.currentAdmin(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if r.Method == http.MethodGet {
		s.previewMediaCleanup(w, r, siteID)
		return
	}

	var input struct {
		AfterID   int64 `json:"after_id"`
		ThroughID int64 `json:"through_id"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if err := decodeRequest(r, &input); err != nil || input.AfterID < 0 || input.ThroughID <= input.AfterID {
		http.Error(w, "清理进度参数无效", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	s.mediaStorageMu.Lock()
	defer s.mediaStorageMu.Unlock()

	config, err := s.loadMediaStorage(ctx)
	if err != nil {
		http.Error(w, "读取 R2 配置失败", http.StatusInternalServerError)
		return
	}
	if !mediaCleanupReady(config) {
		http.Error(w, "请先在文件存储中配置 R2", http.StatusConflict)
		return
	}
	client, err := s.r2Client(config)
	if err != nil {
		http.Error(w, "R2 凭据不可用", http.StatusServiceUnavailable)
		return
	}
	assets, err := s.selectMigratedImages(ctx, siteID, input.AfterID, input.ThroughID, imageCleanupBatchSize+1)
	if err != nil {
		http.Error(w, "读取待清理图片失败", http.StatusInternalServerError)
		return
	}
	result := imageCleanupBatch{AfterID: input.AfterID, Failures: []imageMigrationFailure{}}
	if len(assets) > imageCleanupBatchSize {
		result.More = true
		assets = assets[:imageCleanupBatchSize]
	}
	if len(assets) == 0 {
		writeJSON(w, http.StatusOK, result)
		return
	}

	root, err := os.OpenRoot(s.assetsRoot)
	if err != nil {
		http.Error(w, "无法打开本地附件目录", http.StatusInternalServerError)
		return
	}
	defer root.Close()

	type itemResult struct {
		bytes int64
		err   error
	}
	outcomes := make([]itemResult, len(assets))
	work := make(chan int)
	var workers sync.WaitGroup
	for range min(imageCleanupWorkers, len(assets)) {
		workers.Go(func() {
			for i := range work {
				outcomes[i].bytes, outcomes[i].err = cleanMigratedImage(ctx, root, client, config.Bucket, siteID, assets[i])
			}
		})
	}
	for i := range assets {
		work <- i
	}
	close(work)
	workers.Wait()

	for i, resultItem := range outcomes {
		asset := assets[i]
		result.AfterID = asset.ID
		if resultItem.err != nil {
			result.Failures = append(result.Failures, imageMigrationFailure{ID: asset.ID, Name: asset.OriginalName, Error: resultItem.err.Error()})
		} else if resultItem.bytes > 0 {
			result.Removed++
			result.Bytes += resultItem.bytes
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func mediaCleanupReady(config mediaStorageSettings) bool {
	return config.Endpoint != "" && config.Bucket != "" && config.HasSecret
}

func (s *Server) previewMediaCleanup(w http.ResponseWriter, r *http.Request, siteID int64) {
	ctx := r.Context()
	config, err := s.loadMediaStorage(ctx)
	if err != nil {
		http.Error(w, "读取 R2 配置失败", http.StatusInternalServerError)
		return
	}
	assets, err := s.selectMigratedImages(ctx, siteID, 0, 1<<63-1, -1)
	if err != nil {
		http.Error(w, "读取待清理图片失败", http.StatusInternalServerError)
		return
	}
	result := imageMigrationPreview{Ready: mediaCleanupReady(config)}
	if len(assets) == 0 {
		writeJSON(w, http.StatusOK, result)
		return
	}
	root, err := os.OpenRoot(s.assetsRoot)
	if err != nil {
		http.Error(w, "无法打开本地附件目录", http.StatusInternalServerError)
		return
	}
	defer root.Close()
	for _, asset := range assets {
		rel := localUploadImagePath(asset.URL, siteID)
		if rel == "" {
			continue
		}
		info, err := root.Lstat(filepath.FromSlash(rel))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			http.Error(w, "读取本地图片副本失败", http.StatusInternalServerError)
			return
		}
		result.Count++
		result.Bytes += info.Size()
		result.ThroughID = asset.ID
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) selectMigratedImages(ctx context.Context, siteID, afterID, throughID int64, limit int) ([]MediaAsset, error) {
	legacySiteID := siteID
	if siteID == 1 {
		legacySiteID = 0
	}
	site := strconv.FormatInt(siteID, 10)
	rows, err := s.database.QueryContext(ctx, mediaColumns+migratedImagesForCleanup,
		siteID, legacySiteID, "sites/"+site+"/image/migrated/*", "/assets/"+site+"/uploads/*", afterID, throughID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var assets []MediaAsset
	for rows.Next() {
		asset, err := scanMedia(rows)
		if err != nil {
			return nil, err
		}
		assets = append(assets, asset)
	}
	return assets, rows.Err()
}

func cleanMigratedImage(ctx context.Context, root *os.Root, client *s3.Client, bucket string, siteID int64, asset MediaAsset) (int64, error) {
	rel := localUploadImagePath(asset.URL, siteID)
	if rel == "" {
		return 0, errors.New("原图片路径无效")
	}
	rel = filepath.FromSlash(rel)
	before, err := root.Lstat(rel)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > maxMediaUploadSize ||
		before.Size() != asset.SizeBytes {
		return 0, errors.New("本地副本与附件记录不一致")
	}

	digest, err := hex.DecodeString(asset.SHA256)
	if err != nil || len(digest) != sha256.Size || !verifiedMigrationKey(siteID, asset) {
		return 0, errors.New("迁移对象记录无效")
	}
	if err := compareLocalImage(root, rel, before, digest); err != nil {
		return 0, err
	}
	if err := verifyR2Image(ctx, client, bucket, asset.storagePath, asset.SizeBytes, digest); err != nil {
		return 0, err
	}
	if ctx.Err() != nil {
		return 0, errors.New("清理超时，保留本地副本")
	}
	after, err := root.Lstat(rel)
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || before.ModTime() != after.ModTime() {
		return 0, errors.New("本地副本在校验期间发生变化")
	}
	if err := root.Remove(rel); err != nil {
		return 0, errors.New("移除本地副本失败")
	}
	return asset.SizeBytes, nil
}

func verifiedMigrationKey(siteID int64, asset MediaAsset) bool {
	key := fmt.Sprintf("sites/%d/image/migrated/%d-%s", siteID, asset.ID, strings.ToLower(asset.SHA256))
	extension := path.Ext(asset.storagePath)
	switch extension {
	case ".jpg", ".png", ".gif", ".webp", ".heic":
		return asset.storagePath == key+extension
	default:
		return false
	}
}

func compareLocalImage(root *os.Root, name string, expected os.FileInfo, digest []byte) error {
	// Refuse symlink components even when they resolve within the assets root.
	for parent := filepath.Dir(name); parent != "."; parent = filepath.Dir(parent) {
		info, err := root.Lstat(parent)
		if err != nil || !info.IsDir() {
			return errors.New("本地副本目录无效")
		}
	}
	file, err := root.Open(name)
	if err != nil {
		return errors.New("读取本地副本失败")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !os.SameFile(expected, info) {
		return errors.New("本地副本在校验期间发生变化")
	}
	hasher := sha256.New()
	size, err := io.Copy(hasher, io.LimitReader(file, expected.Size()+1))
	if err != nil || size != expected.Size() || !bytes.Equal(hasher.Sum(nil), digest) {
		return errors.New("本地副本内容已变更")
	}
	return nil
}
