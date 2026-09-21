package site

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"gocms/internal/db"
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

func (s *Server) resolveSiteID(r *http.Request, user *AdminUser) (int64, error) {
	siteIDStr := strings.TrimSpace(r.URL.Query().Get("site_id"))
	if siteIDStr == "" {
		siteIDStr = strings.TrimSpace(r.Header.Get("X-Site-Id"))
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
		} else {
			siteID = 1
		}
	}
	if user != nil && !user.CanManageSite(siteID) {
		return 0, errors.New("无权访问该站点")
	}
	return siteID, nil
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
		var accessible []db.Site
		for _, site := range allSites {
			if user.CanManageSite(site.ID) {
				accessible = append(accessible, site)
			}
		}
		if accessible == nil {
			accessible = []db.Site{}
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
		writeJSON(w, http.StatusCreated, created)
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
		writeJSON(w, http.StatusOK, site)

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
		writeJSON(w, http.StatusOK, payload)

	case http.MethodDelete:
		if !user.IsSuper {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "只有超级管理员可以删除站点"})
			return
		}
		if err := db.DeleteSite(r.Context(), s.database, id); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
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
		writeJSON(w, http.StatusOK, settings)

	case http.MethodPut, http.MethodPost:
		var payload map[string]string
		if err := decodeRequest(r, &payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式不正确"})
			return
		}
		if err := db.SaveSiteSettingsForSite(r.Context(), s.database, siteID, payload); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})

	default:
		methodNotAllowed(w)
	}
}
