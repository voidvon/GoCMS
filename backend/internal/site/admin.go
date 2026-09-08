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

	"bilvie/internal/auth"
	"bilvie/internal/routing"
)

type adminCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type AdminUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Flags    string `json:"flags"`
}

type AdminStats struct {
	Products        int64 `json:"products"`
	VisibleProducts int64 `json:"visible_products"`
	News            int64 `json:"news"`
	Messages        int64 `json:"messages"`
	PendingMessages int64 `json:"pending_messages"`
}

type NewsItem struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Category    int64  `json:"category_id"`
	PublishedAt string `json:"published_at"`
	Picture     string `json:"picture"`
	Featured    int64  `json:"featured"`
}

type NewsDetail struct {
	NewsItem
	Content     string `json:"content"`
	Source      string `json:"source"`
	Keywords    string `json:"keywords"`
	Description string `json:"description"`
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
	ProductID int64  `json:"product_id"`
}

type CategoryItem struct {
	ID                int64  `json:"id"`
	Name              string `json:"name"`
	ParentID          int64  `json:"parent_id"`
	OrderID           int64  `json:"order_id"`
	ProductCount      int64  `json:"product_count"`
	ListPath          string `json:"list_path"`
	ListFilePattern   string `json:"list_file_pattern"`
	DetailPath        string `json:"detail_path"`
	DetailFilePattern string `json:"detail_file_pattern"`
}

type categoryPayload struct {
	Name              string `json:"name"`
	ParentID          int64  `json:"parent_id"`
	OrderID           int64  `json:"order_id"`
	ListPath          string `json:"list_path"`
	ListFilePattern   string `json:"list_file_pattern"`
	DetailPath        string `json:"detail_path"`
	DetailFilePattern string `json:"detail_file_pattern"`
}

type productPayload struct {
	Name     string `json:"name"`
	Code     string `json:"code"`
	Category int64  `json:"category_id"`
	Remark   string `json:"remark"`
	Content  string `json:"content"`
	SmallPic string `json:"small_pic"`
	BigPic   string `json:"big_pic"`
	Keywords string `json:"keywords"`
	OrderID  int64  `json:"order_id"`
	Featured int64  `json:"featured"`
	Visible  int64  `json:"visible"`
}

type newsPayload struct {
	Title       string `json:"title"`
	Content     string `json:"content"`
	Category    int64  `json:"category_id"`
	PublishedAt string `json:"published_at"`
	Source      string `json:"source"`
	Picture     string `json:"picture"`
	Keywords    string `json:"keywords"`
	Description string `json:"description"`
	Featured    int64  `json:"featured"`
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
	var user AdminUser
	var storedPassword string
	if err := s.database.QueryRowContext(request.Context(), `
		SELECT "Id", COALESCE("UserName", ''), COALESCE("Flag", ''), COALESCE("PassWord", '')
		FROM "benming_master" WHERE "UserName" = ?`, credentials.Username).
		Scan(&user.ID, &user.Username, &user.Flags, &storedPassword); err != nil || !auth.ComparePassword(credentials.Password, storedPassword) {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "用户名或密码不正确"})
		return
	}
	token, err := s.createSession(user.Username)
	if err != nil {
		http.Error(response, "session error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(response, sessionCookie(token, 86400))
	writeJSON(response, http.StatusOK, map[string]any{"user": user})
}

func (s *Server) adminLogout(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(response)
		return
	}
	if cookie, err := request.Cookie("bilvie_admin"); err == nil {
		_, _ = s.database.ExecContext(request.Context(), `DELETE FROM "bilvie_admin_session" WHERE "token" = ?`, cookie.Value)
	}
	http.SetCookie(response, sessionCookie("", -1))
	writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) adminSession(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(response)
		return
	}
	username, ok := s.adminUsername(request)
	if !ok {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "未登录"})
		return
	}
	var user AdminUser
	err := s.database.QueryRowContext(request.Context(), `
		SELECT "Id", COALESCE("UserName", ''), COALESCE("Flag", '')
		FROM "benming_master" WHERE "UserName" = ?`, username).Scan(&user.ID, &user.Username, &user.Flags)
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"user": user})
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
		{&stats.Products, `SELECT COUNT(*) FROM "benming_ch_prod"`},
		{&stats.VisibleProducts, `SELECT COUNT(*) FROM "benming_ch_prod" WHERE "show" = 1`},
		{&stats.News, `SELECT COUNT(*) FROM "benming_ch_news"`},
		{&stats.Messages, `SELECT COUNT(*) FROM "benming_ch_Msg"`},
		{&stats.PendingMessages, `SELECT COUNT(*) FROM "benming_ch_Msg" WHERE COALESCE("state", 0) = 0`},
	}
	for _, item := range queries {
		if err := s.database.QueryRowContext(request.Context(), item.query).Scan(item.destination); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
	}
	writeJSON(response, http.StatusOK, stats)
}

