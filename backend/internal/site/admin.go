package site

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gocms/internal/apikey"
	"gocms/internal/auth"
	"gocms/internal/db"
	"gocms/internal/routing"
	"gocms/internal/templateconfig"
)

type adminCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type AdminUser struct {
	CategoryIDs []int64  `json:"category_ids"`
	ID          int64    `json:"id"`
	Username    string   `json:"username"`
	Flags       string   `json:"flags"`
	IsSuper     bool     `json:"is_super"`
	GroupID     int64    `json:"group_id"`
	Permissions []string `json:"permissions"`
}

type AdminStats struct {
	Contents        int64 `json:"contents"`
	VisibleContents int64 `json:"visible_contents"`
	Messages        int64 `json:"messages"`
	PendingMessages int64 `json:"pending_messages"`
}

type MessageItem struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Name      string `json:"name"`
	Phone     string `json:"phone"`
	Mobile    string `json:"mobile"`
	Email     string `json:"email"`
	Address   string `json:"address"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
	State     int64  `json:"state"`
	ContentID int64  `json:"content_id"`
}

type CategoryItem struct {
	ID                int64                              `json:"id"`
	Name              string                             `json:"name"`
	ParentID          int64                              `json:"parent_id"`
	OrderID           int64                              `json:"order_id"`
	ListPageSize      int64                              `json:"list_page_size"`
	PageType          string                             `json:"page_type"`
	RouteID           int64                              `json:"route_id"`
	ContentCount      int64                              `json:"content_count"`
	ListPath          string                             `json:"list_path"`
	ListFilePattern   string                             `json:"list_file_pattern"`
	ListTemplate      string                             `json:"list_template"`
	CoverTemplate     string                             `json:"cover_template"`
	DetailPath        string                             `json:"detail_path"`
	DetailFilePattern string                             `json:"detail_file_pattern"`
	DetailTemplate    string                             `json:"detail_template"`
	Keywords          string                             `json:"keywords,omitempty"`
	Description       string                             `json:"description,omitempty"`
	CoverContent      string                             `json:"cover_content,omitempty"`
	ModelID           int64                              `json:"model_id"`
	Lang              string                             `json:"lang,omitempty"`
	IsFallback        bool                               `json:"is_fallback,omitempty"`
	FallbackLang      string                             `json:"fallback_lang,omitempty"`
	Translations      map[string]CategoryTranslationItem `json:"translations,omitempty"`
}

type categoryPayload struct {
	Lang              string                             `json:"lang,omitempty"`
	Translations      map[string]CategoryTranslationItem `json:"translations,omitempty"`
	Name              string                             `json:"name"`
	ParentID          int64                              `json:"parent_id"`
	OrderID           int64                              `json:"order_id"`
	ListPageSize      int64                              `json:"list_page_size"`
	PageType          string                             `json:"page_type"`
	ListPath          string                             `json:"list_path"`
	ListFilePattern   string                             `json:"list_file_pattern"`
	ListTemplate      string                             `json:"list_template"`
	CoverTemplate     string                             `json:"cover_template"`
	DetailPath        string                             `json:"detail_path"`
	DetailFilePattern string                             `json:"detail_file_pattern"`
	DetailTemplate    string                             `json:"detail_template"`
	Keywords          string                             `json:"keywords"`
	Description       string                             `json:"description"`
	CoverContent      string                             `json:"cover_content"`
	ModelID           int64                              `json:"model_id"`
}

func (s *Server) adminLogin(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(response)
		return
	}
	var credentials adminCredentials
	if err := decodeRequest(request, &credentials); err != nil {
		http.Error(response, "invalid login payload", http.StatusBadRequest)
		return
	}
	credentials.Username = strings.TrimSpace(credentials.Username)
	var recentFailures int
	if err := s.database.QueryRowContext(request.Context(), `SELECT COUNT(*) FROM gocms_admin_login WHERE success = 0 AND created_at >= datetime('now','-15 minutes') AND (username = ? OR ip = ?)`, credentials.Username, clientIP(request)).Scan(&recentFailures); err != nil {
		http.Error(response, "login temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	if recentFailures >= 5 {
		response.Header().Set("Retry-After", "900")
		writeJSON(response, http.StatusTooManyRequests, map[string]string{"error": "登录失败次数过多，请 15 分钟后重试"})
		return
	}
	var user AdminUser
	var storedPassword string
	if err := s.database.QueryRowContext(request.Context(), `
		SELECT "id", COALESCE("username", ''), COALESCE("flags", ''), COALESCE("password_hash", '')
		FROM "gocms_admin_user" WHERE "username" = ?`, credentials.Username).
		Scan(&user.ID, &user.Username, &user.Flags, &storedPassword); err != nil || !auth.ComparePassword(credentials.Password, storedPassword) {
		_, _ = s.database.ExecContext(request.Context(), `INSERT INTO gocms_admin_login (username, success, ip) VALUES (?, 0, ?)`, credentials.Username, clientIP(request))
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "用户名或密码不正确"})
		return
	}
	loaded, err := s.loadAdmin(request.Context(), user.Username)
	if err != nil {
		_, _ = s.database.ExecContext(request.Context(), `INSERT INTO gocms_admin_login (username, success, ip) VALUES (?, 0, ?)`, user.Username, clientIP(request))
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "用户名或密码不正确，或账号已停用"})
		return
	}
	user = *loaded
	token, err := s.createSession(user.Username)
	if err != nil {
		http.Error(response, "session error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(response, sessionCookie(token, 86400))
	_, _ = s.database.ExecContext(request.Context(), `INSERT INTO gocms_admin_login (username, success, ip) VALUES (?, 1, ?)`, user.Username, clientIP(request))
	writeJSON(response, http.StatusOK, map[string]any{"user": user})
}

func (s *Server) adminLogout(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(response)
		return
	}
	if cookie, err := request.Cookie("gocms_admin"); err == nil {
		_, _ = s.database.ExecContext(request.Context(), `DELETE FROM "gocms_admin_session" WHERE "token" = ?`, cookie.Value)
	}
	http.SetCookie(response, sessionCookie("", -1))
	writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) adminSession(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(response)
		return
	}
	user, ok, _ := s.authenticateRequest(request)
	if !ok || user == nil {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "未登录"})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"user": *user})
}

func (s *Server) adminStats(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet || !s.requireAdmin(response, request) {
		return
	}
	var stats AdminStats
	queries := []struct {
		destination *int64
		query       string
	}{
		{&stats.Contents, `SELECT COUNT(*) FROM "gocms_content"`},
		{&stats.VisibleContents, `SELECT COUNT(*) FROM "gocms_content" WHERE "visible" = 1`},
		{&stats.Messages, `SELECT COUNT(*) FROM "gocms_message"`},
		{&stats.PendingMessages, `SELECT COUNT(*) FROM "gocms_message" WHERE COALESCE("state", 0) = 0`},
	}
	for _, item := range queries {
		if err := s.database.QueryRowContext(request.Context(), item.query).Scan(item.destination); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
	}
	writeJSON(response, http.StatusOK, stats)
}

func (s *Server) adminMessages(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet || !s.requireAdmin(response, request) {
		return
	}
	page := positiveInt(request.URL.Query().Get("page"), 1)
	pageSize := positiveInt(request.URL.Query().Get("page_size"), 20)
	if pageSize > 100 {
		pageSize = 100
	}
	var total int64
	if err := s.database.QueryRowContext(request.Context(), `SELECT COUNT(*) FROM "gocms_message"`).Scan(&total); err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	rows, err := s.database.QueryContext(request.Context(), `
		SELECT "id", COALESCE("title", ''), COALESCE("name", ''), COALESCE("phone", ''), COALESCE("mobile", ''),
		       COALESCE("email", ''), COALESCE("address", ''), COALESCE("content", ''), COALESCE("created_at", ''),
		       COALESCE("state", 0), COALESCE("content_id", 0)
		FROM "gocms_message" ORDER BY "id" DESC LIMIT ? OFFSET ?`, pageSize, (page-1)*pageSize)
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	items := make([]MessageItem, 0, pageSize)
	for rows.Next() {
		var item MessageItem
		if err := rows.Scan(&item.ID, &item.Title, &item.Name, &item.Phone, &item.Mobile, &item.Email, &item.Address, &item.Content, &item.CreatedAt, &item.State, &item.ContentID); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		items = append(items, item)
	}
	writeJSON(response, http.StatusOK, map[string]any{"page": page, "page_size": pageSize, "total": total, "items": items})
}

func (s *Server) adminMessage(response http.ResponseWriter, request *http.Request, rawID string) {
	if request.Method != http.MethodPatch && request.Method != http.MethodDelete {
		methodNotAllowed(response)
		return
	}
	if !s.requireAdmin(response, request) {
		return
	}
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id < 1 {
		http.Error(response, "invalid message id", http.StatusBadRequest)
		return
	}
	if request.Method == http.MethodDelete {
		result, err := s.database.ExecContext(request.Context(), `DELETE FROM "gocms_message" WHERE "id" = ?`, id)
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		affected, err := result.RowsAffected()
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		if affected == 0 {
			http.Error(response, "message not found", http.StatusNotFound)
			return
		}
		writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	var payload struct {
		State int64 `json:"state"`
	}
	if err := decodeRequest(request, &payload); err != nil {
		http.Error(response, "invalid message payload", http.StatusBadRequest)
		return
	}
	if payload.State != 0 && payload.State != 1 {
		http.Error(response, "state must be 0 or 1", http.StatusBadRequest)
		return
	}
	_, err = s.database.ExecContext(request.Context(), `UPDATE "gocms_message" SET "state" = ? WHERE "id" = ?`, payload.State, id)
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) adminCategories(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodPost {
		methodNotAllowed(response)
		return
	}
	if !s.requireAdmin(response, request) {
		return
	}
	if request.Method == http.MethodPost {
		s.saveCategory(response, request, 0)
		return
	}
	requestedLang := strings.TrimSpace(request.URL.Query().Get("lang"))
	defaultLang, fallbackLang := s.getDefaultAndFallbackLang(request.Context())
	if requestedLang == "" {
		requestedLang = defaultLang
	}

	rows, err := s.database.QueryContext(request.Context(), `
		SELECT c."id", c."name", c."parent_id", c."order_id", c."list_page_size", c."page_type", c."route_id",
		       (SELECT COUNT(*) FROM "gocms_content" content WHERE content."category_id" = c."id"),
		       c."list_path", c."list_file_pattern", c."list_template", c."cover_template", c."detail_path",
		       c."detail_file_pattern", c."detail_template", COALESCE(c."keywords", ''), COALESCE(c."description", ''),
		       COALESCE(c."cover_content", ''), COALESCE(c."model_id", 1)
		FROM "gocms_category" c
		ORDER BY c."parent_id", c."order_id", c."id"`)
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	items := make([]CategoryItem, 0)
	for rows.Next() {
		var item CategoryItem
		if err := rows.Scan(&item.ID, &item.Name, &item.ParentID, &item.OrderID, &item.ListPageSize, &item.PageType, &item.RouteID,
			&item.ContentCount, &item.ListPath, &item.ListFilePattern, &item.ListTemplate,
			&item.CoverTemplate, &item.DetailPath, &item.DetailFilePattern, &item.DetailTemplate,
			&item.Keywords, &item.Description, &item.CoverContent, &item.ModelID); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		items = append(items, item)
	}
	_ = rows.Close()

	allTranslations, _ := s.loadAllCategoryTranslations(request.Context())
	for i := range items {
		translations := allTranslations[items[i].ID]
		if translations == nil {
			translations = make(map[string]CategoryTranslationItem)
		}
		if _, ok := translations[defaultLang]; !ok {
			translations[defaultLang] = CategoryTranslationItem{
				Name:         items[i].Name,
				Keywords:     items[i].Keywords,
				Description:  items[i].Description,
				CoverContent: items[i].CoverContent,
			}
		}

		if trans, ok := translations[requestedLang]; ok && strings.TrimSpace(trans.Name) != "" {
			items[i].Name = trans.Name
			items[i].Keywords = trans.Keywords
			items[i].Description = trans.Description
			items[i].CoverContent = trans.CoverContent
		} else {
			// FALLBACK to fallback language
			fallbackTrans := translations[fallbackLang]
			if strings.TrimSpace(fallbackTrans.Name) != "" {
				items[i].Name = fallbackTrans.Name
				items[i].Keywords = fallbackTrans.Keywords
				items[i].Description = fallbackTrans.Description
				items[i].CoverContent = fallbackTrans.CoverContent
				if requestedLang != fallbackLang {
					items[i].IsFallback = true
					items[i].FallbackLang = fallbackLang
				}
			}
		}
		items[i].Lang = requestedLang
		items[i].Translations = translations
	}
	if err := rows.Err(); err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(response, http.StatusOK, items)
}

func (s *Server) adminCategory(response http.ResponseWriter, request *http.Request, rawID string) {
	if request.Method != http.MethodPut && request.Method != http.MethodPatch && request.Method != http.MethodDelete {
		methodNotAllowed(response)
		return
	}
	if !s.requireAdmin(response, request) {
		return
	}
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id < 1 {
		http.Error(response, "invalid category id", http.StatusBadRequest)
		return
	}
	if request.Method == http.MethodDelete {
		s.deleteCategory(response, request, id)
		return
	}
	s.saveCategory(response, request, id)
}

func (s *Server) saveCategory(response http.ResponseWriter, request *http.Request, id int64) {
	var payload categoryPayload
	if err := decodeRequest(request, &payload); err != nil {
		http.Error(response, "invalid category payload", http.StatusBadRequest)
		return
	}
	payload.Name = strings.TrimSpace(payload.Name)
	if payload.Name == "" && payload.Translations != nil {
		defaultLang, _ := s.getDefaultAndFallbackLang(request.Context())
		if def, ok := payload.Translations[defaultLang]; ok && strings.TrimSpace(def.Name) != "" {
			payload.Name = strings.TrimSpace(def.Name)
			payload.Keywords = def.Keywords
			payload.Description = def.Description
			payload.CoverContent = def.CoverContent
		}
	}
	if payload.Name == "" {
		http.Error(response, "category name is required", http.StatusBadRequest)
		return
	}
	if payload.ParentID < 0 {
		payload.ParentID = 0
	}
	if payload.OrderID < 0 {
		payload.OrderID = 0
	}
	if payload.ModelID < 1 {
		payload.ModelID = 1
	}
	if err := s.normalizeCategoryRoutes(request.Context(), id, &payload); err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.validateCategoryParent(request.Context(), id, payload.ParentID); err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	requestedLang := strings.TrimSpace(payload.Lang)
	if requestedLang == "" {
		requestedLang = strings.TrimSpace(request.URL.Query().Get("lang"))
	}
	defaultLang, _ := s.getDefaultAndFallbackLang(request.Context())
	if requestedLang == "" {
		requestedLang = defaultLang
	}

	if id == 0 {
		if err := s.nextCategoryOrder(request.Context(), payload.ParentID, &payload.OrderID); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		result, err := s.database.ExecContext(request.Context(), `
			INSERT INTO "gocms_category"
			("name", "parent_id", "order_id", "list_page_size", "page_type", "route_id", "list_path", "list_file_pattern", "list_template", "cover_template", "detail_path", "detail_file_pattern", "detail_template", "keywords", "description", "cover_content", "model_id")
			VALUES (?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			payload.Name, payload.ParentID, payload.OrderID, payload.ListPageSize, payload.PageType, payload.ListPath, payload.ListFilePattern, payload.ListTemplate, payload.CoverTemplate, payload.DetailPath, payload.DetailFilePattern, payload.DetailTemplate, payload.Keywords, payload.Description, payload.CoverContent, payload.ModelID)
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		newID, err := result.LastInsertId()
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		if _, err := s.database.ExecContext(request.Context(), `UPDATE "gocms_category" SET "route_id" = ? WHERE "id" = ?`, newID, newID); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		if payload.Translations != nil && len(payload.Translations) > 0 {
			_ = s.saveCategoryTranslations(request.Context(), s.database, newID, payload.Translations)
		} else {
			_ = s.saveCategoryTranslations(request.Context(), s.database, newID, map[string]CategoryTranslationItem{
				requestedLang: {Name: payload.Name, Keywords: payload.Keywords, Description: payload.Description, CoverContent: payload.CoverContent},
			})
		}
	} else {
		if payload.Translations != nil && len(payload.Translations) > 0 {
			_ = s.saveCategoryTranslations(request.Context(), s.database, id, payload.Translations)
			if def, ok := payload.Translations[defaultLang]; ok && strings.TrimSpace(def.Name) != "" {
				payload.Name = def.Name
				payload.Keywords = def.Keywords
				payload.Description = def.Description
				payload.CoverContent = def.CoverContent
			}
		} else {
			_ = s.saveCategoryTranslations(request.Context(), s.database, id, map[string]CategoryTranslationItem{
				requestedLang: {Name: payload.Name, Keywords: payload.Keywords, Description: payload.Description, CoverContent: payload.CoverContent},
			})
		}

		if requestedLang == defaultLang || payload.Translations != nil {
			result, err := s.database.ExecContext(request.Context(), `
				UPDATE "gocms_category" SET "name" = ?, "parent_id" = ?, "order_id" = ?, "list_page_size" = ?, "page_type" = ?,
				"list_path" = ?, "list_file_pattern" = ?, "list_template" = ?, "cover_template" = ?, "detail_path" = ?, "detail_file_pattern" = ?, "detail_template" = ?, "keywords" = ?, "description" = ?, "cover_content" = ?, "model_id" = ? WHERE "id" = ?`,
				payload.Name, payload.ParentID, payload.OrderID, payload.ListPageSize, payload.PageType, payload.ListPath, payload.ListFilePattern, payload.ListTemplate, payload.CoverTemplate, payload.DetailPath, payload.DetailFilePattern, payload.DetailTemplate, payload.Keywords, payload.Description, payload.CoverContent, payload.ModelID, id)
			if err != nil {
				http.Error(response, "database error", http.StatusInternalServerError)
				return
			}
			affected, err := result.RowsAffected()
			if err != nil || affected == 0 {
				http.Error(response, "category not found", http.StatusNotFound)
				return
			}
		} else {
			result, err := s.database.ExecContext(request.Context(), `
				UPDATE "gocms_category" SET "parent_id" = ?, "order_id" = ?, "list_page_size" = ?, "page_type" = ?,
				"list_path" = ?, "list_file_pattern" = ?, "list_template" = ?, "cover_template" = ?, "detail_path" = ?, "detail_file_pattern" = ?, "detail_template" = ?, "model_id" = ? WHERE "id" = ?`,
				payload.ParentID, payload.OrderID, payload.ListPageSize, payload.PageType, payload.ListPath, payload.ListFilePattern, payload.ListTemplate, payload.CoverTemplate, payload.DetailPath, payload.DetailFilePattern, payload.DetailTemplate, payload.ModelID, id)
			if err != nil {
				http.Error(response, "database error", http.StatusInternalServerError)
				return
			}
			affected, err := result.RowsAffected()
			if err != nil || affected == 0 {
				http.Error(response, "category not found", http.StatusNotFound)
				return
			}
		}
	}
	s.contentSaved(response, request)
}

