package site

import (
	"errors"
	"net/http"
)

const accessLogClearRoute = "/api/admin/access-logs/clear"

// adminAccessLogs serves the per-site public access log: a paginated raw list
// (GET) and a filtered clear action (DELETE). Both are scoped to the active
// site through resolveSiteID, and permissions are enforced by
// authorizeAdminRoute before the handler runs.
func (s *Server) adminAccessLogs(response http.ResponseWriter, request *http.Request) {
	if !s.requireAccessLogDB(response) {
		return
	}
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
		job, err := s.startAccessLogClear(request.Context(), siteID, parseAccessLogFilter(request.URL.Query()))
		if err != nil {
			writeAccessLogClearError(response, err)
			return
		}
		select {
		case <-request.Context().Done():
			return
		case <-job.done:
		}
		if job.State == "failed" {
			writeJSON(response, http.StatusInternalServerError, map[string]any{
				"error":   "清理中断，已删除的记录仍然生效，可以重新发起清理",
				"deleted": job.Deleted, "site_id": siteID,
			})
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"deleted": job.Deleted, "site_id": siteID})
	default:
		methodNotAllowed(response)
	}
}

func (s *Server) adminAccessLogClear(response http.ResponseWriter, request *http.Request) {
	if !s.requireAccessLogDB(response) {
		return
	}
	siteID, err := s.resolveSiteID(request, s.currentAdmin(request))
	if err != nil || siteID <= 0 {
		writeJSON(response, http.StatusForbidden, map[string]string{"error": "无权访问该站点统计"})
		return
	}
	switch request.Method {
	case http.MethodGet:
		response.Header().Set("Cache-Control", "private, no-store")
		writeJSON(response, http.StatusOK, s.accessLogClear.status(siteID))
	case http.MethodPost:
		_, err := s.startAccessLogClear(request.Context(), siteID, parseAccessLogFilter(request.URL.Query()))
		if err != nil {
			writeAccessLogClearError(response, err)
			return
		}
		writeJSON(response, http.StatusAccepted, s.accessLogClear.status(siteID))
	default:
		methodNotAllowed(response)
	}
}

func (s *Server) requireAccessLogDB(response http.ResponseWriter) bool {
	if s.accessLogDB() != nil {
		return true
	}
	writeJSON(response, http.StatusServiceUnavailable, map[string]string{"error": "访问日志数据库暂不可用，请检查服务日志"})
	return false
}

func writeAccessLogClearError(response http.ResponseWriter, err error) {
	if errors.Is(err, errAccessLogClearRunning) {
		writeJSON(response, http.StatusConflict, map[string]string{"error": "已有访问日志清理任务在运行，请稍后重试"})
		return
	}
	writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "启动访问日志清理失败，请稍后重试"})
}

// adminAccessLogErrors returns the grouped error list (path + status) that the
// dashboard and the error view use to point at broken URLs.
func (s *Server) adminAccessLogErrors(response http.ResponseWriter, request *http.Request) {
	if !s.requireAccessLogDB(response) {
		return
	}
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
	response.Header().Set("Cache-Control", "private, no-store")
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
	if !s.requireAccessLogDB(response) {
		return
	}
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
	items, total, err := s.listAccessLogPages(request.Context(), siteID, parseAccessLogFilter(request.URL.Query()), limit, offset)
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