func (s *Server) adminProducts(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodPost {
		methodNotAllowed(response)
		return
	}
	if !s.requireAdmin(response, request) {
		return
	}
	if request.Method == http.MethodPost {
		s.saveProduct(response, request, 0)
		return
	}

	query := strings.TrimSpace(request.URL.Query().Get("q"))
	page := positiveInt(request.URL.Query().Get("page"), 1)
	pageSize := positiveInt(request.URL.Query().Get("page_size"), 20)
	if pageSize > 100 {
		pageSize = 100
	}
	where := "1 = 1"
	args := make([]any, 0, 3)
	if query != "" {
		where += ` AND ("prodName" LIKE ? OR "prodCode" LIKE ? OR "key" LIKE ?)`
		pattern := "%" + query + "%"
		args = append(args, pattern, pattern, pattern)
	}
	var total int64
	if err := s.database.QueryRowContext(request.Context(), `SELECT COUNT(*) FROM "benming_ch_prod" WHERE `+where, args...).Scan(&total); err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.database.QueryContext(request.Context(), `
		SELECT "id", COALESCE("prodName", ''), COALESCE("prodCode", ''), COALESCE("CatId", 0),
		       COALESCE("remark", ''), COALESCE("itemize", ''), COALESCE("smallpic", ''),
		       COALESCE("bigpic", ''), COALESCE("key", ''), COALESCE("orderid", 0),
		       COALESCE("tjhome", 0), COALESCE("show", 0)
		FROM "benming_ch_prod" WHERE `+where+` ORDER BY "id" DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	items := make([]Product, 0, pageSize)
	for rows.Next() {
		var item Product
		if err := rows.Scan(&item.ID, &item.Name, &item.Code, &item.Category, &item.Remark, &item.Content, &item.SmallPic, &item.BigPic, &item.Keywords, &item.OrderID, &item.Featured, &item.Visible); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(response, http.StatusOK, SearchResult{Query: query, Page: page, PageSize: pageSize, Total: total, Items: items})
}

func (s *Server) adminProduct(response http.ResponseWriter, request *http.Request, rawID string) {
	if request.Method != http.MethodPut && request.Method != http.MethodPatch && request.Method != http.MethodDelete {
		methodNotAllowed(response)
		return
	}
	if !s.requireAdmin(response, request) {
		return
	}
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id < 1 {
		http.Error(response, "invalid product id", http.StatusBadRequest)
		return
	}
	if request.Method == http.MethodDelete {
		if request.URL.Query().Get("delete") != "1" {
			_, err = s.database.ExecContext(request.Context(), `UPDATE "benming_ch_prod" SET "show" = 0 WHERE "id" = ?`, id)
		} else {
			var result sql.Result
			result, err = s.database.ExecContext(request.Context(), `DELETE FROM "benming_ch_prod" WHERE "id" = ?`, id)
			if err == nil {
				var affected int64
				affected, err = result.RowsAffected()
				if err == nil && affected == 0 {
					http.Error(response, "product not found", http.StatusNotFound)
					return
				}
			}
		}
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		s.contentSaved(response, request)
		return
	}
	s.saveProduct(response, request, id)
}

func (s *Server) saveProduct(response http.ResponseWriter, request *http.Request, id int64) {
	var payload productPayload
	if err := decodeRequest(request, &payload); err != nil {
		http.Error(response, "invalid product payload", http.StatusBadRequest)
		return
	}
	payload.Name = strings.TrimSpace(payload.Name)
	if payload.Name == "" {
		http.Error(response, "product name is required", http.StatusBadRequest)
		return
	}
	payload.SmallPic = canonicalImageURL(payload.SmallPic)
	payload.BigPic = canonicalImageURL(payload.BigPic)
	var err error
	if id == 0 {
		_, err = s.database.ExecContext(request.Context(), `
			INSERT INTO "benming_ch_prod" ("prodName", "prodCode", "CatId", "remark", "itemize", "smallpic", "bigpic", "key", "orderid", "tjhome", "show")
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, payload.Name, payload.Code, payload.Category, payload.Remark, payload.Content, payload.SmallPic, payload.BigPic, payload.Keywords, payload.OrderID, payload.Featured, payload.Visible)
	} else {
		_, err = s.database.ExecContext(request.Context(), `
			UPDATE "benming_ch_prod" SET "prodName" = ?, "prodCode" = ?, "CatId" = ?, "remark" = ?, "itemize" = ?,
			"smallpic" = ?, "bigpic" = ?, "key" = ?, "orderid" = ?, "tjhome" = ?, "show" = ? WHERE "id" = ?`,
			payload.Name, payload.Code, payload.Category, payload.Remark, payload.Content, payload.SmallPic, payload.BigPic, payload.Keywords, payload.OrderID, payload.Featured, payload.Visible, id)
	}
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	s.contentSaved(response, request)
}