func (s *Server) normalizeCategoryRoutes(ctx context.Context, id int64, payload *categoryPayload) error {
	var current categoryPayload
	if id > 0 {
		err := s.database.QueryRowContext(ctx, `
			SELECT "list_page_size", "page_type", "list_path", "list_file_pattern", "list_template", "cover_template", "detail_path", "detail_file_pattern", "detail_template", COALESCE("model_id", 1)
			FROM "gocms_category" WHERE "id" = ?`, id).
			Scan(&current.ListPageSize, &current.PageType, &current.ListPath, &current.ListFilePattern, &current.ListTemplate, &current.CoverTemplate, &current.DetailPath, &current.DetailFilePattern, &current.DetailTemplate, &current.ModelID)
		if err == sql.ErrNoRows {
			return fmt.Errorf("分类不存在")
		}
		if err != nil {
			return fmt.Errorf("读取分类路由失败: %w", err)
		}
		if id > 0 && current.ModelID > 0 {
			payload.ModelID = current.ModelID
		} else if payload.ModelID < 1 {
			payload.ModelID = current.ModelID
		}
		if payload.ListPath == "" {
			payload.ListPath = current.ListPath
		}
		if payload.ListPageSize <= 0 {
			payload.ListPageSize = current.ListPageSize
		}
		if payload.PageType == "" {
			payload.PageType = current.PageType
		}
		if payload.ListFilePattern == "" {
			payload.ListFilePattern = current.ListFilePattern
		}
		if payload.ListTemplate == "" {
			payload.ListTemplate = current.ListTemplate
		}
		if payload.CoverTemplate == "" {
			payload.CoverTemplate = current.CoverTemplate
		}
		if payload.DetailPath == "" {
			payload.DetailPath = current.DetailPath
		}
		if payload.DetailFilePattern == "" {
			payload.DetailFilePattern = current.DetailFilePattern
		}
		if payload.DetailTemplate == "" {
			payload.DetailTemplate = current.DetailTemplate
		}
	}
	if payload.ListPageSize <= 0 {
		payload.ListPageSize = 14
	}
	if payload.ListPageSize > 200 {
		payload.ListPageSize = 200
	}
	var err error
	if payload.PageType, err = routing.NormalizePageType(payload.PageType); err != nil {
		return err
	}
	if payload.ListPath == "" && payload.PageType != routing.PageTypeCover {
		payload.ListPath = routing.DefaultListPath()
		if payload.ParentID > 0 {
			var parentPath string
			if err := s.database.QueryRowContext(ctx, `SELECT COALESCE("list_path", '') FROM "gocms_category" WHERE "id" = ?`, payload.ParentID).Scan(&parentPath); err == nil && parentPath != "" {
				payload.ListPath = parentPath
			}
		}
	}
	if payload.ListFilePattern == "" {
		payload.ListFilePattern = routing.DefaultListPattern
	}
	if payload.ListTemplate == "" {
		payload.ListTemplate = templateconfig.DefaultListTemplate
	}
	if payload.CoverTemplate == "" && payload.PageType == routing.PageTypeCover {
		payload.CoverTemplate = payload.ListTemplate
	}
	if payload.DetailPath == "" {
		if payload.ParentID > 0 {
			var parentPath string
			if err := s.database.QueryRowContext(ctx, `SELECT COALESCE("detail_path", '') FROM "gocms_category" WHERE "id" = ?`, payload.ParentID).Scan(&parentPath); err == nil && parentPath != "" {
				payload.DetailPath = parentPath
			}
		}
		if payload.DetailPath == "" {
			payload.DetailPath = routing.DefaultDetailPath
		}
	}
	if payload.DetailFilePattern == "" {
		payload.DetailFilePattern = routing.DefaultDetailPattern
	}
	if payload.DetailTemplate == "" {
		payload.DetailTemplate = templateconfig.DefaultDetailTemplate
	}
	if payload.PageType == routing.PageTypeCover {
		if payload.ListPath, err = routing.NormalizeOptionalDirectory(payload.ListPath); err != nil {
			return fmt.Errorf("栏目目录无效: %w", err)
		}
	} else if payload.ListPath, err = routing.NormalizeDirectory(payload.ListPath); err != nil {
		return fmt.Errorf("列表目录无效: %w", err)
	}
	if payload.PageType == routing.PageTypeCover {
		if payload.ListFilePattern == "" {
			payload.ListFilePattern = routing.DefaultCoverPattern
		}
		if payload.ListFilePattern, err = routing.NormalizeCoverFilePattern(payload.ListFilePattern); err != nil {
			return fmt.Errorf("封面文件名规则无效: %w", err)
		}
	} else if payload.ListFilePattern, err = routing.NormalizeFilePattern(payload.ListFilePattern, false); err != nil {
		return fmt.Errorf("列表文件名规则无效: %w", err)
	}
	if payload.DetailPath, err = routing.NormalizeDirectory(payload.DetailPath); err != nil {
		return fmt.Errorf("详情目录无效: %w", err)
	}
	if payload.DetailFilePattern, err = routing.NormalizeFilePattern(payload.DetailFilePattern, false); err != nil {
		return fmt.Errorf("详情文件名规则无效: %w", err)
	}
	if payload.ListTemplate, err = templateconfig.NormalizePath(payload.ListTemplate); err != nil {
		return fmt.Errorf("列表模板无效: %w", err)
	}
	if payload.DetailTemplate, err = templateconfig.NormalizePath(payload.DetailTemplate); err != nil {
		return fmt.Errorf("详情模板无效: %w", err)
	}
	if payload.CoverTemplate != "" {
		if payload.CoverTemplate, err = templateconfig.NormalizePath(payload.CoverTemplate); err != nil {
			return fmt.Errorf("封面模板无效: %w", err)
		}
	}
	if payload.PageType == routing.PageTypeCover && payload.CoverTemplate == "" {
		return fmt.Errorf("封面模板不能为空")
	}
	_, templateRoot := s.themePaths()
	if templateRoot != "" {
		templates := map[string]string{"详情": payload.DetailTemplate}
		if payload.PageType == routing.PageTypeCover {
			templates["封面"] = payload.CoverTemplate
		} else {
			templates["列表"] = payload.ListTemplate
		}
		for label, templatePath := range templates {
			if _, _, err := readThemeFile(templateRoot, ".html", templatePath); err != nil {
				return fmt.Errorf("%s模板不可用: %w", label, err)
			}
		}
	}
	return nil
}

