package site

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"gocms/internal/auth"
)

// Module permissions follow EmpireCMS's user-group model. Account/group
// administration and executable updates remain exclusive to super administrators.
var adminPermissions = []struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}{
	{"content", "内容管理"}, {"categories", "栏目管理"},
	{"content.add", "新增内容"}, {"content.edit", "修改内容"},
	{"content.delete", "删除内容"}, {"content.review", "审核并公开内容"},
	{"media", "附件管理"}, {"messages", "信息反馈"},
	{"models", "系统模型"}, {"theme", "模板管理"},
	{"publish", "网站发布"}, {"languages", "语言配置"},
	{"logs", "查看操作日志"},
	{"login_logs", "查看登录日志"},
}

func (u *AdminUser) hasPermission(key string) bool {
	if u == nil {
		return false
	}
	if u.IsSuper {
		return true
	}
	for _, permission := range u.Permissions {
		if permission == key {
			return true
		}
	}
	return false
}

func (s *Server) loadAdmin(ctx context.Context, username string) (*AdminUser, error) {
	user := &AdminUser{Permissions: []string{}}
	var permissions, categories, groupSites, userSites, groupSitePerms sql.NullString
	err := s.database.QueryRowContext(ctx, `SELECT u.id, u.username, u.flags, u.is_super,
		u.group_id, COALESCE(g.permissions, '[]'), u.category_ids, g.site_ids, u.site_ids,
		COALESCE(g.site_permissions, '{}') FROM gocms_admin_user u
		LEFT JOIN gocms_admin_group g ON g.id = u.group_id
		WHERE u.username = ? AND u.disabled = 0`, username).
		Scan(&user.ID, &user.Username, &user.Flags, &user.IsSuper, &user.GroupID, &permissions, &categories, &groupSites, &userSites, &groupSitePerms)
	if err != nil {
		return nil, err
	}
	if permissions.Valid && permissions.String != "" {
		if err := json.Unmarshal([]byte(permissions.String), &user.Permissions); err != nil {
			return nil, err
		}
	}
	if categories.Valid && categories.String != "" && categories.String != "null" {
		_ = json.Unmarshal([]byte(categories.String), &user.CategoryIDs)
	}
	if groupSitePerms.Valid && groupSitePerms.String != "" && groupSitePerms.String != "{}" && groupSitePerms.String != "null" {
		var rawMap map[string][]string
		if err := json.Unmarshal([]byte(groupSitePerms.String), &rawMap); err == nil && len(rawMap) > 0 {
			user.SitePermissions = make(map[int64][]string, len(rawMap))
			var derivedSiteIDs []int64
			for k, v := range rawMap {
				if id, err := strconv.ParseInt(k, 10, 64); err == nil {
					user.SitePermissions[id] = v
					derivedSiteIDs = append(derivedSiteIDs, id)
				}
			}
			user.SiteIDs = derivedSiteIDs
		}
	}
	if user.SitePermissions == nil {
		var ids []int64
		hasGroupSites := groupSites.Valid && groupSites.String != "" && groupSites.String != "null"
		hasUserSites := userSites.Valid && userSites.String != "" && userSites.String != "null"
		if hasGroupSites {
			if err := json.Unmarshal([]byte(groupSites.String), &ids); err == nil {
				user.SiteIDs = ids
			}
		} else if hasUserSites {
			if err := json.Unmarshal([]byte(userSites.String), &ids); err == nil {
				user.SiteIDs = ids
			}
		} else {
			user.SiteIDs = nil
		}
	}
	return user, nil
}

type contentScopeKey struct{}

func contentScopeSQL(ctx context.Context) string {
	u, _ := ctx.Value(contentScopeKey{}).(*AdminUser)
	if u == nil || u.IsSuper || u.CategoryIDs == nil {
		return ""
	}
	// IDs are decoded as integers and encoded as JSON, never SQL text.
	raw, _ := json.Marshal(u.CategoryIDs)
	return ` AND category_id IN (SELECT value FROM json_each('` + string(raw) + `'))`
}

func (u *AdminUser) canManageCategory(id int64) bool {
	if u.IsSuper || u.CategoryIDs == nil {
		return true
	}
	for _, allowed := range u.CategoryIDs {
		if allowed == id {
			return true
		}
	}
	return false
}