func (s *Server) adminNews(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet || !s.requireAdmin(response, request) {
		return
	}
	query := strings.TrimSpace(request.URL.Query().Get("q"))
	page := positiveInt(request.URL.Query().Get("page"), 1)
	pageSize := positiveInt(request.URL.Query().Get("page_size"), 20)
	if pageSize > 100 {
		pageSize = 100
	}
	where := "1 = 1"
	args := make([]any, 0, 1)
	if query != "" {
		where += ` AND "Title" LIKE ?`
		args = append(args, "%"+query+"%")
	}
	var total int64
	if err := s.database.QueryRowContext(request.Context(), `SELECT COUNT(*) FROM "benming_ch_news" WHERE `+where, args...).Scan(&total); err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.database.QueryContext(request.Context(), `
		SELECT "newsid", COALESCE("Title", ''), COALESCE("Typeid", 0), COALESCE("Dateandtime", ''),
		       COALESCE("Picture", ''), COALESCE("tjhome", 0)
		FROM "benming_ch_news" WHERE `+where+` ORDER BY "newsid" DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	items := make([]NewsItem, 0, pageSize)
	for rows.Next() {
		var item NewsItem
		if err := rows.Scan(&item.ID, &item.Title, &item.Category, &item.PublishedAt, &item.Picture, &item.Featured); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		items = append(items, item)
	}
	writeJSON(response, http.StatusOK, map[string]any{"query": query, "page": page, "page_size": pageSize, "total": total, "items": items})
}

func (s *Server) adminNewsItem(response http.ResponseWriter, request *http.Request, rawID string) {
	if request.Method != http.MethodGet && request.Method != http.MethodPut && request.Method != http.MethodPatch && request.Method != http.MethodDelete {
		methodNotAllowed(response)
		return
	}
	if !s.requireAdmin(response, request) {
		return
	}
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id < 1 {
		http.Error(response, "invalid news id", http.StatusBadRequest)
		return
	}
	if request.Method == http.MethodGet {
		var item NewsDetail
		err = s.database.QueryRowContext(request.Context(), `
			SELECT "newsid", COALESCE("Title", ''), COALESCE("Typeid", 0), COALESCE("Dateandtime", ''),
			       COALESCE("Picture", ''), COALESCE("tjhome", 0), COALESCE("Content", ''), COALESCE("Nfrom", ''),
			       COALESCE("key", ''), COALESCE("desc", '')
			FROM "benming_ch_news" WHERE "newsid" = ?`, id).
			Scan(&item.ID, &item.Title, &item.Category, &item.PublishedAt, &item.Picture, &item.Featured, &item.Content, &item.Source, &item.Keywords, &item.Description)
		if err != nil {
			if err == sql.ErrNoRows {
				http.Error(response, "news not found", http.StatusNotFound)
				return
			}
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, item)
		return
	}
	if request.Method == http.MethodDelete {
		result, err := s.database.ExecContext(request.Context(), `DELETE FROM "benming_ch_news" WHERE "newsid" = ?`, id)
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
			http.Error(response, "news not found", http.StatusNotFound)
			return
		}
		s.contentSaved(response, request)
		return
	}
	s.saveNews(response, request, id)
}

func (s *Server) saveNews(response http.ResponseWriter, request *http.Request, id int64) {
	var payload newsPayload
	if err := decodeRequest(request, &payload); err != nil {
		http.Error(response, "invalid news payload", http.StatusBadRequest)
		return
	}
	payload.Title = strings.TrimSpace(payload.Title)
	if payload.Title == "" {
		http.Error(response, "news title is required", http.StatusBadRequest)
		return
	}
	payload.Picture = canonicalImageURL(payload.Picture)
	payload.PublishedAt = normalizeNewsDate(payload.PublishedAt)
	_, err := s.database.ExecContext(request.Context(), `
		UPDATE "benming_ch_news" SET "Title" = ?, "Content" = ?, "Typeid" = ?, "Nfrom" = ?,
		"Picture" = ?, "Dateandtime" = ?, "tjhome" = ?, "key" = ?, "desc" = ? WHERE "newsid" = ?`,
		payload.Title, payload.Content, payload.Category, payload.Source, payload.Picture, payload.PublishedAt,
		payload.Featured, payload.Keywords, payload.Description, id)
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	s.contentSaved(response, request)
}

func normalizeNewsDate(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == len("2006-01-02T15:04") && strings.Contains(value, "T") {
		return strings.Replace(value, "T", " ", 1) + ":00"
	}
	return value
}

func (s *Server) adminNewsCategories(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet || !s.requireAdmin(response, request) {
		return
	}
	rows, err := s.database.QueryContext(request.Context(), `
		SELECT "id", COALESCE("CatName", ''), COALESCE("Root", 0), COALESCE("ORderID", 0)
		FROM "benming_ch_NewsCat" ORDER BY "Root", "ORderID", "id"`)
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	items := make([]CategoryItem, 0)
	for rows.Next() {
		var item CategoryItem
		if err := rows.Scan(&item.ID, &item.Name, &item.ParentID, &item.OrderID); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		items = append(items, item)
	}
	writeJSON(response, http.StatusOK, items)
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
	if err := s.database.QueryRowContext(request.Context(), `SELECT COUNT(*) FROM "benming_ch_Msg"`).Scan(&total); err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	rows, err := s.database.QueryContext(request.Context(), `
		SELECT "id", COALESCE("Title", ''), COALESCE("linkren", ''), COALESCE("phone", ''), COALESCE("mobile", ''),
		       COALESCE("email", ''), COALESCE("address", ''), COALESCE("content", ''), COALESCE("date", ''),
		       COALESCE("state", 0), COALESCE("prodid", 0)
		FROM "benming_ch_Msg" ORDER BY "id" DESC LIMIT ? OFFSET ?`, pageSize, (page-1)*pageSize)
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	items := make([]MessageItem, 0, pageSize)
	for rows.Next() {
		var item MessageItem
		if err := rows.Scan(&item.ID, &item.Title, &item.Name, &item.Phone, &item.Mobile, &item.Email, &item.Address, &item.Content, &item.CreatedAt, &item.State, &item.ProductID); err != nil {
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
		result, err := s.database.ExecContext(request.Context(), `DELETE FROM "benming_ch_Msg" WHERE "id" = ?`, id)
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
	_, err = s.database.ExecContext(request.Context(), `UPDATE "benming_ch_Msg" SET "state" = ?, "statedate" = ? WHERE "id" = ?`, payload.State, time.Now().Format("2006-01-02 15:04:05"), id)
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
	rows, err := s.database.QueryContext(request.Context(), `
		SELECT c."id", COALESCE(c."CatName", ''), COALESCE(c."Root", 0), COALESCE(c."Orderid", 0), COUNT(p."id"),
		       COALESCE(c."ListPath", ''), COALESCE(c."ListFilePattern", ''),
		       COALESCE(c."DetailPath", ''), COALESCE(c."DetailFilePattern", '')
		FROM "benming_ch_ProdCat" c
		LEFT JOIN "benming_ch_prod" p ON p."CatId" = c."id"
		GROUP BY c."id", c."CatName", c."Root", c."Orderid", c."ListPath", c."ListFilePattern", c."DetailPath", c."DetailFilePattern"
		ORDER BY c."Root", c."Orderid", c."id"`)
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	items := make([]CategoryItem, 0)
	for rows.Next() {
		var item CategoryItem
		if err := rows.Scan(&item.ID, &item.Name, &item.ParentID, &item.OrderID, &item.ProductCount,
			&item.ListPath, &item.ListFilePattern, &item.DetailPath, &item.DetailFilePattern); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		items = append(items, item)
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
	if err := s.validateCategoryParent(request.Context(), id, payload.ParentID); err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.normalizeCategoryRoutes(request.Context(), id, &payload); err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	if id == 0 {
		if err := s.nextCategoryOrder(request.Context(), payload.ParentID, &payload.OrderID); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		_, err := s.database.ExecContext(request.Context(), `
			INSERT INTO "benming_ch_ProdCat"
			("CatName", "Root", "Orderid", "ListPath", "ListFilePattern", "DetailPath", "DetailFilePattern")
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			payload.Name, payload.ParentID, payload.OrderID, payload.ListPath, payload.ListFilePattern, payload.DetailPath, payload.DetailFilePattern)
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
	} else {
		result, err := s.database.ExecContext(request.Context(), `
			UPDATE "benming_ch_ProdCat" SET "CatName" = ?, "Root" = ?, "Orderid" = ?,
			"ListPath" = ?, "ListFilePattern" = ?, "DetailPath" = ?, "DetailFilePattern" = ? WHERE "id" = ?`,
			payload.Name, payload.ParentID, payload.OrderID, payload.ListPath, payload.ListFilePattern, payload.DetailPath, payload.DetailFilePattern, id)
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
			http.Error(response, "category not found", http.StatusNotFound)
			return
		}
	}
	s.contentSaved(response, request)
}

