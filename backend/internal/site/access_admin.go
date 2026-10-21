package site

import (
	"net/http"
	"strings"
)

// adminAccessLogs serves the per-site public access log: a paginated raw list
// (GET) and a filtered clear action (DELETE). Both are scoped to the active
// site through resolveSiteID, and permissions are enforced by
// authorizeAdminRoute before the handler runs.
func (s *Server) adminAccessLogs(response http.ResponseWriter, request *http.Request) {
	user := s.currentAdmin(request)
	siteID, err := s.resolveSiteID(request, user)
	if err != nil || siteID <= 0 {
		writeJSON(response, http.StatusForbidden, map[string]string{"error": "无权访问该站点统计"})
		return
	}
	switch request.Method {
	case http.MethodGet:
		filter := parseAccessLogFilter(request.URL.Query())
		page := positiveInt(request.URL.Query().Get("page"), 1)
		pageSize := positiveInt(request.URL.Query().Get("page_size"), accessLogDefaultPageSize)
		if pageSize > accessLogMaxPageSize {
			pageSize = accessLogMaxPageSize
		}
		items, total, err := s.listAccessLogs(request.Context(), siteID, filter, page, pageSize)
		if err != nil {
			accountError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{
			"items":     items,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
			"site_id":   siteID,
		})
	case http.MethodDelete:
		deleted, err := s.clearAccessLogs(request.Context(), siteID, parseAccessLogFilter(request.URL.Query()))
		if err != nil {
			accountError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"deleted": deleted, "site_id": siteID})
	default:
		methodNotAllowed(response)
	}
}

// adminAccessLogErrors returns the grouped error list (path + status) that the
// dashboard and the error view use to point at broken URLs.
func (s *Server) adminAccessLogErrors(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(response)
		return
	}
	user := s.currentAdmin(request)
	siteID, err := s.resolveSiteID(request, user)
	if err != nil || siteID <= 0 {
		writeJSON(response, http.StatusForbidden, map[string]string{"error": "无权访问该站点统计"})
		return
	}
	limit := positiveInt(request.URL.Query().Get("limit"), 100)
	if limit > 500 {
		limit = 500
	}
	offset := nonNegativeInt(request.URL.Query().Get("offset"), 0)
	items, total, err := s.listAccessLogErrors(request.Context(), siteID, parseAccessLogFilter(request.URL.Query()), limit, offset)
	if err != nil {
		accountError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"items":   items,
		"total":   total,
		"offset":  offset,
		"limit":   limit,
		"site_id": siteID,
	})
}

// adminAccessLogPages serves the paginated "popular pages" ranking used by the
// dashboard. It groups successful human page views by path, ordered by visits,
// and is scoped to the active site like every other access-log endpoint.
func (s *Server) adminAccessLogPages(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(response)
		return
	}
	user := s.currentAdmin(request)
	siteID, err := s.resolveSiteID(request, user)
	if err != nil || siteID <= 0 {
		writeJSON(response, http.StatusForbidden, map[string]string{"error": "无权访问该站点统计"})
		return
	}
	limit := positiveInt(request.URL.Query().Get("limit"), 20)
	if limit > 200 {
		limit = 200
	}
	offset := nonNegativeInt(request.URL.Query().Get("offset"), 0)
	items, total, err := s.listAccessLogPages(request.Context(), siteID, limit, offset)
	if err != nil {
		accountError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"items":   items,
		"total":   total,
		"offset":  offset,
		"limit":   limit,
		"site_id": siteID,
	})
}

// adminAccessLogIgnores manages path-only suppressions used by the error
// statistics. Rules are scoped to the active site but match every host, so a
// scanner path such as /wp-admin/install.php is hidden on the main domain and
// on every language/alias domain at once.
func (s *Server) adminAccessLogIgnores(response http.ResponseWriter, request *http.Request) {
	user := s.currentAdmin(request)
	siteID, err := s.resolveSiteID(request, user)
	if err != nil || siteID <= 0 {
		writeJSON(response, http.StatusForbidden, map[string]string{"error": "无权访问该站点统计"})
		return
	}
	switch request.Method {
	case http.MethodGet:
		items, err := s.listAccessLogIgnores(request.Context(), siteID)
		if err != nil {
			accountError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{
			"items":   items,
			"total":   len(items),
			"site_id": siteID,
		})
	case http.MethodPost:
		var payload struct {
			Path string `json:"path"`
		}
		if err := decodeRequest(request, &payload); err != nil {
			writeJSON(response, http.StatusBadRequest, map[string]string{"error": "请求格式无效"})
			return
		}
		item, err := s.addAccessLogIgnore(request.Context(), siteID, payload.Path)
		if err != nil {
			writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(response, http.StatusCreated, map[string]any{"ok": true, "item": item, "site_id": siteID})
	case http.MethodDelete:
		pathValue := strings.TrimSpace(request.URL.Query().Get("path"))
		if pathValue == "" {
			writeJSON(response, http.StatusBadRequest, map[string]string{"error": "请指定要取消忽略的路径"})
			return
		}
		removed, err := s.removeAccessLogIgnore(request.Context(), siteID, pathValue)
		if err != nil {
			writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"ok": true, "removed": removed, "site_id": siteID})
	default:
		methodNotAllowed(response)
	}
}
