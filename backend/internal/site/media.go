package site

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"html"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	maxMediaUploadSize  int64 = 10 << 20
	maxMediaRequestSize int64 = maxMediaUploadSize + 1<<20
	maxMediaDimension   int64 = 12000
	maxMediaPixels      int64 = 48_000_000
)

var contentImageSource = regexp.MustCompile(`(?is)<img\b[^>]*?\ssrc\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)

type MediaAsset struct {
	ID           int64  `json:"id"`
	Kind         string `json:"kind"`
	URL          string `json:"url"`
	OriginalName string `json:"original_name"`
	MimeType     string `json:"mime_type"`
	SizeBytes    int64  `json:"size_bytes"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	SHA256       string `json:"sha256"`
	Status       string `json:"status"`
	UploadedBy   string `json:"uploaded_by"`
	CreatedAt    string `json:"created_at"`
}

type MediaPage struct {
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
	Total    int64        `json:"total"`
	Items    []MediaAsset `json:"items"`
}

func (s *Server) adminMedia(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodPost {
		response.Header().Set("Allow", "GET, POST")
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireAdmin(response, request) {
		return
	}
	if request.Method == http.MethodPost {
		s.uploadMedia(response, request)
		return
	}
	s.listMedia(response, request)
}

func (s *Server) adminMediaItem(response http.ResponseWriter, request *http.Request, rawID string) {
	if request.Method != http.MethodGet && request.Method != http.MethodDelete {
		response.Header().Set("Allow", "GET, DELETE")
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireAdmin(response, request) {
		return
	}
	id, err := strconv.ParseInt(strings.TrimSpace(rawID), 10, 64)
	if err != nil || id < 1 {
		http.Error(response, "invalid media id", http.StatusBadRequest)
		return
	}
	asset, err := s.readMedia(request.Context(), id)
	if err == sql.ErrNoRows {
		http.Error(response, "media not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	if request.Method == http.MethodGet {
		writeJSON(response, http.StatusOK, asset)
		return
	}
	s.deleteMedia(response, request, asset)
}

func (s *Server) uploadMedia(response http.ResponseWriter, request *http.Request) {
	if s.database == nil || strings.TrimSpace(s.assetsRoot) == "" {
		http.Error(response, "media storage is not configured", http.StatusServiceUnavailable)
		return
	}
	if !strings.HasPrefix(strings.ToLower(request.Header.Get("Content-Type")), "multipart/form-data;") {
		http.Error(response, "multipart upload is required", http.StatusBadRequest)
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxMediaRequestSize)
	file, header, err := request.FormFile("file")
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "too large") {
			http.Error(response, "图片不能超过 10 MB", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(response, "请选择图片文件", http.StatusBadRequest)
		return
	}
	defer file.Close()

	monthPath := time.Now().UTC().Format("2006/01")
	uploadDirectory := filepath.Join(s.assetsRoot, "images", "uploads", filepath.FromSlash(monthPath))
	if err := os.MkdirAll(uploadDirectory, 0o755); err != nil {
		http.Error(response, "创建图片目录失败", http.StatusInternalServerError)
		return
	}
	temporary, err := os.CreateTemp(uploadDirectory, ".upload-*")
	if err != nil {
		http.Error(response, "创建临时文件失败", http.StatusInternalServerError)
		return
	}
	temporaryPath := temporary.Name()
	defer func() {
		if temporaryPath != "" {
			_ = os.Remove(temporaryPath)
		}
	}()

	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(temporary, hasher), io.LimitReader(file, maxMediaUploadSize+1))
	if err != nil {
		http.Error(response, "读取图片失败", http.StatusBadRequest)
		return
	}
	if written > maxMediaUploadSize {
		http.Error(response, "图片不能超过 10 MB", http.StatusRequestEntityTooLarge)
		return
	}
	if written == 0 {
		http.Error(response, "图片文件为空", http.StatusBadRequest)
		return
	}
	if err := temporary.Sync(); err != nil {
		http.Error(response, "保存图片失败", http.StatusInternalServerError)
		return
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		http.Error(response, "读取图片失败", http.StatusInternalServerError)
		return
	}
	config, format, err := image.DecodeConfig(temporary)
	if err != nil {
		http.Error(response, "文件不是受支持的图片格式", http.StatusBadRequest)
		return
	}
	mimeType, extension, ok := mediaImageFormat(format)
	if !ok {
		http.Error(response, "只支持 JPEG、PNG 和 GIF 图片", http.StatusBadRequest)
		return
	}
	if config.Width < 1 || config.Height < 1 || int64(config.Width) > maxMediaDimension || int64(config.Height) > maxMediaDimension || int64(config.Width)*int64(config.Height) > maxMediaPixels {
		http.Error(response, "图片尺寸过大", http.StatusBadRequest)
		return
	}

	randomName := make([]byte, 16)
	if _, err := rand.Read(randomName); err != nil {
		http.Error(response, "生成图片名称失败", http.StatusInternalServerError)
		return
	}
	filename := hex.EncodeToString(randomName) + extension
	storagePath := path.Join("images", "uploads", monthPath, filename)
	finalPath := filepath.Join(s.assetsRoot, filepath.FromSlash(storagePath))
	if err := temporary.Close(); err != nil {
		http.Error(response, "保存图片失败", http.StatusInternalServerError)
		return
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		http.Error(response, "保存图片失败", http.StatusInternalServerError)
		return
	}
	temporaryPath = ""
	if err := os.Chmod(finalPath, 0o644); err != nil {
		_ = os.Remove(finalPath)
		http.Error(response, "保存图片失败", http.StatusInternalServerError)
		return
	}
	removeFinal := true
	defer func() {
		if removeFinal {
			_ = os.Remove(finalPath)
		}
	}()

	originalName := path.Base(strings.ReplaceAll(header.Filename, "\\", "/"))
	if originalName == "." {
		originalName = ""
	}
	uploadedBy, _ := s.adminUsername(request)
	createdAt := time.Now().UTC().Format(time.RFC3339)
	result, err := s.database.ExecContext(request.Context(), `
		INSERT INTO "gocms_media"
		("kind", "storage_path", "public_path", "original_name", "mime_type", "size_bytes", "width", "height", "sha256", "status", "uploaded_by", "created_at")
		VALUES ('image', ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?)`,
		storagePath, "/"+storagePath, originalName, mimeType, written, config.Width, config.Height,
		hex.EncodeToString(hasher.Sum(nil)), uploadedBy, createdAt)
	if err != nil {
		http.Error(response, "保存图片记录失败", http.StatusInternalServerError)
		return
	}
	id, err := result.LastInsertId()
	if err != nil {
		http.Error(response, "读取图片记录失败", http.StatusInternalServerError)
		return
	}
	removeFinal = false
	writeJSON(response, http.StatusCreated, map[string]any{
		"ok": true,
		"asset": MediaAsset{
			ID: id, Kind: "image", URL: "/" + storagePath, OriginalName: originalName,
			MimeType: mimeType, SizeBytes: written, Width: config.Width, Height: config.Height,
			SHA256: hex.EncodeToString(hasher.Sum(nil)), Status: "active", UploadedBy: uploadedBy, CreatedAt: createdAt,
		},
	})
}

func mediaImageFormat(format string) (string, string, bool) {
	switch strings.ToLower(format) {
	case "jpeg":
		return "image/jpeg", ".jpg", true
	case "png":
		return "image/png", ".png", true
	case "gif":
		return "image/gif", ".gif", true
	default:
		return "", "", false
	}
}

func (s *Server) listMedia(response http.ResponseWriter, request *http.Request) {
	kind := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("kind")))
	if kind == "" {
		kind = "image"
	}
	if kind != "image" {
		http.Error(response, "invalid media kind", http.StatusBadRequest)
		return
	}
	page := positiveInt(request.URL.Query().Get("page"), 1)
	pageSize := positiveInt(request.URL.Query().Get("page_size"), 20)
	if pageSize > 100 {
		pageSize = 100
	}
	contentID := parseIntOrZero(request.URL.Query().Get("content_id"))
	search := strings.TrimSpace(request.URL.Query().Get("q"))
	where := `"status" = 'active' AND "kind" = ?`
	args := []any{kind}
	if contentID > 0 {
		where += ` AND EXISTS (SELECT 1 FROM "gocms_media_ref" AS ref WHERE ref."media_id" = "gocms_media"."id" AND ref."content_id" = ?)`
		args = append(args, contentID)
	}
	if search != "" {
		where += ` AND ("original_name" LIKE ? OR "storage_path" LIKE ?)`
		pattern := "%" + search + "%"
		args = append(args, pattern, pattern)
	}
	var total int64
	if err := s.database.QueryRowContext(request.Context(), `SELECT COUNT(*) FROM "gocms_media" WHERE `+where, args...).Scan(&total); err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	listArgs := append(append([]any{}, args...), pageSize, (page-1)*pageSize)
	rows, err := s.database.QueryContext(request.Context(), `
		SELECT "id", "kind", "public_path", "original_name", "mime_type", "size_bytes", "width", "height", "sha256", "status", "uploaded_by", "created_at"
		FROM "gocms_media" WHERE `+where+` ORDER BY "id" DESC LIMIT ? OFFSET ?`, listArgs...)
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	items := make([]MediaAsset, 0, pageSize)
	for rows.Next() {
		asset, err := scanMedia(rows)
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		items = append(items, asset)
	}
	if err := rows.Err(); err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(response, http.StatusOK, MediaPage{Page: page, PageSize: pageSize, Total: total, Items: items})
}

