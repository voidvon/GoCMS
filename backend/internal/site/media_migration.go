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
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const mediaMigrationRoute = "/api/admin/media-storage/migrate"
const imageMigrationBatchSize = 10

const localImagesForMigration = ` WHERE site_id IN (?, ?) AND kind='image' AND status='active'
	AND storage_provider='local' AND storage_path GLOB ? AND public_path='/assets/' || storage_path`

type imageMigrationPreview struct {
	Count     int   `json:"count"`
	Bytes     int64 `json:"bytes"`
	ThroughID int64 `json:"through_id"`
	Ready     bool  `json:"ready"`
}

type imageMigrationFailure struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Error string `json:"error"`
}

type imageMigrationBatch struct {
	Migrated int                     `json:"migrated"`
	Failures []imageMigrationFailure `json:"failures"`
	AfterID  int64                   `json:"after_id"`
	More     bool                    `json:"more"`
}

func (s *Server) adminMediaMigration(w http.ResponseWriter, r *http.Request) {
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
	legacySiteID := siteID
	if siteID == 1 {
		legacySiteID = 0
	}
	paths := strconv.FormatInt(siteID, 10) + "/uploads/*"
	ctx := r.Context()
	if r.Method == http.MethodPost {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 35*time.Second)
		defer cancel()
		s.mediaStorageMu.Lock()
		defer s.mediaStorageMu.Unlock()
	}

	config, err := s.loadMediaStorage(ctx)
	if err != nil {
		http.Error(w, "读取 R2 配置失败", http.StatusInternalServerError)
		return
	}
	ready := config.Bucket != "" && config.Endpoint != "" && config.HasSecret
	if r.Method == http.MethodGet {
		result := imageMigrationPreview{Ready: ready}
		err := s.database.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(size_bytes), 0), COALESCE(MAX(id), 0)
			FROM gocms_media`+localImagesForMigration, siteID, legacySiteID, paths).Scan(&result.Count, &result.Bytes, &result.ThroughID)
		if err != nil {
			http.Error(w, "统计待迁移图片失败", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	if !ready {
		http.Error(w, "请先在文件存储中配置 R2", http.StatusConflict)
		return
	}
	var input struct {
		AfterID   int64 `json:"after_id"`
		ThroughID int64 `json:"through_id"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if err := decodeRequest(r, &input); err != nil || input.AfterID < 0 || input.ThroughID <= 0 || input.AfterID > input.ThroughID {
		http.Error(w, "迁移进度参数无效", http.StatusBadRequest)
		return
	}
	client, err := s.r2Client(config)
	if err != nil {
		http.Error(w, "R2 凭据不可用", http.StatusServiceUnavailable)
		return
	}
	assets, err := s.localImagesForMigration(ctx, siteID, legacySiteID, paths, input.AfterID, input.ThroughID)
	if err != nil {
		http.Error(w, "读取待迁移图片失败", http.StatusInternalServerError)
		return
	}
	result := imageMigrationBatch{AfterID: input.AfterID, Failures: []imageMigrationFailure{}}
	for _, asset := range assets {
		if result.Migrated+len(result.Failures) == imageMigrationBatchSize || ctx.Err() != nil {
			result.More = true
			break
		}
		err := s.migrateLocalImage(ctx, client, config.Bucket, siteID, asset)
		if ctx.Err() != nil {
			result.More = true
			break
		}
		if err != nil {
			result.Failures = append(result.Failures, imageMigrationFailure{ID: asset.ID, Name: asset.OriginalName, Error: err.Error()})
		} else {
			result.Migrated++
		}
		result.AfterID = asset.ID
	}
	if result.More && result.AfterID == input.AfterID {
		http.Error(w, "R2 本批迁移超时，请稍后重试", http.StatusGatewayTimeout)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) localImagesForMigration(ctx context.Context, siteID, legacySiteID int64, paths string, afterID, throughID int64) ([]MediaAsset, error) {
	rows, err := s.database.QueryContext(ctx, mediaColumns+localImagesForMigration+
		` AND id>? AND id<=? ORDER BY id LIMIT ?`, siteID, legacySiteID, paths, afterID, throughID, imageMigrationBatchSize+1)
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

func (s *Server) migrateLocalImage(ctx context.Context, client *s3.Client, bucket string, siteID int64, asset MediaAsset) error {
	rel := localUploadImagePath(asset.URL, siteID)
	if rel == "" || rel != asset.storagePath {
		return errors.New("图片路径无效")
	}
	ext := strings.ToLower(path.Ext(rel))
	format := strings.TrimPrefix(ext, ".")
	if format == "jpg" {
		format = "jpeg"
	}
	mimeType, extension, valid := mediaImageFormat(format)
	if !valid || (asset.MimeType != "" && mimeType != asset.MimeType) {
		return errors.New("图片格式无效")
	}
	file, err := os.OpenInRoot(s.assetsRoot, filepath.FromSlash(rel))
	if err != nil {
		return errors.New("本地图片无法读取")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxMediaUploadSize {
		return errors.New("本地图片不是可迁移的文件")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxMediaUploadSize+1))
	if err != nil || len(data) == 0 || int64(len(data)) != info.Size() {
		return errors.New("读取本地图片失败")
	}
	digest := sha256.Sum256(data)
	digestHex := hex.EncodeToString(digest[:])
	if (asset.SHA256 != "" && !strings.EqualFold(asset.SHA256, digestHex)) ||
		(asset.SizeBytes > 0 && asset.SizeBytes != int64(len(data))) {
		return errors.New("本地图片与附件记录不一致")
	}
	// A stable key lets retries verify an existing copy without replacing it.
	key := fmt.Sprintf("sites/%d/image/migrated/%d-%s%s", siteID, asset.ID, digestHex, extension)
	objectBucket, objectKey := aws.String(bucket), aws.String(key)
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: objectBucket, Key: objectKey, Body: bytes.NewReader(data),
		ContentType: aws.String(mimeType), ContentLength: aws.Int64(int64(len(data))),
		ContentDisposition: aws.String(mediaDisposition(asset)), IfNoneMatch: aws.String("*"),
	})
	if err != nil && !hasS3Status(err, http.StatusPreconditionFailed) {
		return errors.New("上传 R2 失败")
	}
	if err := verifyR2Image(ctx, client, bucket, key, int64(len(data)), digest[:]); err != nil {
		return err
	}
	result, err := s.database.ExecContext(ctx, `UPDATE gocms_media SET storage_provider='r2', shared_storage=1,
		storage_path=?, sha256=?, size_bytes=?, mime_type=?
		WHERE id=? AND storage_provider='local' AND status='active' AND storage_path=?`,
		key, digestHex, len(data), mimeType, asset.ID, rel)
	if err != nil {
		return errors.New("切换图片存储失败")
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return errors.New("附件已变更，请刷新后重试")
	}
	return nil
}

