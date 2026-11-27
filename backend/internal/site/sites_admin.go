package site

import (
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gocms/internal/consent"
	"gocms/internal/db"
	"gocms/internal/imageopt"
	"gocms/internal/security"
	"gocms/internal/sitecontacts"
)

func (s *Server) currentAdmin(r *http.Request) *AdminUser {
	if u, ok := r.Context().Value(contentScopeKey{}).(*AdminUser); ok && u != nil {
		return u
	}
	user, ok, _ := s.authenticateRequest(r)
	if ok && user != nil {
		return user
	}
	return nil
}

func requestHost(r *http.Request) string {
	if r == nil {
		return ""
	}
	return security.RequestHost(r)
}

func requestScheme(r *http.Request) string {
	if r == nil {
		return "http"
	}
	if r.TLS != nil {
		return "https"
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Scheme")), "https") {
		return "https"
	}
	if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto != "" {
		if comma := strings.Index(proto, ","); comma != -1 {
			proto = strings.TrimSpace(proto[:comma])
		}
		if strings.EqualFold(proto, "https") {
			return "https"
		}
	}
	return "http"
}

func (s *Server) resolveSiteID(r *http.Request, user *AdminUser) (int64, error) {
	siteIDStr := ""
	if r != nil {
		siteIDStr = strings.TrimSpace(r.URL.Query().Get("site_id"))
		if siteIDStr == "" {
			siteIDStr = strings.TrimSpace(r.Header.Get("X-Site-Id"))
		}
		if siteIDStr == "" && r.Form != nil {
			siteIDStr = strings.TrimSpace(r.Form.Get("site_id"))
		}
	}
	var siteID int64
	if siteIDStr != "" {
		parsed, err := strconv.ParseInt(siteIDStr, 10, 64)
		if err == nil && parsed > 0 {
			siteID = parsed
		}
	}
	if siteID <= 0 {
		if user != nil && !user.IsSuper && len(user.SiteIDs) > 0 {
			siteID = user.SiteIDs[0]
		} else if s.database != nil && r != nil {
			host := requestHost(r)
			if matched, err := db.GetSiteByHost(r.Context(), s.database, host); err == nil && matched != nil {
				siteID = matched.ID
			} else if def, err := db.GetDefaultSite(r.Context(), s.database); err == nil && def != nil {
				siteID = def.ID
			}
		}
	}
	if siteID <= 0 {
		siteID = 1
	}

	if s.database != nil && r != nil {
		target, err := db.GetSiteByID(r.Context(), s.database, siteID)
		if err != nil || target == nil {
			if user != nil {
				return 0, errors.New("站点不存在")
			}
			if def, errDef := db.GetDefaultSite(r.Context(), s.database); errDef == nil && def != nil {
				target = def
				siteID = def.ID
			} else {
				return 0, errors.New("站点不存在")
			}
		}
		if user == nil && target.Status != "active" {
			return 0, errors.New("站点已停用")
		}
	}

	if user != nil && !user.CanManageSite(siteID) {
		return 0, errors.New("无权访问该站点")
	}
	return siteID, nil
}

// resolvePublicSiteID binds public reads and submissions to the request host.
// Explicit site selectors are accepted only when no configured host matched
// (for local tooling and backwards-compatible development requests).
func (s *Server) resolvePublicSiteID(r *http.Request) (int64, error) {
	if s.database != nil && r != nil {
		if matched, err := db.GetSiteByHost(r.Context(), s.database, requestHost(r)); err == nil && matched != nil {
			if matched.Status != "active" {
				return 0, errors.New("站点已停用")
			}
			return matched.ID, nil
		}
	}
	return s.resolveSiteID(r, nil)
}

type adminSiteView struct {
	db.Site
	PublicOrigin string `json:"public_origin"`
}

func siteForAdmin(site db.Site) adminSiteView {
	return adminSiteView{Site: site, PublicOrigin: db.PublicOrigin(site.Domain)}
}

const siteOptionsRoute = "/api/admin/site-options"

