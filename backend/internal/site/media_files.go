package site

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"golang.org/x/text/language"
)

const (
	mediaFilePrefix = "/api/files/media/"
	maxPDFSize      = 64 << 20
)

func (s *Server) uploadPDF(w http.ResponseWriter, r *http.Request) {
	if s.database == nil {
		http.Error(w, "附件存储不可用", http.StatusServiceUnavailable)
		return
	}
	tag, err := language.Parse(strings.TrimSpace(r.URL.Query().Get("language")))
	if err != nil || tag == language.Und || len(tag.String()) > 64 {
		http.Error(w, "请选择有效的文档语言，例如 de-DE", http.StatusBadRequest)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPDFSize+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "PDF 不能超过 64 MiB", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "请选择 PDF 文件", http.StatusBadRequest)
		}
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "请选择 PDF 文件", http.StatusBadRequest)
		return
	}
	defer file.Close()
	if header.Size > maxPDFSize {
		http.Error(w, "PDF 不能超过 64 MiB", http.StatusRequestEntityTooLarge)
		return
	}
	magic := make([]byte, 8)
	if _, err := io.ReadFull(file, magic); err != nil || http.DetectContentType(magic) != "application/pdf" {
		http.Error(w, "文件内容不是有效的 PDF 格式", http.StatusBadRequest)
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		http.Error(w, "读取 PDF 失败", http.StatusBadRequest)
		return
	}
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		http.Error(w, "读取 PDF 失败", http.StatusBadRequest)
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		http.Error(w, "读取 PDF 失败", http.StatusBadRequest)
		return
	}
	siteID, err := s.resolveSiteID(r, s.currentAdmin(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	asset := MediaAsset{
		SiteID: siteID, Kind: "pdf", Language: tag.String(), MimeType: "application/pdf",
		OriginalName: mediaOriginalName(header.Filename), SizeBytes: size, SHA256: hex.EncodeToString(hash.Sum(nil)),
	}

	s.mediaStorageMu.Lock()
	defer s.mediaStorageMu.Unlock()
	existing, err := scanMedia(s.database.QueryRowContext(r.Context(), mediaColumns+
		` WHERE site_id=? AND kind='pdf' AND sha256=? AND status='active'`, siteID, asset.SHA256))
	if err == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "asset": existing, "reused": true})
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "查找已有 PDF 失败", http.StatusInternalServerError)
		return
	}
	config, err := s.loadMediaStorage(r.Context())
	if err != nil {
		http.Error(w, "读取文件存储配置失败", http.StatusInternalServerError)
		return
	}
	s.saveMediaFile(w, r, asset, ".pdf", file, config, config.Enabled)
}

func mediaOriginalName(name string) string {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" {
		return ""
	}
	return name
}

func mediaDisposition(asset MediaAsset) string {
	if asset.OriginalName == "" {
		return "inline"
	}
	return mime.FormatMediaType("inline", map[string]string{"filename": asset.OriginalName})
}