func (s *Server) deleteCategory(response http.ResponseWriter, request *http.Request, id int64) {
	var exists, children, content int64
	if err := s.database.QueryRowContext(request.Context(), `SELECT COUNT(*) FROM "gocms_category" WHERE "id" = ?`, id).Scan(&exists); err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	if exists == 0 {
		http.Error(response, "category not found", http.StatusNotFound)
		return
	}
	if err := s.database.QueryRowContext(request.Context(), `SELECT COUNT(*) FROM "gocms_category" WHERE "parent_id" = ?`, id).Scan(&children); err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	if children > 0 {
		http.Error(response, "分类下还有子分类，请先移动或删除子分类", http.StatusConflict)
		return
	}
	if err := s.database.QueryRowContext(request.Context(), `
		SELECT COUNT(*) FROM "gocms_content" WHERE "category_id" = ?`, id).Scan(&content); err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	if content > 0 {
		http.Error(response, "栏目下还有内容，请先调整内容所属栏目", http.StatusConflict)
		return
	}
	if _, err := s.database.ExecContext(request.Context(), `DELETE FROM "gocms_category" WHERE "id" = ?`, id); err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	_, _ = s.database.ExecContext(request.Context(), `DELETE FROM "`+db.CategoryTranslationTable+`" WHERE "category_id" = ?`, id)
	s.contentSaved(response, request)
}