type mediaScanner interface {
	Scan(...any) error
}

func scanMedia(scanner mediaScanner) (MediaAsset, error) {
	var asset MediaAsset
	err := scanner.Scan(
		&asset.ID, &asset.Kind, &asset.URL, &asset.OriginalName, &asset.MimeType, &asset.SizeBytes,
		&asset.Width, &asset.Height, &asset.SHA256, &asset.Status, &asset.UploadedBy, &asset.CreatedAt,
	)
	return asset, err
}

func (s *Server) readMedia(ctx context.Context, id int64) (MediaAsset, error) {
	return scanMedia(s.database.QueryRowContext(ctx, `
		SELECT "id", "kind", "public_path", "original_name", "mime_type", "size_bytes", "width", "height", "sha256", "status", "uploaded_by", "created_at"
		FROM "gocms_media" WHERE "id" = ?`, id))
}

func (s *Server) deleteMedia(response http.ResponseWriter, request *http.Request, asset MediaAsset) {
	var references int64
	if err := s.database.QueryRowContext(request.Context(), `SELECT COUNT(*) FROM "gocms_media_ref" WHERE "media_id" = ?`, asset.ID).Scan(&references); err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	if references > 0 {
		http.Error(response, "图片正在被内容使用，不能删除", http.StatusConflict)
		return
	}
	diskPath, err := s.mediaDiskPath(asset.URL)
	if err != nil {
		http.Error(response, "invalid media path", http.StatusInternalServerError)
		return
	}
	if err := os.Remove(diskPath); err != nil && !os.IsNotExist(err) {
		http.Error(response, "删除图片文件失败", http.StatusInternalServerError)
		return
	}
	result, err := s.database.ExecContext(request.Context(), `DELETE FROM "gocms_media" WHERE "id" = ?`, asset.ID)
	if err != nil {
		http.Error(response, "删除图片记录失败", http.StatusInternalServerError)
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		http.Error(response, "数据库错误", http.StatusInternalServerError)
		return
	}
	if affected == 0 {
		http.Error(response, "media not found", http.StatusNotFound)
		return
	}
	writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) mediaDiskPath(publicPath string) (string, error) {
	if s.assetsRoot == "" {
		return "", fmt.Errorf("media storage is not configured")
	}
	parsed, err := url.Parse(publicPath)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Path == "" {
		return "", fmt.Errorf("invalid media path")
	}
	cleanPublic := path.Clean(parsed.Path)
	if !strings.HasPrefix(cleanPublic, "/images/") {
		return "", fmt.Errorf("invalid media path")
	}
	storagePath := strings.TrimPrefix(cleanPublic, "/")
	root, err := filepath.Abs(s.assetsRoot)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(storagePath)))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid media path")
	}
	return target, nil
}