func (s *Server) adminSiteOptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	user := s.currentAdmin(r)
	sites, err := db.ListSites(r.Context(), s.database)
	if err != nil {
		accountError(w, err)
		return
	}
	type option struct {
		ID           int64  `json:"id"`
		Name         string `json:"name"`
		Domain       string `json:"domain"`
		PublicOrigin string `json:"public_origin"`
		IsDefault    bool   `json:"is_default"`
		Status       string `json:"status"`
	}
	items := make([]option, 0, len(sites))
	for _, site := range sites {
		if user.CanManageSite(site.ID) {
			items = append(items, option{site.ID, site.Name, site.Domain, db.PublicOrigin(site.Domain), site.IsDefault, site.Status})
		}
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) adminSites(w http.ResponseWriter, r *http.Request) {
	user := s.currentAdmin(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录或账号已停用"})
		return
	}

	if r.Method == http.MethodGet {
		allSites, err := db.ListSites(r.Context(), s.database)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		accessible := make([]adminSiteView, 0, len(allSites))
		for _, site := range allSites {
			if user.CanManageSite(site.ID) {
				accessible = append(accessible, siteForAdmin(site))
			}
		}
		writeJSON(w, http.StatusOK, accessible)
		return
	}

	if r.Method == http.MethodPost {
		if !user.IsSuper {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "只有超级管理员可以创建站点"})
			return
		}
		var payload db.Site
		if err := decodeRequest(r, &payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式不正确"})
			return
		}
		created, err := db.CreateSite(r.Context(), s.database, &payload)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, siteForAdmin(*created))
		return
	}

	methodNotAllowed(w)
}

func (s *Server) adminSiteItem(w http.ResponseWriter, r *http.Request, idStr string) {
	user := s.currentAdmin(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录或账号已停用"})
		return
	}

	id, err := strconv.ParseInt(strings.TrimSpace(idStr), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}

	if !user.CanManageSite(id) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "无权操作该站点"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		site, err := db.GetSiteByID(r.Context(), s.database, id)
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, siteForAdmin(*site))

	case http.MethodPut:
		var payload db.Site
		if err := decodeRequest(r, &payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式不正确"})
			return
		}
		payload.ID = id
		if !user.IsSuper {
			// Ordinary site admin cannot change code or is_default
			existing, err := db.GetSiteByID(r.Context(), s.database, id)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			payload.Code = existing.Code
			payload.IsDefault = existing.IsDefault
		}
		if err := db.UpdateSite(r.Context(), s.database, &payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		s.invalidateSitePublication(id)
		writeJSON(w, http.StatusOK, siteForAdmin(payload))

	case http.MethodDelete:
		if !user.IsSuper {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "只有超级管理员可以删除站点"})
			return
		}
		existing, err := db.GetSiteByID(r.Context(), s.database, id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "站点不存在"})
			return
		}
		s.mediaStorageMu.Lock()
		defer s.mediaStorageMu.Unlock()
		var hasR2Attachments bool
		if err := s.database.QueryRowContext(r.Context(), `SELECT EXISTS (
			SELECT 1 FROM gocms_media WHERE site_id=? AND storage_provider='r2')`, id).Scan(&hasR2Attachments); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "检查站点附件失败"})
			return
		}
		if hasR2Attachments {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "本站还有 R2 附件，请先处理附件再删除站点"})
			return
		}
		if err := db.DeleteSite(r.Context(), s.database, id); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		s.invalidateSitePublication(id)
		if s.siteRoot != "" && id > 1 {
			targetDir := ""
			if existing.OutputDir != "" {
				targetDir = filepath.Join(s.siteRoot, existing.OutputDir)
			} else {
				targetDir = filepath.Join(s.siteRoot, strconv.FormatInt(id, 10))
			}
			if targetDir != "" && targetDir != s.siteRoot {
				_ = os.RemoveAll(targetDir)
			}
		}
		if s.assetsRoot != "" && id > 1 {
			_ = os.RemoveAll(filepath.Join(s.assetsRoot, strconv.FormatInt(id, 10)))
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) adminSiteSettings(w http.ResponseWriter, r *http.Request) {
	user := s.currentAdmin(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录或账号已停用"})
		return
	}

	siteID, err := s.resolveSiteID(r, user)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}

	switch r.Method {
	case http.MethodGet:
		settings, err := db.LoadSiteSettingsForSite(r.Context(), s.database, siteID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		delete(settings, sitecontacts.SettingKey)
		writeJSON(w, http.StatusOK, settings)

	case http.MethodPut, http.MethodPost:
		var payload map[string]string
		if err := decodeRequest(r, &payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式不正确"})
			return
		}
		if _, ok := payload["site_url"]; ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请在多站点管理中编辑主域名"})
			return
		}
		if _, ok := payload[sitecontacts.SettingKey]; ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请在语言管理中编辑联系方式"})
			return
		}
		if _, err := imageopt.ParseSettings(payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if raw, ok := payload[consent.SettingKey]; ok {
			if _, err := consent.Parse(raw); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "隐私同意配置无效: " + err.Error()})
				return
			}
		}
		if err := db.SaveSiteSettingsForSite(r.Context(), s.database, siteID, payload); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.invalidateSitePublication(siteID)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		methodNotAllowed(w)
	}
}
