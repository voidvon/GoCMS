package site

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

type siteUserAdminItem struct {
	MaxSessions int    `json:"max_sessions"`
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"display_name"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	LastLoginAt string `json:"last_login_at,omitempty"`
}

func (s *Server) adminSiteUsers(w http.ResponseWriter, r *http.Request) {
	user := s.currentAdmin(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录或账号已停用"})
		return
	}
	siteID, err := s.resolveSiteID(r, user)
	if err != nil || siteID <= 0 {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "无权访问该站点"})
		return
	}
	if !user.HasPermissionInSite(siteID, "members") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "当前用户组没有此会员管理权限"})
		return
	}

	if r.Method == http.MethodGet {
		s.listSiteUsers(w, r, siteID)
		return
	}
	if r.Method != http.MethodPatch && r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	var in struct {
		ID             int64  `json:"id"`
		Status         string `json:"status"`
		MaxSessions    *int   `json:"max_sessions"`
		RevokeSessions bool   `json:"revoke_sessions"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || in.ID <= 0 {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	status := strings.TrimSpace(in.Status)
	if in.MaxSessions != nil && (*in.MaxSessions < 1 || *in.MaxSessions > 100) {
		http.Error(w, "max_sessions must be between 1 and 100", 400)
		return
	}
	if r.Method != http.MethodDelete && status != "" && status != "active" && status != "disabled" && status != "pending" {
		http.Error(w, "invalid status", 400)
		return
	}
	tx, err := s.database.BeginTx(r.Context(), nil)
	if err != nil {
		accountError(w, err)
		return
	}
	defer tx.Rollback()
	query := "UPDATE gocms_user SET status=COALESCE(NULLIF(?,''),status), max_sessions=COALESCE(?,max_sessions), updated_at=CURRENT_TIMESTAMP WHERE id=? AND (site_id=? OR id IN (SELECT user_id FROM gocms_site_member WHERE site_id=?))"
	args := []any{status, in.MaxSessions, in.ID, siteID, siteID}
	if r.Method == http.MethodDelete {
		var userOriginSite int64
		if err := tx.QueryRowContext(r.Context(), `SELECT site_id FROM gocms_user WHERE id=?`, in.ID).Scan(&userOriginSite); err != nil {
			http.NotFound(w, r)
			return
		}
		if userOriginSite == siteID {
			query = "DELETE FROM gocms_user WHERE id=?"
			args = []any{in.ID}
		} else {
			query = "DELETE FROM gocms_site_member WHERE user_id=? AND site_id=?"
			args = []any{in.ID, siteID}
		}
	}
	result, err := tx.ExecContext(r.Context(), query, args...)
	if err != nil {
		accountError(w, err)
		return
	}
	n, err := result.RowsAffected()
	if err != nil {
		accountError(w, err)
		return
	}
	if n == 0 {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodDelete {
		_, _ = tx.ExecContext(r.Context(), "DELETE FROM gocms_site_member WHERE user_id=? AND site_id=?", in.ID, siteID)
		_, _ = tx.ExecContext(r.Context(), "DELETE FROM gocms_user_group_member WHERE user_id=? AND group_id IN (SELECT id FROM gocms_user_group WHERE site_id=?)", in.ID, siteID)
		_, _ = tx.ExecContext(r.Context(), "DELETE FROM gocms_user_session WHERE user_id=?", in.ID)
	} else if (status != "" && status != "active") || in.RevokeSessions {
		if _, err := tx.ExecContext(r.Context(), "DELETE FROM gocms_user_session WHERE user_id=? AND user_id IN (SELECT id FROM gocms_user WHERE id=? AND site_id=?)", in.ID, in.ID, siteID); err != nil {
			accountError(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		accountError(w, err)
		return
	}

	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) listSiteUsers(w http.ResponseWriter, r *http.Request, siteID int64) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page > 1000000 {
		page = 1000000
	}
	if page < 1 {
		page = 1
	}
	size, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if size < 1 || size > 100 {
		size = 30
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	like := "%" + q + "%"
	var total int64
	if err := s.database.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM gocms_user WHERE (site_id=? OR id IN (SELECT user_id FROM gocms_site_member WHERE site_id=?)) AND (?='' OR username LIKE ? OR email LIKE ? OR display_name LIKE ?)`, siteID, siteID, q, like, like, like).Scan(&total); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	rows, err := s.database.QueryContext(r.Context(), `SELECT id,username,email,display_name,status,max_sessions,created_at,COALESCE(last_login_at,'') FROM gocms_user WHERE (site_id=? OR id IN (SELECT user_id FROM gocms_site_member WHERE site_id=?)) AND (?='' OR username LIKE ? OR email LIKE ? OR display_name LIKE ?) ORDER BY id DESC LIMIT ? OFFSET ?`, siteID, siteID, q, like, like, like, size, (page-1)*size)

	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer rows.Close()
	items := []siteUserAdminItem{}
	for rows.Next() {
		var x siteUserAdminItem
		if err := rows.Scan(&x.ID, &x.Username, &x.Email, &x.DisplayName, &x.Status, &x.MaxSessions, &x.CreatedAt, &x.LastLoginAt); err != nil {
			http.Error(w, "database error", 500)
			return
		}
		items = append(items, x)
	}
	if err := rows.Err(); err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "page_size": size})
}