func (s *Server) normalizeCategoryRoutes(ctx context.Context, id int64, payload *categoryPayload) error {
	if id > 0 {
		var current categoryPayload
		err := s.database.QueryRowContext(ctx, `
			SELECT COALESCE("ListPath", ''), COALESCE("ListFilePattern", ''),
			       COALESCE("DetailPath", ''), COALESCE("DetailFilePattern", '')
			FROM "benming_ch_ProdCat" WHERE "id" = ?`, id).
			Scan(&current.ListPath, &current.ListFilePattern, &current.DetailPath, &current.DetailFilePattern)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("读取分类路由失败: %w", err)
		}
		if payload.ListPath == "" {
			payload.ListPath = current.ListPath
		}
		if payload.ListFilePattern == "" {
			payload.ListFilePattern = current.ListFilePattern
		}
		if payload.DetailPath == "" {
			payload.DetailPath = current.DetailPath
		}
		if payload.DetailFilePattern == "" {
			payload.DetailFilePattern = current.DetailFilePattern
		}
	}
	if payload.ListPath == "" {
		payload.ListPath = routing.DefaultListPath(payload.ParentID)
	}
	if payload.ListFilePattern == "" {
		payload.ListFilePattern = routing.DefaultListPattern
	}
	if payload.DetailPath == "" {
		payload.DetailPath = routing.DefaultDetailPath
	}
	if payload.DetailFilePattern == "" {
		payload.DetailFilePattern = routing.DefaultDetailPattern
	}
	var err error
	if payload.ListPath, err = routing.NormalizeDirectory(payload.ListPath); err != nil {
		return fmt.Errorf("列表目录无效: %w", err)
	}
	if payload.ListFilePattern, err = routing.NormalizeFilePattern(payload.ListFilePattern, false); err != nil {
		return fmt.Errorf("列表文件名规则无效: %w", err)
	}
	if payload.DetailPath, err = routing.NormalizeDirectory(payload.DetailPath); err != nil {
		return fmt.Errorf("详情目录无效: %w", err)
	}
	if payload.DetailFilePattern, err = routing.NormalizeFilePattern(payload.DetailFilePattern, false); err != nil {
		return fmt.Errorf("详情文件名规则无效: %w", err)
	}
	return nil
}

