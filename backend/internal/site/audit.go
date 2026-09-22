package site

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type operationResponse struct {
	http.ResponseWriter
	status int
}

func (w *operationResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *operationResponse) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	if status >= 200 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *operationResponse) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (s *Server) recordOperation(username string, r *http.Request, result *operationResponse) {
	status := result.status
	if status == 0 {
		status = http.StatusOK
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	admin, _ := r.Context().Value(contentScopeKey{}).(*AdminUser)
	siteID, _ := s.resolveSiteID(r, admin)
	if siteID <= 0 {
		siteID = 1
	}
	// Never persist passwords, API keys, request bodies, or query strings.
	_, err := s.database.ExecContext(ctx, `INSERT INTO gocms_admin_operation
		(site_id, username, method, path, status, ip) VALUES (?, ?, ?, ?, ?, ?)`, siteID, username, r.Method, r.URL.Path, status, clientIP(r))
	if err != nil {
		log.Printf("record administrator operation: %v", err)
	}
}

type operationLog struct {
	ID        int64  `json:"id"`
	SiteID    int64  `json:"site_id"`
	Username  string `json:"username"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	Status    int    `json:"status"`
	IP        string `json:"ip"`
	CreatedAt string `json:"created_at"`
}

func (s *Server) adminOperationLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	user := s.currentAdmin(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录或账号已停用"})
		return
	}
	if !user.IsSuper && !user.hasPermission("logs") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "当前用户组没有此操作权限"})
		return
	}
	page := positiveInt(r.URL.Query().Get("page"), 1)
	if page > 1000000 {
		page = 1000000
	}
	const pageSize = 30
	username := strings.TrimSpace(r.URL.Query().Get("username"))
	where := "1=1"
	args := []any{}

	if !user.IsSuper {
		siteID, err := s.resolveSiteID(r, user)
		if err != nil || siteID <= 0 {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "无权访问该站点日志"})
			return
		}
		if !user.HasPermissionInSite(siteID, "logs") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "当前用户组没有此站点的日志查看权限"})
			return
		}
		where += " AND site_id = ?"
		args = append(args, siteID)
		if username != "" {
			where += " AND username = ?"
			args = append(args, username)
		}
	} else {
		if siteIDStr := strings.TrimSpace(r.URL.Query().Get("site_id")); siteIDStr != "" {
			if sid, err := strconv.ParseInt(siteIDStr, 10, 64); err == nil && sid > 0 {
				where += " AND site_id = ?"
				args = append(args, sid)
			}
		}
		if username != "" {
			where += " AND username = ?"
			args = append(args, username)
		}
	}

	var total int64
	if err := s.database.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM gocms_admin_operation WHERE `+where, args...).Scan(&total); err != nil {
		accountError(w, err)
		return
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.database.QueryContext(r.Context(), `SELECT id, site_id, username, method, path, status, ip, created_at FROM gocms_admin_operation WHERE `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		accountError(w, err)
		return
	}
	defer rows.Close()
	items := []operationLog{}
	for rows.Next() {
		var item operationLog
		if err := rows.Scan(&item.ID, &item.SiteID, &item.Username, &item.Method, &item.Path, &item.Status, &item.IP, &item.CreatedAt); err != nil {
			accountError(w, err)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "page_size": pageSize})
}

func (s *Server) adminLoginLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	user := s.currentAdmin(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录或账号已停用"})
		return
	}
	if !user.IsSuper && !user.hasPermission("login_logs") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "当前用户组没有此操作权限"})
		return
	}
	query := `SELECT id, username, success, ip, created_at FROM gocms_admin_login ORDER BY id DESC LIMIT 100`
	args := []any{}
	if !user.IsSuper {
		query = `SELECT id, username, success, ip, created_at FROM gocms_admin_login WHERE username = ? ORDER BY id DESC LIMIT 100`
		args = append(args, user.Username)
	}
	rows, err := s.database.QueryContext(r.Context(), query, args...)
	if err != nil {
		accountError(w, err)
		return
	}
	defer rows.Close()
	type item struct {
		ID        int64  `json:"id"`
		Username  string `json:"username"`
		Success   bool   `json:"success"`
		IP        string `json:"ip"`
		CreatedAt string `json:"created_at"`
	}
	items := []item{}
	for rows.Next() {
		var x item
		if err := rows.Scan(&x.ID, &x.Username, &x.Success, &x.IP, &x.CreatedAt); err != nil {
			accountError(w, err)
			return
		}
		items = append(items, x)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) adminClearLogs(w http.ResponseWriter, r *http.Request) {
	u, ok, session := s.authenticateRequest(r)
	if !ok || !session || u == nil || !u.IsSuper {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "清理日志需要超级管理员登录"})
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		Before string `json:"before"`
	}
	if err := decodeRequest(r, &input); err != nil || len(input.Before) != 10 {
		accountInputError(w, "日期格式必须为 YYYY-MM-DD")
		return
	}
	if _, err := time.Parse("2006-01-02", input.Before); err != nil {
		accountInputError(w, "日期格式必须为 YYYY-MM-DD")
		return
	}
	tx, err := s.database.BeginTx(r.Context(), nil)
	if err != nil {
		accountError(w, err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM gocms_admin_operation WHERE created_at < datetime(?, '+1 day')`, input.Before); err != nil {
		accountError(w, err)
		return
	}
	// Recent records also enforce login throttling; cleanup must not unlock it.
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM gocms_admin_login WHERE created_at < datetime(?, '+1 day') AND created_at < datetime('now', '-15 minutes')`, input.Before); err != nil {
		accountError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