type mediaReferenceStore interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func syncContentMediaRefs(ctx context.Context, store mediaReferenceStore, contentID int64, body, cover string) error {
	if contentID < 1 {
		return nil
	}
	if _, err := store.ExecContext(ctx, `DELETE FROM "gocms_media_ref" WHERE "content_id" = ?`, contentID); err != nil {
		return err
	}
	fields := map[string][]string{
		"body":        contentImageURLs(body),
		"cover_image": {normalizeMediaURL(cover)},
	}
	for field, values := range fields {
		seen := make(map[string]bool)
		for _, value := range values {
			if value == "" || seen[value] {
				continue
			}
			seen[value] = true
			var mediaID int64
			err := store.QueryRowContext(ctx, `
				SELECT "id" FROM "gocms_media"
				WHERE "public_path" = ? AND "kind" = 'image' AND "status" = 'active'`, value).Scan(&mediaID)
			if err == sql.ErrNoRows {
				continue
			}
			if err != nil {
				return err
			}
			if _, err := store.ExecContext(ctx, `
				INSERT OR IGNORE INTO "gocms_media_ref" ("media_id", "content_id", "field_name")
				VALUES (?, ?, ?)`, mediaID, contentID, field); err != nil {
				return err
			}
		}
	}
	return nil
}

func contentImageURLs(body string) []string {
	matches := contentImageSource.FindAllStringSubmatch(body, -1)
	urls := make([]string, 0, len(matches))
	for _, match := range matches {
		for index := 1; index < len(match); index++ {
			if match[index] != "" {
				if value := normalizeMediaURL(html.UnescapeString(match[index])); value != "" {
					urls = append(urls, value)
				}
				break
			}
		}
	}
	return urls
}

func normalizeMediaURL(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Scheme != "" || parsed.Path == "" {
		return ""
	}
	imagePath := parsed.Path
	if !strings.HasPrefix(imagePath, "/") {
		imagePath = "/" + imagePath
	}
	imagePath = path.Clean(imagePath)
	if !strings.HasPrefix(imagePath, "/images/") {
		return ""
	}
	return imagePath
}
