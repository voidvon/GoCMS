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

	var userOriginSite int64
	var currentStatus string
	err = tx.QueryRowContext(r.Context(), `
		SELECT u.site_id, u.status
		FROM gocms_user u
		LEFT JOIN gocms_site_member sm ON sm.user_id = u.id AND sm.site_id = ?
		WHERE u.id = ? AND (u.site_id = ? OR sm.site_id = ?)`,
		siteID, in.ID, siteID, siteID,
	).Scan(&userOriginSite, &currentStatus)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if r.Method == http.MethodDelete {
		var otherMemberships int
		_ = tx.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM gocms_site_member WHERE user_id = ? AND site_id != ?", in.ID, siteID).Scan(&otherMemberships)
		if (user.IsSuper || otherMemberships == 0) && userOriginSite == siteID {
			if _, err := tx.ExecContext(r.Context(), "DELETE FROM gocms_user WHERE id = ?", in.ID); err != nil {
				accountError(w, err)
				return
			}
			_, _ = tx.ExecContext(r.Context(), "DELETE FROM gocms_site_member WHERE user_id = ?", in.ID)
			_, _ = tx.ExecContext(r.Context(), "DELETE FROM gocms_user_group_member WHERE user_id = ?", in.ID)
			_, _ = tx.ExecContext(r.Context(), "DELETE FROM gocms_user_session WHERE user_id = ?", in.ID)
		} else {
			if _, err := tx.ExecContext(r.Context(), "DELETE FROM gocms_site_member WHERE user_id = ? AND site_id = ?", in.ID, siteID); err != nil {
				accountError(w, err)
				return
			}
			_, _ = tx.ExecContext(r.Context(), "DELETE FROM gocms_user_group_member WHERE user_id = ? AND group_id IN (SELECT id FROM gocms_user_group WHERE site_id = ?)", in.ID, siteID)
			if userOriginSite == siteID {
				var nextSiteID int64
				if err := tx.QueryRowContext(r.Context(), "SELECT site_id FROM gocms_site_member WHERE user_id = ? LIMIT 1", in.ID).Scan(&nextSiteID); err == nil && nextSiteID > 0 {
					_, _ = tx.ExecContext(r.Context(), "UPDATE gocms_user SET site_id = ? WHERE id = ?", nextSiteID, in.ID)
				}
			}
		}
	} else {
		if status != "" {
			_, err = tx.ExecContext(r.Context(), `
				INSERT OR IGNORE INTO gocms_site_member (site_id, user_id, status)
				SELECT ?, id, ? FROM gocms_user WHERE id = ?`,
				siteID, status, in.ID,
			)
			if err != nil {
				accountError(w, err)
				return
			}
			_, err = tx.ExecContext(r.Context(), "UPDATE gocms_site_member SET status = ? WHERE site_id = ? AND user_id = ?", status, siteID, in.ID)
			if err != nil {
				accountError(w, err)
				return
			}
			if userOriginSite == siteID {
				_, err = tx.ExecContext(r.Context(), "UPDATE gocms_user SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", status, in.ID)
				if err != nil {
					accountError(w, err)
					return
				}
			}
		}

		if in.MaxSessions != nil && (user.IsSuper || userOriginSite == siteID) {
			_, err = tx.ExecContext(r.Context(), "UPDATE gocms_user SET max_sessions = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", *in.MaxSessions, in.ID)
			if err != nil {
				accountError(w, err)
				return
			}
		}

		if (status != "" && status != "active") || in.RevokeSessions {
			if userOriginSite == siteID {
				if _, err := tx.ExecContext(r.Context(), "DELETE FROM gocms_user_session WHERE user_id = ?", in.ID); err != nil {
					accountError(w, err)
					return
				}
			}
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
	countQuery := `
		SELECT COUNT(DISTINCT u.id)
		FROM gocms_user u
		LEFT JOIN gocms_site_member sm ON sm.user_id = u.id AND sm.site_id = ?
		WHERE (u.site_id = ? OR sm.site_id = ?)
		  AND (? = '' OR u.username LIKE ? OR u.email LIKE ? OR u.display_name LIKE ?)`
	if err := s.database.QueryRowContext(r.Context(), countQuery, siteID, siteID, siteID, q, like, like, like).Scan(&total); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	selectQuery := `
		SELECT u.id, u.username, u.email, u.display_name,
		       CASE WHEN u.status != 'active' THEN u.status WHEN sm.status IS NOT NULL THEN sm.status ELSE u.status END AS effective_status,
		       u.max_sessions, u.created_at, COALESCE(u.last_login_at, '')
		FROM gocms_user u
		LEFT JOIN gocms_site_member sm ON sm.user_id = u.id AND sm.site_id = ?
		WHERE (u.site_id = ? OR sm.site_id = ?)
		  AND (? = '' OR u.username LIKE ? OR u.email LIKE ? OR u.display_name LIKE ?)
		ORDER BY u.id DESC LIMIT ? OFFSET ?`
	rows, err := s.database.QueryContext(r.Context(), selectQuery, siteID, siteID, siteID, q, like, like, like, size, (page-1)*size)
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