func (s *Server) nextCategoryOrder(ctx context.Context, parentID int64, orderID *int64) error {
	if *orderID != 0 {
		return nil
	}
	var maxOrder sql.NullInt64
	if err := s.database.QueryRowContext(ctx, `SELECT MAX("order_id") FROM "gocms_category" WHERE "parent_id" = ?`, parentID).Scan(&maxOrder); err != nil {
		return err
	}
	if maxOrder.Valid {
		*orderID = maxOrder.Int64 + 1
	}
	return nil
}

func (s *Server) validateCategoryParent(ctx context.Context, id, parentID int64) error {
	if id > 0 && id == parentID {
		return fmt.Errorf("分类不能设置自己为父分类")
	}
	seen := make(map[int64]bool)
	for current := parentID; current > 0; {
		if current == id {
			return fmt.Errorf("分类不能移动到自己的子分类下")
		}
		if seen[current] {
			return fmt.Errorf("分类层级存在循环引用")
		}
		seen[current] = true
		var parent int64
		if err := s.database.QueryRowContext(ctx, `SELECT COALESCE("parent_id", 0) FROM "gocms_category" WHERE "id" = ?`, current).Scan(&parent); err != nil {
			if err == sql.ErrNoRows {
				return fmt.Errorf("父分类不存在")
			}
			return fmt.Errorf("读取父分类失败: %w", err)
		}
		current = parent
	}
	return nil
}

