package site

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gocms/internal/geography"
)

func (s *Server) ConfigureGeography(data string) error {
	store, err := geography.Open(filepath.Join(data, "geography"))
	if err != nil {
		return err
	}
	s.geography = store
	return nil
}
func (s *Server) CloseGeography() {
	if s.geography != nil {
		_ = s.geography.Close()
	}
}
func geographyError(w http.ResponseWriter, err error) {
	status := 503
	code := "unavailable"
	message := "地址数据暂不可用，请稍后重试"
	if errors.Is(err, geography.ErrInvalid) {
		status = 400
		code = "invalid_input"
		message = "地址数据文件或查询条件无效，请使用 GeoNames 官方格式"
	}
	if errors.Is(err, geography.ErrBusy) {
		status = 409
		code = "conflict"
		message = "地址数据正在导入，请等待完成"
	}
	writeJSON(w, status, map[string]string{"code": code, "error": message})
}
func (s *Server) publicGeography(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if s.geography == nil {
		geographyError(w, errors.New("unavailable"))
		return
	}
	country := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("country")))
	var items []geography.Item
	var err error
	switch strings.TrimPrefix(r.URL.Path, "/api/geography/") {
	case "countries":
		items, err = s.geography.Countries(r.Context())
	case "regions":
		items, err = s.geography.Regions(r.Context(), country)
	case "cities":
		items, err = s.geography.Cities(r.Context(), country, r.URL.Query().Get("region"), r.URL.Query().Get("q"))
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		geographyError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "source": geography.Source, "license": geography.License})
}
func (s *Server) adminGeography(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.geography == nil {
		geographyError(w, errors.New("unavailable"))
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/admin/geography")
	if (path == "" || path == "/") && r.Method == http.MethodGet {
		status, err := s.geography.Status(r.Context())
		if err != nil {
			geographyError(w, err)
			return
		}
		writeJSON(w, 200, status)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	// All catalogue mutations require a real super-admin session (enforced by
	// authorizeAdminRoute) and same-origin requests, including multipart uploads.
	if !geographyOrigin(r) {
		writeJSON(w, 403, map[string]string{"error": "请求来源不被允许"})
		return
	}
	switch path {
	case "/download":
		if err := s.geography.Start(s.geography.Download); err != nil {
			geographyError(w, err)
			return
		}
		writeJSON(w, 202, map[string]bool{"ok": true})
	case "/activate":
		var body struct {
			ID int64 `json:"id"`
		}
		if !memberRequest(w, r) {
			return
		}
		if err := memberDecode(r, &body); err != nil {
			geographyError(w, geography.ErrInvalid)
			return
		}
		if err := s.geography.Activate(r.Context(), body.ID); err != nil {
			geographyError(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	case "/import":
		s.uploadGeography(w, r)
	default:
		http.NotFound(w, r)
	}
}
func (s *Server) uploadGeography(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, geography.MaxUpload)
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		writeJSON(w, 400, map[string]string{"error": "上传失败，文件总大小不得超过 64 MiB"})
		return
	}
	defer r.MultipartForm.RemoveAll()
	version := strings.TrimSpace(r.FormValue("version"))
	if version == "" || len(version) > 80 {
		writeJSON(w, 400, map[string]string{"error": "版本名称不能为空，且不能超过 80 个 UTF-8 字节"})
		return
	}
	dir, err := os.MkdirTemp("", "gocms-geography-upload-")
	if err != nil {
		geographyError(w, err)
		return
	}
	queued := false
	defer func() {
		if !queued {
			os.RemoveAll(dir)
		}
	}()
	paths := map[string]string{}
	for _, name := range []string{"countries", "regions", "cities"} {
		file, _, err := r.FormFile(name)
		if errors.Is(err, http.ErrMissingFile) && name == "countries" {
			continue
		}
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "请选择行政区和城市文件"})
			return
		}
		path := filepath.Join(dir, name)
		target, err := os.Create(path)
		if err != nil {
			file.Close()
			geographyError(w, err)
			return
		}
		size, copyErr := io.Copy(target, io.LimitReader(file, geography.MaxUpload+1))
		closeErr := target.Close()
		file.Close()
		if copyErr != nil || closeErr != nil || size > geography.MaxUpload {
			writeJSON(w, 400, map[string]string{"error": "文件保存失败或超过大小限制"})
			return
		}
		paths[name] = path
	}
	err = s.geography.Start(func(ctx context.Context) error {
		defer os.RemoveAll(dir)
		return s.geography.Import(ctx, version, paths["countries"], paths["regions"], paths["cities"])
	})
	if err != nil {
		geographyError(w, err)
		return
	}
	queued = true
	writeJSON(w, 202, map[string]bool{"ok": true})
}
func geographyOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		return err == nil && strings.EqualFold(u.Host, requestHost(r)) && strings.EqualFold(u.Scheme, requestScheme(r))
	}
	return true
}