func (s *Server) deleteCategory(response http.ResponseWriter, request *http.Request, id int64) {
	var exists, children, products int64
	if err := s.database.QueryRowContext(request.Context(), `SELECT COUNT(*) FROM "benming_ch_ProdCat" WHERE "id" = ?`, id).Scan(&exists); err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	if exists == 0 {
		http.Error(response, "category not found", http.StatusNotFound)
		return
	}
	if err := s.database.QueryRowContext(request.Context(), `SELECT COUNT(*) FROM "benming_ch_ProdCat" WHERE "Root" = ?`, id).Scan(&children); err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	if children > 0 {
		http.Error(response, "分类下还有子分类，请先移动或删除子分类", http.StatusConflict)
		return
	}
	if err := s.database.QueryRowContext(request.Context(), `SELECT COUNT(*) FROM "benming_ch_prod" WHERE "CatId" = ?`, id).Scan(&products); err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	if products > 0 {
		http.Error(response, "分类下还有产品，请先调整产品分类", http.StatusConflict)
		return
	}
	if _, err := s.database.ExecContext(request.Context(), `DELETE FROM "benming_ch_ProdCat" WHERE "id" = ?`, id); err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	s.contentSaved(response, request)
}

func (s *Server) nextCategoryOrder(ctx context.Context, parentID int64, orderID *int64) error {
	if *orderID != 0 {
		return nil
	}
	var maxOrder sql.NullInt64
	if err := s.database.QueryRowContext(ctx, `SELECT MAX("Orderid") FROM "benming_ch_ProdCat" WHERE "Root" = ?`, parentID).Scan(&maxOrder); err != nil {
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
		if err := s.database.QueryRowContext(ctx, `SELECT COALESCE("Root", 0) FROM "benming_ch_ProdCat" WHERE "id" = ?`, current).Scan(&parent); err != nil {
			if err == sql.ErrNoRows {
				return fmt.Errorf("父分类不存在")
			}
			return fmt.Errorf("读取父分类失败: %w", err)
		}
		current = parent
	}
	return nil
}

