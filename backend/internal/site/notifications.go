package site

import (
	"context"
	"net/http"

	"gocms/internal/db"
	"gocms/internal/timestamps"
)

// Admin notification inbox.
//
// Integrations that fail in the background (a message that could not be pushed
// to Feishu, for example) must not stay invisible until somebody opens the
// affected record. They are therefore kept as site-scoped notifications that
// surface in the admin header on every page.
func (s *Server) adminNotifications(response http.ResponseWriter, request *http.Request) {
	if !s.requireAdmin(response, request) {
		return
	}
	siteID, err := s.resolveSiteID(request, s.currentAdmin(request))
	if err != nil {
		writeJSON(response, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}

	switch request.Method {
	case http.MethodGet:
		limit := positiveInt(request.URL.Query().Get("limit"), 50)
		items, unread, err := db.ListAdminNotifications(request.Context(), s.database, siteID, limit)
		if err != nil {
			writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "读取通知失败"})
			return
		}
		for index := range items {
			items[index].CreatedAt = timestamps.Local(items[index].CreatedAt)
			items[index].UpdatedAt = timestamps.Local(items[index].UpdatedAt)
			items[index].ReadAt = timestamps.Local(items[index].ReadAt)
		}
		writeJSON(response, http.StatusOK, map[string]any{"items": items, "unread": unread})

	case http.MethodPost:
		var payload struct {
			IDs []int64 `json:"ids"`
			All bool    `json:"all"`
		}
		if err := decodeRequest(request, &payload); err != nil {
			writeJSON(response, http.StatusBadRequest, map[string]string{"error": "请求格式不正确"})
			return
		}
		if !payload.All && len(payload.IDs) == 0 {
			writeJSON(response, http.StatusBadRequest, map[string]string{"error": "请指定要标记的通知"})
			return
		}
		ids := payload.IDs
		if payload.All {
			ids = nil
		}
		if err := db.MarkAdminNotificationsRead(request.Context(), s.database, siteID, ids); err != nil {
			writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "标记通知失败"})
			return
		}
		unread, err := db.CountUnreadAdminNotifications(request.Context(), s.database, siteID)
		if err != nil {
			unread = 0
		}
		writeJSON(response, http.StatusOK, map[string]any{"ok": true, "unread": unread})

	default:
		methodNotAllowed(response)
	}
}

// recordAdminNotification is a best-effort helper: a failure to store the
// notification must never break the work that triggered it.
func (s *Server) recordAdminNotification(ctx context.Context, notification db.AdminNotification) {
	_ = db.RecordAdminNotification(ctx, s.database, notification)
}
