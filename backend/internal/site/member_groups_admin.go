package site

import (
	"encoding/json"
	"gocms/internal/member"
	"net/http"
	"strings"
	"time"
)

type memberGroup struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
	Status      string `json:"status"`
	IsDefault   bool   `json:"is_default"`
}

func (s *Server) adminMemberGroups(w http.ResponseWriter, r *http.Request) {
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
		if r.URL.Query().Get("group_id") != "" {
			s.listMemberGroupMembers(w, r, siteID)
			return
		}
		rows, err := s.database.QueryContext(r.Context(), `SELECT id,name,slug,description,sort_order,status,is_default FROM gocms_user_group WHERE site_id=? ORDER BY sort_order,id`, siteID)
		if err != nil {
			accountError(w, err)
			return
		}
		defer rows.Close()
		out := []memberGroup{}
		for rows.Next() {
			var g memberGroup
			var isDef int
			if err := rows.Scan(&g.ID, &g.Name, &g.Slug, &g.Description, &g.SortOrder, &g.Status, &isDef); err != nil {
				accountError(w, err)
				return
			}
			g.IsDefault = isDef == 1
			out = append(out, g)
		}
		writeJSON(w, 200, map[string]any{"items": out})
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodPatch && r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	var in struct {
		ID          int64   `json:"id"`
		UserID      int64   `json:"user_id"`
		GroupID     int64   `json:"group_id"`
		Name        string  `json:"name"`
		Slug        string  `json:"slug"`
		Description string  `json:"description"`
		SortOrder   int     `json:"sort_order"`
		Status      string  `json:"status"`
		IsDefault   *bool   `json:"is_default"`
		ExpiresAt   *string `json:"expires_at"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		http.Error(w, "invalid payload", 400)
		return
	}
	if r.Method == http.MethodDelete {
		if in.ID > 0 {
			res, err := s.database.ExecContext(r.Context(), `DELETE FROM gocms_user_group WHERE id=? AND site_id=?`, in.ID, siteID)
			if err != nil {
				accountError(w, err)
				return
			}
			n, _ := res.RowsAffected()
			if n == 0 {
				http.NotFound(w, r)
				return
			}
			writeJSON(w, 200, map[string]bool{"ok": true})
			return
		}
		if in.UserID <= 0 || in.GroupID <= 0 {
			http.Error(w, "invalid payload", 400)
			return
		}
		res, err := s.database.ExecContext(r.Context(), `DELETE FROM gocms_user_group_member WHERE user_id=? AND group_id IN (SELECT id FROM gocms_user_group WHERE id=? AND site_id=?)`, in.UserID, in.GroupID, siteID)
		if err != nil {
			accountError(w, err)
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	if in.UserID > 0 {
		if in.GroupID <= 0 {
			http.Error(w, "invalid payload", 400)
			return
		}
		var validCount int
		err := s.database.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM gocms_user u, gocms_user_group g WHERE u.id=? AND (u.site_id=? OR EXISTS (SELECT 1 FROM json_each(u.site_ids) WHERE value=?)) AND g.id=? AND g.site_id=?`, in.UserID, siteID, siteID, in.GroupID, siteID).Scan(&validCount)
		if err != nil || validCount == 0 {
			http.Error(w, "用户或会员组不存在，或不属于当前站点", 400)
			return
		}

		if in.ExpiresAt != nil && *in.ExpiresAt != "" {
			end, err := member.ParseTime(*in.ExpiresAt)
			if err != nil {
				http.Error(w, "expires_at must be a valid timestamp", 400)
				return
			}
			normalized := end.UTC().Format(time.RFC3339)
			in.ExpiresAt = &normalized
		} else {
			in.ExpiresAt = nil
		}
		_, err = s.database.ExecContext(r.Context(), `INSERT INTO gocms_user_group_member(user_id,group_id,expires_at) VALUES(?,?,?) ON CONFLICT(user_id,group_id) DO UPDATE SET expires_at=excluded.expires_at,status='active'`, in.UserID, in.GroupID, in.ExpiresAt)
		if err != nil {
			accountError(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Slug = strings.TrimSpace(in.Slug)
	if in.Name == "" || in.Slug == "" {
		http.Error(w, "name and slug are required", 400)
		return
	}
	status := "active"
	if in.Status == "disabled" {
		status = "disabled"
	}
	tx, err := s.database.BeginTx(r.Context(), nil)
	if err != nil {
		accountError(w, err)
		return
	}
	defer tx.Rollback()

	if r.Method == http.MethodPatch {
		if in.ID <= 0 {
			http.Error(w, "invalid id", 400)
			return
		}
		var defVal int
		if in.IsDefault != nil {
			if *in.IsDefault {
				defVal = 1
			} else {
				defVal = 0
			}
		} else {
			_ = tx.QueryRowContext(r.Context(), `SELECT is_default FROM gocms_user_group WHERE id=? AND site_id=?`, in.ID, siteID).Scan(&defVal)
		}
		if defVal == 1 {
			if _, err := tx.ExecContext(r.Context(), `UPDATE gocms_user_group SET is_default=0 WHERE site_id=? AND id!=?`, siteID, in.ID); err != nil {
				accountError(w, err)
				return
			}
		}
		res, err := tx.ExecContext(r.Context(), `UPDATE gocms_user_group SET name=?,slug=?,description=?,sort_order=?,status=?,is_default=? WHERE id=? AND site_id=?`, in.Name, in.Slug, in.Description, in.SortOrder, status, defVal, in.ID, siteID)
		if err != nil {
			http.Error(w, "group already exists or update failed", 409)
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			http.NotFound(w, r)
			return
		}
	} else {
		isDefault := in.IsDefault != nil && *in.IsDefault
		if isDefault {
			if _, err := tx.ExecContext(r.Context(), `UPDATE gocms_user_group SET is_default=0 WHERE site_id=?`, siteID); err != nil {
				accountError(w, err)
				return
			}
		}
		defVal := 0
		if isDefault {
			defVal = 1
		}
		_, err = tx.ExecContext(r.Context(), `INSERT INTO gocms_user_group(site_id,name,slug,description,sort_order,status,is_default) VALUES(?,?,?,?,?,?,?)`, siteID, in.Name, in.Slug, in.Description, in.SortOrder, status, defVal)
		if err != nil {
			http.Error(w, "group already exists", 409)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, 201, map[string]bool{"ok": true})
}

func (s *Server) listMemberGroupMembers(w http.ResponseWriter, r *http.Request, siteID int64) {
	groupID := r.URL.Query().Get("group_id")
	rows, err := s.database.QueryContext(r.Context(), `SELECT u.id,u.username,u.email,u.display_name,m.status,m.expires_at FROM gocms_user_group_member m JOIN gocms_user_group g ON g.id=m.group_id JOIN gocms_user u ON u.id=m.user_id WHERE m.group_id=? AND g.site_id=? ORDER BY u.id DESC`, groupID, siteID)

	if err != nil {
		accountError(w, err)
		return
	}
	defer rows.Close()
	type item struct {
		ID          int64   `json:"id"`
		Username    string  `json:"username"`
		Email       string  `json:"email"`
		DisplayName string  `json:"display_name"`
		Status      string  `json:"status"`
		ExpiresAt   *string `json:"expires_at"`
	}
	out := []item{}
	for rows.Next() {
		var x item
		if err := rows.Scan(&x.ID, &x.Username, &x.Email, &x.DisplayName, &x.Status, &x.ExpiresAt); err != nil {
			accountError(w, err)
			return
		}
		out = append(out, x)
	}
	writeJSON(w, 200, map[string]any{"items": out})
}