func (s *Server) validateContentCategory(ctx context.Context, id int64) error {
	if id == 0 {
		return nil
	}
	var exists int64
	if err := s.database.QueryRowContext(ctx, `SELECT COUNT(*) FROM "gocms_category" WHERE "id" = ?`, id).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("栏目不存在")
		}
		return fmt.Errorf("读取栏目失败: %w", err)
	}
	if exists == 0 {
		return fmt.Errorf("栏目不存在")
	}
	return nil
}

func (s *Server) resolveApiKeyToken(request *http.Request) string {
	if headerValue := strings.TrimSpace(request.Header.Get("X-API-Key")); headerValue != "" {
		return headerValue
	}
	authHeader := strings.TrimSpace(request.Header.Get("Authorization"))
	if authHeader != "" {
		if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
			token := strings.TrimSpace(authHeader[7:])
			if strings.HasPrefix(token, apikey.KeyPrefix) || strings.HasPrefix(token, "gocms_live_") {
				return token
			}
		}
	}
	return ""
}

func (s *Server) adminSessionUser(request *http.Request) (*AdminUser, bool) {
	cookie, err := request.Cookie("gocms_admin")
	if err != nil || cookie.Value == "" || s.database == nil {
		return nil, false
	}
	var username string
	err = s.database.QueryRowContext(request.Context(), `
		SELECT "username" FROM "gocms_admin_session"
		WHERE "token" = ? AND "expires_at" > ?`, cookie.Value, time.Now().Unix()).
		Scan(&username)
	if err != nil || username == "" {
		return nil, false
	}
	user, err := s.loadAdmin(request.Context(), username)
	return user, err == nil
}