func (s *Server) authorizeAdminRoute(w http.ResponseWriter, r *http.Request, route string) bool {
	u, ok, session := s.authenticateRequest(r)
	if !ok || u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录或账号已停用"})
		return false
	}
	module := strings.Split(strings.TrimPrefix(route, "/api/admin/"), "/")[0]
	if module == "logins" {
		module = "login_logs"
	}
	*r = *r.WithContext(context.WithValue(r.Context(), contentScopeKey{}, u))
	// API keys cannot administer accounts or groups even when owned by a super administrator.
	if module == "users" || module == "groups" || module == "site-users" || module == "member-groups" || (module == "sites" && r.Method != http.MethodGet) {
		if u.IsSuper && session {
			return true
		}
	} else if u.IsSuper {
		return true
	} else {
		siteID, _ := s.resolveSiteID(r, u)
		hasPerm := func(key string) bool {
			if siteID > 0 {
				return u.HasPermissionInSite(siteID, key)
			}
			return u.hasPermission(key)
		}
		if module == "content" {
			key := "content"
			switch r.Method {
			case http.MethodPost:
				key = "content.add"
			case http.MethodPut, http.MethodPatch:
				key = "content.edit"
			case http.MethodDelete:
				key = "content.delete"
			}
			if hasPerm("content") && hasPerm(key) {
				return true
			}
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "当前用户组没有此内容操作权限"})
			return false
		}
		switch module {
		case "session", "stats":
			return true
		case "sites", "languages", "categories", "models", "model-fields", "model-tables":
			// Editor selectors require read-only site, model, language and category metadata.
			if r.Method == http.MethodGet {
				return true
			}
		}
		switch module {
		case "template", "templates":
			module = "theme"
		case "model-fields", "model-tables":
			module = "models"
		case "feedback", "feedback-classes", "feedback-fields":
			module = "messages"
		}
		if hasPerm(module) {
			return true
		}
	}
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "当前用户组没有此操作权限"})
	return false
}

type adminGroup struct {
	ID              int64              `json:"id"`
	Name            string             `json:"name"`
	Permissions     []string           `json:"permissions"`
	SiteIDs         *[]int64           `json:"site_ids"`
	SitePermissions map[int64][]string `json:"site_permissions,omitempty"`
}

