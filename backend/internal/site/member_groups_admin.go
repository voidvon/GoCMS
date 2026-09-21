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
}

func (s *Server) adminMemberGroups(w http.ResponseWriter, r *http.Request) {
	user := s.currentAdmin(r)
	siteID, err := s.resolveSiteID(r, user)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	if r.Method == http.MethodGet {
		if r.URL.Query().Get("group_id") != "" {
			s.listMemberGroupMembers(w, r)
			return
		}
		rows, err := s.database.QueryContext(r.Context(), `SELECT id,name,slug,description,sort_order,status FROM gocms_user_group WHERE site_id=? ORDER BY sort_order,id`, siteID)
		if err != nil {
			accountError(w, err)
			return
		}
		defer rows.Close()
		out := []memberGroup{}
		for rows.Next() {
			var g memberGroup
			if err := rows.Scan(&g.ID, &g.Name, &g.Slug, &g.Description, &g.SortOrder, &g.Status); err != nil {
				accountError(w, err)
				return
			}
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
		ExpiresAt   *string `json:"expires_at"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		http.Error(w, "invalid payload", 400)
		return
	}
	if r.Method == http.MethodDelete {
		if in.ID > 0 {
			_, err := s.database.ExecContext(r.Context(), `DELETE FROM gocms_user_group WHERE id=?`, in.ID)
			if err != nil {
				accountError(w, err)
				return
			}
			writeJSON(w, 200, map[string]bool{"ok": true})
			return
		}
		if in.UserID <= 0 || in.GroupID <= 0 {
			http.Error(w, "invalid payload", 400)
			return
		}
		_, err := s.database.ExecContext(r.Context(), `DELETE FROM gocms_user_group_member WHERE user_id=? AND group_id=?`, in.UserID, in.GroupID)
		if err != nil {
			accountError(w, err)
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
		_, err := s.database.ExecContext(r.Context(), `INSERT INTO gocms_user_group_member(user_id,group_id,expires_at) VALUES(?,?,?) ON CONFLICT(user_id,group_id) DO UPDATE SET expires_at=excluded.expires_at,status='active'`, in.UserID, in.GroupID, in.ExpiresAt)
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
	if r.Method == http.MethodPatch {
		if in.ID <= 0 {
			http.Error(w, "invalid id", 400)
			return
		}
		_, err = s.database.ExecContext(r.Context(), `UPDATE gocms_user_group SET name=?,slug=?,description=?,sort_order=?,status=? WHERE id=?`, in.Name, in.Slug, in.Description, in.SortOrder, status, in.ID)
	} else {
		_, err = s.database.ExecContext(r.Context(), `INSERT INTO gocms_user_group(site_id,name,slug,description,sort_order,status) VALUES(?,?,?,?,?,?)`, siteID, in.Name, in.Slug, in.Description, in.SortOrder, status)
	}
	if err != nil {
		http.Error(w, "group already exists", 409)
		return
	}
	writeJSON(w, 201, map[string]bool{"ok": true})
}

func (s *Server) listMemberGroupMembers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.database.QueryContext(r.Context(), `SELECT u.id,u.username,u.email,u.display_name,m.status,m.expires_at FROM gocms_user_group_member m JOIN gocms_user u ON u.id=m.user_id WHERE m.group_id=? ORDER BY u.id DESC`, r.URL.Query().Get("group_id"))
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