func (s *Server) authenticateRequest(request *http.Request) (*AdminUser, bool, bool) {
	if s.database == nil {
		return nil, false, false
	}
	if token := s.resolveApiKeyToken(request); token != "" {
		ident, err := apikey.Authenticate(request.Context(), s.database, token)
		if err == nil && ident != nil {
			go apikey.TouchUsage(context.Background(), s.database, ident.ApiKeyID, clientIP(request))
			user, err := s.loadAdmin(request.Context(), ident.Username)
			return user, err == nil, false
		}
		return nil, false, false
	}

	user, ok := s.adminSessionUser(request)
	if ok {
		return user, true, true
	}
	return nil, false, false
}

func (s *Server) requireAdmin(response http.ResponseWriter, request *http.Request) bool {
	if _, ok := s.adminUsername(request); ok {
		return true
	}
	writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "未登录"})
	return false
}

func (s *Server) requireAdminSession(response http.ResponseWriter, request *http.Request) (*AdminUser, bool) {
	user, ok, isSession := s.authenticateRequest(request)
	if !ok || !isSession || user == nil {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "请通过管理员后台登录操作"})
		return nil, false
	}
	return user, true
}

func (s *Server) adminUsername(request *http.Request) (string, bool) {
	user, ok, _ := s.authenticateRequest(request)
	if !ok || user == nil {
		return "", false
	}
	return user.Username, true
}

func (s *Server) createSession(username string) (string, error) {
	if s.database == nil {
		return "", fmt.Errorf("database is unavailable")
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	expiresAt := time.Now().Add(24 * time.Hour).Unix()
	if _, err := s.database.Exec(`
		DELETE FROM "gocms_admin_session" WHERE "expires_at" <= ?`, time.Now().Unix()); err != nil {
		return "", fmt.Errorf("clean expired sessions: %w", err)
	}
	if _, err := s.database.Exec(`
		INSERT INTO "gocms_admin_session" ("token", "username", "expires_at") VALUES (?, ?, ?)`, token, username, expiresAt); err != nil {
		return "", fmt.Errorf("store admin session: %w", err)
	}
	return token, nil
}

func sessionCookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{Name: "gocms_admin", Value: value, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: maxAge}
}

func decodeRequest(request *http.Request, destination any) error {
	if strings.HasPrefix(request.Header.Get("Content-Type"), "application/json") {
		return json.NewDecoder(request.Body).Decode(destination)
	}
	if err := request.ParseForm(); err != nil {
		return err
	}
	encoded, err := json.Marshal(request.Form)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, destination)
}