func verifyR2Image(ctx context.Context, client *s3.Client, bucket, key string, size int64, digest []byte) error {
	object, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		return errors.New("读取 R2 副本失败")
	}
	hasher := sha256.New()
	readSize, copyErr := io.Copy(hasher, io.LimitReader(object.Body, size+1))
	closeErr := object.Body.Close()
	if copyErr != nil || closeErr != nil || readSize != size || !bytes.Equal(hasher.Sum(nil), digest) {
		return errors.New("R2 副本校验失败")
	}
	return nil
}

func localUploadImagePath(publicPath string, siteID int64) string {
	rel, _ := strings.CutPrefix(publicPath, "/assets/")
	prefix := strconv.FormatInt(siteID, 10) + "/uploads/"
	if len(rel) <= len(prefix) || !strings.HasPrefix(rel, prefix) || path.Clean(rel) != rel ||
		strings.ContainsAny(rel, "\\\x00") {
		return ""
	}
	return rel
}

func removeLocalUpload(root, rel string) error {
	files, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer files.Close()
	err = files.Remove(filepath.FromSlash(rel))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Match the stored canonical path even when the request used an uploads alias.
func (s *Server) serveMigratedImage(w http.ResponseWriter, r *http.Request, publicPath string) bool {
	if s.database == nil {
		return false
	}
	rel, ok := strings.CutPrefix(publicPath, "/assets/")
	if !ok {
		return false
	}
	id, _, _ := strings.Cut(rel, "/")
	siteID, err := strconv.ParseInt(id, 10, 64)
	if err != nil || siteID < 1 || localUploadImagePath(publicPath, siteID) == "" {
		return false
	}
	asset, err := scanMedia(s.database.QueryRowContext(r.Context(), mediaColumns+
		` WHERE public_path=? AND kind='image' AND storage_provider='r2' AND shared_storage=1`, publicPath))
	if err != nil {
		return false
	}
	if asset.Status != "active" {
		w.Header().Set("Cache-Control", "no-store")
		http.NotFound(w, r)
		return true
	}
	return s.redirectR2Media(w, r, asset)
}