func (s *Server) requireAdmin(response http.ResponseWriter, request *http.Request) bool {
	if _, ok := s.adminUsername(request); ok {
		return true
	}
	writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "未登录"})
	return false
}

func (s *Server) adminUsername(request *http.Request) (string, bool) {
	cookie, err := request.Cookie("bilvie_admin")
	if err != nil || cookie.Value == "" || s.database == nil {
		return "", false
	}
	var username string
	err = s.database.QueryRowContext(request.Context(), `
		SELECT "username" FROM "bilvie_admin_session"
		WHERE "token" = ? AND "expires_at" > ?`, cookie.Value, time.Now().Unix()).Scan(&username)
	if err != nil {
		return "", false
	}
	return username, true
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
		DELETE FROM "bilvie_admin_session" WHERE "expires_at" <= ?`, time.Now().Unix()); err != nil {
		return "", fmt.Errorf("clean expired sessions: %w", err)
	}
	if _, err := s.database.Exec(`
		INSERT INTO "bilvie_admin_session" ("token", "username", "expires_at") VALUES (?, ?, ?)`, token, username, expiresAt); err != nil {
		return "", fmt.Errorf("store admin session: %w", err)
	}
	return token, nil
}

func sessionCookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{Name: "bilvie_admin", Value: value, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: maxAge}
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