// saveMediaFile runs while mediaStorageMu is held, keeping configuration and
// writes in sync. Each attempt owns a new key which it can safely clean up.
func (s *Server) saveMediaFile(w http.ResponseWriter, r *http.Request, asset MediaAsset, extension string, body io.ReadSeeker, config mediaStorageSettings, remote bool) {
	ctx := r.Context()
	now := time.Now().UTC()
	name := strings.ToLower(rand.Text()) + extension
	site := strconv.FormatInt(asset.SiteID, 10)
	asset.URL = mediaFilePrefix + site + "/" + name
	asset.Status = "active"
	asset.CreatedAt = now.Format(time.RFC3339)
	asset.UploadedBy, _ = s.adminUsername(r)

	var remove func()
	if remote {
		asset.Storage = "r2"
		asset.sharedStorage = true
		asset.storagePath = path.Join("sites", site, "image", now.Format("2006/01"), name)
		if asset.Kind == "pdf" {
			asset.storagePath = path.Join("sites", site, "pdf", asset.Language, name)
		}
		client, err := s.r2Client(config)
		if err != nil {
			http.Error(w, "R2 凭据不可用", http.StatusServiceUnavailable)
			return
		}
		bucket, key := aws.String(config.Bucket), aws.String(asset.storagePath)
		remove = func() {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			_, _ = client.DeleteObject(cleanup, &s3.DeleteObjectInput{Bucket: bucket, Key: key})
		}
		if _, err := client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: bucket, Key: key, Body: body, ContentType: aws.String(asset.MimeType),
			ContentLength: aws.Int64(asset.SizeBytes), ContentDisposition: aws.String(mediaDisposition(asset)),
		}); err != nil {
			remove()
			http.Error(w, "上传到 R2 失败", http.StatusBadGateway)
			return
		}
	} else {
		asset.Storage = "local"
		asset.storagePath = path.Join(site, "uploads", "pdf", asset.Language, name)
		diskPath, err := s.mediaDiskPath("/assets/" + asset.storagePath)
		if err != nil || writeLocalMedia(diskPath, body) != nil {
			http.Error(w, "保存 PDF 失败", http.StatusInternalServerError)
			return
		}
		remove = func() { _ = os.Remove(diskPath) }
	}
	row := s.database.QueryRowContext(ctx, `INSERT INTO gocms_media
		(site_id, kind, storage_provider, shared_storage, language, storage_path, public_path, original_name, mime_type,
		 size_bytes, width, height, sha256, status, uploaded_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		asset.SiteID, asset.Kind, asset.Storage, asset.sharedStorage, asset.Language, asset.storagePath, asset.URL, asset.OriginalName,
		asset.MimeType, asset.SizeBytes, asset.Width, asset.Height, asset.SHA256, asset.Status, asset.UploadedBy, asset.CreatedAt)
	if err := row.Scan(&asset.ID); err != nil {
		// Check independently of the request in case an interrupted response
		// arrived after SQLite committed the insert.
		check, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if checkErr := s.database.QueryRowContext(check, `SELECT id FROM gocms_media WHERE public_path=?`, asset.URL).Scan(&asset.ID); checkErr != nil {
			if errors.Is(checkErr, sql.ErrNoRows) {
				remove()
			}
			http.Error(w, "保存附件记录失败", http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "asset": asset})
}

func writeLocalMedia(destination string, body io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(destination), ".upload-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, copyErr := io.Copy(file, body)
	syncErr := file.Sync()
	if err := errors.Join(copyErr, syncErr, file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), destination)
}

func (s *Server) serveMediaFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		methodNotAllowed(w)
		return
	}
	if s.database == nil {
		http.NotFound(w, r)
		return
	}
	sitePart, filename, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, mediaFilePrefix), "/")
	if !ok || filename == "" || strings.Contains(filename, "/") {
		http.NotFound(w, r)
		return
	}
	siteID, err := strconv.ParseInt(sitePart, 10, 64)
	if err != nil || siteID <= 0 {
		http.NotFound(w, r)
		return
	}
	publicRequest := r.Clone(r.Context())
	publicRequest.URL.RawQuery = ""
	publicRequest.Header.Del("X-Site-Id")
	publicSite, err := s.resolvePublicSiteID(publicRequest)
	if err != nil || publicSite != siteID {
		user := s.currentAdmin(r)
		if user == nil || !user.CanManageSite(siteID) || !user.HasPermissionInSite(siteID, "media") {
			http.NotFound(w, r)
			return
		}
	}
	asset, err := scanMedia(s.database.QueryRowContext(r.Context(), mediaColumns+
		` WHERE site_id=? AND public_path=? AND status='active'`, siteID, r.URL.Path))
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "附件暂时不可用", http.StatusServiceUnavailable)
		return
	}
	if asset.Storage == "local" {
		diskPath, err := s.mediaDiskPath("/assets/" + asset.storagePath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", asset.MimeType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", mediaDisposition(asset))
		http.ServeFile(w, r, diskPath)
		return
	}
	if !s.redirectR2Media(w, r, asset) {
		http.Error(w, "附件暂时不可用", http.StatusServiceUnavailable)
	}
}

func (s *Server) redirectR2Media(w http.ResponseWriter, r *http.Request, asset MediaAsset) bool {
	config, err := s.loadAttachmentStorage(r.Context(), asset)
	if err != nil || asset.Storage != "r2" || config.Endpoint == "" {
		return false
	}
	w.Header().Set("Cache-Control", "private, no-store")
	if config.PublicURL != "" {
		http.Redirect(w, r, config.PublicURL+"/"+asset.storagePath, http.StatusTemporaryRedirect)
		return true
	}
	client, err := s.r2Client(config)
	if err != nil {
		return false
	}
	presign := s3.NewPresignClient(client, func(o *s3.PresignOptions) { o.Expires = 5 * time.Minute })
	bucket, key := aws.String(config.Bucket), aws.String(asset.storagePath)
	var target string
	if r.Method == http.MethodHead {
		request, err := presign.PresignHeadObject(r.Context(), &s3.HeadObjectInput{Bucket: bucket, Key: key})
		if err == nil {
			target = request.URL
		}
	} else {
		request, err := presign.PresignGetObject(r.Context(), &s3.GetObjectInput{Bucket: bucket, Key: key})
		if err == nil {
			target = request.URL
		}
	}
	if target == "" {
		return false
	}
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, target, http.StatusTemporaryRedirect)
	return true
}

func (s *Server) deleteStoredMedia(w http.ResponseWriter, r *http.Request, asset MediaAsset) {
	ctx := r.Context()
	// Check references while marking the record, before attempting the object
	// deletion. An uncertain remote response leaves a retryable record.
	result, err := s.database.ExecContext(ctx, `UPDATE gocms_media SET status='deleting'
		WHERE id=? AND status IN ('active','deleting')
		AND NOT EXISTS (SELECT 1 FROM gocms_media_ref WHERE media_id=?)
		AND NOT EXISTS (SELECT 1 FROM gocms_commerce_offer WHERE site_id=? AND image_url=? AND archived=0)
		AND NOT EXISTS (SELECT 1 FROM gocms_commerce_order_item i JOIN gocms_commerce_order o ON o.id=i.order_id
			WHERE o.site_id=? AND i.image_url=?)`, asset.ID, asset.ID, asset.SiteID, asset.URL, asset.SiteID, asset.URL)
	if err != nil {
		http.Error(w, "标记附件删除失败", http.StatusInternalServerError)
		return
	}
	if count, _ := result.RowsAffected(); count != 1 {
		http.Error(w, "附件正在被内容或交易记录引用，不能删除", http.StatusConflict)
		return
	}
	switch asset.Storage {
	case "r2":
		config, loadErr := s.loadAttachmentStorage(ctx, asset)
		if loadErr != nil {
			err = loadErr
			break
		}
		client, clientErr := s.r2Client(config)
		if clientErr != nil {
			err = clientErr
			break
		}
		_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(config.Bucket), Key: aws.String(asset.storagePath)})
		if err == nil && asset.Kind == "image" {
			siteID := asset.SiteID
			if siteID == 0 {
				siteID = 1
			}
			if rel := localUploadImagePath(asset.URL, siteID); rel != "" {
				err = removeLocalUpload(s.assetsRoot, rel)
			}
		}
	case "local":
		diskPath, pathErr := s.mediaDiskPath("/assets/" + asset.storagePath)
		err = pathErr
		if err == nil {
			err = os.Remove(diskPath)
			if errors.Is(err, os.ErrNotExist) {
				err = nil
			}
		}
	default:
		err = fmt.Errorf("unknown storage provider")
	}
	if err != nil {
		http.Error(w, "附件删除未完成，可以重试", http.StatusBadGateway)
		return
	}
	if _, err := s.database.ExecContext(ctx, `DELETE FROM gocms_media WHERE id=?`, asset.ID); err != nil {
		http.Error(w, "删除附件记录失败，可以重试", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