func (s *Server) adminGroups(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		rows, err := s.database.QueryContext(r.Context(), `SELECT id, name, permissions, site_ids, site_permissions FROM gocms_admin_group ORDER BY id`)
		if err != nil {
			accountError(w, err)
			return
		}
		defer rows.Close()
		items := []adminGroup{}
		for rows.Next() {
			var item adminGroup
			var raw string
			var rawSites, rawSitePerms sql.NullString
			if err := rows.Scan(&item.ID, &item.Name, &raw, &rawSites, &rawSitePerms); err != nil {
				accountError(w, err)
				return
			}
			if err := json.Unmarshal([]byte(raw), &item.Permissions); err != nil {
				accountError(w, err)
				return
			}
			if rawSites.Valid && rawSites.String != "" && rawSites.String != "null" {
				var sites []int64
				if err := json.Unmarshal([]byte(rawSites.String), &sites); err == nil {
					item.SiteIDs = &sites
				}
			}
			if rawSitePerms.Valid && rawSitePerms.String != "" && rawSitePerms.String != "{}" && rawSitePerms.String != "null" {
				var rawMap map[string][]string
				if err := json.Unmarshal([]byte(rawSitePerms.String), &rawMap); err == nil && len(rawMap) > 0 {
					item.SitePermissions = make(map[int64][]string, len(rawMap))
					var derivedSiteIDs []int64
					for k, v := range rawMap {
						if id, err := strconv.ParseInt(k, 10, 64); err == nil {
							item.SitePermissions[id] = v
							derivedSiteIDs = append(derivedSiteIDs, id)
						}
					}
					if item.SiteIDs == nil || len(*item.SiteIDs) == 0 {
						item.SiteIDs = &derivedSiteIDs
					}
				}
			}
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			accountError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "permissions": adminPermissions})
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	var input adminGroup
	if err := decodeRequest(r, &input); err != nil {
		accountInputError(w, "请求格式不正确")
		return
	}
	if r.Method != http.MethodPost && input.ID <= 0 {
		accountInputError(w, "用户组 ID 无效")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if r.Method != http.MethodDelete {
		if input.Name == "" || len(input.Name) > 120 {
			accountInputError(w, "请输入用户组名称（最多 120 字节）")
			return
		}
		for _, key := range input.Permissions {
			valid := false
			for _, permission := range adminPermissions {
				if permission.Key == key {
					valid = true
					break
				}
			}
			if !valid {
				accountInputError(w, "包含未知权限")
				return
			}
		}
	}
	tx, err := s.database.BeginTx(r.Context(), nil)
	if err != nil {
		accountError(w, err)
		return
	}
	defer tx.Rollback()
	if r.Method != http.MethodDelete && input.SiteIDs != nil {
		for _, siteID := range *input.SiteIDs {
			var count int
			if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM gocms_site WHERE id = ?`, siteID).Scan(&count); err != nil || count == 0 {
				accountInputError(w, "包含不存在的站点")
				return
			}
		}
	}
	var result sql.Result
	if r.Method == http.MethodDelete {
		var count int
		if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM gocms_admin_user WHERE group_id = ?`, input.ID).Scan(&count); err != nil {
			accountError(w, err)
			return
		}
		if count > 0 {
			accountInputError(w, "请先将该组的账号移至其他用户组")
			return
		}
		result, err = tx.ExecContext(r.Context(), `DELETE FROM gocms_admin_group WHERE id = ?`, input.ID)
	} else {
		var rawSitePerms = "{}"
		if input.SitePermissions != nil && len(input.SitePermissions) > 0 {
			rawMap := make(map[string][]string, len(input.SitePermissions))
			var derivedSiteIDs []int64
			permSet := make(map[string]bool)
			for siteID, perms := range input.SitePermissions {
				var count int
				if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM gocms_site WHERE id = ?`, siteID).Scan(&count); err != nil || count == 0 {
					accountInputError(w, "包含不存在的站点")
					return
				}
				for _, p := range perms {
					valid := false
					for _, ap := range adminPermissions {
						if ap.Key == p {
							valid = true
							break
						}
					}
					if !valid {
						accountInputError(w, "包含未知权限")
						return
					}
					permSet[p] = true
				}
				rawMap[strconv.FormatInt(siteID, 10)] = perms
				derivedSiteIDs = append(derivedSiteIDs, siteID)
			}
			b, _ := json.Marshal(rawMap)
			rawSitePerms = string(b)
			input.SiteIDs = &derivedSiteIDs
			if len(input.Permissions) == 0 {
				for p := range permSet {
					input.Permissions = append(input.Permissions, p)
				}
			}
		}

		if input.Permissions == nil {
			input.Permissions = []string{}
		}
		raw, _ := json.Marshal(input.Permissions)
		var duplicate int
		if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM gocms_admin_group WHERE name = ? AND id != ?`, input.Name, input.ID).Scan(&duplicate); err != nil {
			accountError(w, err)
			return
		}
		if duplicate > 0 {
			accountInputError(w, "用户组名称已存在")
			return
		}
		rawSites := "null"
		if input.SiteIDs != nil {
			b, _ := json.Marshal(*input.SiteIDs)
			rawSites = string(b)
		}
		if r.Method == http.MethodPost {
			result, err = tx.ExecContext(r.Context(), `INSERT INTO gocms_admin_group (name, permissions, site_ids, site_permissions) VALUES (?, ?, ?, ?)`, input.Name, string(raw), rawSites, rawSitePerms)
		} else {
			result, err = tx.ExecContext(r.Context(), `UPDATE gocms_admin_group SET name = ?, permissions = ?, site_ids = ?, site_permissions = ? WHERE id = ?`, input.Name, string(raw), rawSites, rawSitePerms, input.ID)
		}
	}
	if err != nil {
		accountError(w, err)
		return
	}
	if count, _ := result.RowsAffected(); count == 0 {
		http.NotFound(w, r)
		return
	}
	if err := tx.Commit(); err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type accountInput struct {
	CategoryIDs *[]int64 `json:"category_ids"`
	SiteIDs     *[]int64 `json:"site_ids"`
	ID          int64    `json:"id"`
	Username    string   `json:"username"`
	Password    string   `json:"password,omitempty"`
	GroupID     int64    `json:"group_id"`
	IsSuper     bool     `json:"is_super"`
	Disabled    bool     `json:"disabled"`
}

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		rows, err := s.database.QueryContext(r.Context(), `SELECT u.id, u.username, u.group_id, u.is_super, u.disabled, u.category_ids,
			COALESCE(g.site_ids, u.site_ids) FROM gocms_admin_user u
			LEFT JOIN gocms_admin_group g ON g.id = u.group_id
			ORDER BY u.id`)
		if err != nil {
			accountError(w, err)
			return
		}
		defer rows.Close()
		items := []accountInput{}
		for rows.Next() {
			var item accountInput
			var raw, rawSites sql.NullString
			if err := rows.Scan(&item.ID, &item.Username, &item.GroupID, &item.IsSuper, &item.Disabled, &raw, &rawSites); err != nil {
				accountError(w, err)
				return
			}
			if raw.Valid && raw.String != "" && raw.String != "null" {
				var cats []int64
				if err := json.Unmarshal([]byte(raw.String), &cats); err == nil {
					item.CategoryIDs = &cats
				}
			}
			if rawSites.Valid && rawSites.String != "" && rawSites.String != "null" {
				var sites []int64
				if err := json.Unmarshal([]byte(rawSites.String), &sites); err == nil {
					item.SiteIDs = &sites
				}
			}
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			accountError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	var input accountInput
	if err := decodeRequest(r, &input); err != nil {
		accountInputError(w, "请求格式不正确")
		return
	}
	if r.Method != http.MethodPost && input.ID <= 0 {
		accountInputError(w, "账号 ID 无效")
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	if r.Method != http.MethodDelete && (input.Username == "" || len(input.Username) > 80) {
		accountInputError(w, "请输入账号名称（最多 80 字节）")
		return
	}
	var hash string
	if r.Method != http.MethodDelete && (r.Method == http.MethodPost || input.Password != "") {
		if len(input.Password) < 8 || len(input.Password) > 256 {
			accountInputError(w, "密码长度应为 8–256 字节")
			return
		}
		var err error
		hash, err = auth.HashPassword(input.Password)
		if err != nil {
			accountError(w, err)
			return
		}
	}
	tx, err := s.database.BeginTx(r.Context(), nil)
	if err != nil {
		accountError(w, err)
		return
	}
	defer tx.Rollback()
	oldUsername := ""
	if input.CategoryIDs != nil {
		for _, id := range *input.CategoryIDs {
			var count int
			if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM gocms_category WHERE id = ?`, id).Scan(&count); err != nil {
				accountError(w, err)
				return
			}
			if count == 0 {
				accountInputError(w, "包含不存在的栏目")
				return
			}
		}
	}
	if input.SiteIDs != nil {
		for _, id := range *input.SiteIDs {
			var count int
			if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM gocms_site WHERE id = ?`, id).Scan(&count); err != nil {
				accountError(w, err)
				return
			}
			if count == 0 {
				accountInputError(w, "包含不存在的站点")
				return
			}
		}
	}
	if r.Method != http.MethodPost {
		var super, disabled bool
		err := tx.QueryRowContext(r.Context(), `SELECT username, is_super, disabled FROM gocms_admin_user WHERE id = ?`, input.ID).Scan(&oldUsername, &super, &disabled)
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			accountError(w, err)
			return
		}
		if super && !disabled && (r.Method == http.MethodDelete || !input.IsSuper || input.Disabled) {
			var count int
			if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM gocms_admin_user WHERE is_super = 1 AND disabled = 0`).Scan(&count); err != nil {
				accountError(w, err)
				return
			}
			if count <= 1 {
				accountInputError(w, "必须保留至少一个启用的超级管理员")
				return
			}
		}
	}
	if r.Method == http.MethodDelete {
		_, err = tx.ExecContext(r.Context(), `DELETE FROM gocms_admin_user WHERE id = ?`, input.ID)
	} else {
		if input.IsSuper {
			input.GroupID = 0
		} else {
			var count int
			if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM gocms_admin_group WHERE id = ?`, input.GroupID).Scan(&count); err != nil {
				accountError(w, err)
				return
			}
			if count == 0 {
				accountInputError(w, "请选择有效的用户组")
				return
			}
		}
		var count int
		if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM gocms_admin_user WHERE username = ? AND id != ?`, input.Username, input.ID).Scan(&count); err != nil {
			accountError(w, err)
			return
		}
		if count > 0 {
			accountInputError(w, "账号名称已存在")
			return
		}
		if r.Method == http.MethodPost {
			_, err = tx.ExecContext(r.Context(), `INSERT INTO gocms_admin_user (username, password_hash, group_id, is_super, disabled) VALUES (?, ?, ?, ?, ?)`, input.Username, hash, input.GroupID, input.IsSuper, input.Disabled)
		} else {
			_, err = tx.ExecContext(r.Context(), `UPDATE gocms_admin_user SET username = ?, group_id = ?, is_super = ?, disabled = ?, password_hash = CASE WHEN ? = '' THEN password_hash ELSE ? END WHERE id = ?`, input.Username, input.GroupID, input.IsSuper, input.Disabled, hash, hash, input.ID)
		}
	}
	if err != nil {
		accountError(w, err)
		return
	}
	if r.Method != http.MethodDelete {
		raw, _ := json.Marshal(input.CategoryIDs)
		rawSites, _ := json.Marshal(input.SiteIDs)
		if _, err := tx.ExecContext(r.Context(), `UPDATE gocms_admin_user SET category_ids = ?, site_ids = ? WHERE username = ?`, string(raw), string(rawSites), input.Username); err != nil {
			accountError(w, err)
			return
		}
	}
	if oldUsername != "" {
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM gocms_admin_session WHERE username = ?`, oldUsername); err != nil {
			accountError(w, err)
			return
		}
		// Changes to an account also revoke its outstanding machine credentials.
		if _, err := tx.ExecContext(r.Context(), `UPDATE gocms_api_key SET revoked_at = CURRENT_TIMESTAMP WHERE admin_id = ? AND revoked_at IS NULL`, input.ID); err != nil {
			accountError(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func accountInputError(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": message})
}

func accountError(w http.ResponseWriter, _ error) {
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "账号管理操作失败，请检查服务日志或重试"})
}
