package site

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bilvie/internal/auth"
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
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	ParentID int64  `json:"parent_id"`
	OrderID  int64  `json:"order_id"`
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
		s.sessionsMu.Lock()
		delete(s.sessions, cookie.Value)
		s.sessionsMu.Unlock()
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
		_, err = s.database.ExecContext(request.Context(), `UPDATE "benming_ch_prod" SET "show" = 0 WHERE "id" = ?`, id)
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
	if request.Method != http.MethodPatch {
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
	if request.Method != http.MethodGet || !s.requireAdmin(response, request) {
		return
	}
	rows, err := s.database.QueryContext(request.Context(), `SELECT "id", COALESCE("CatName", ''), COALESCE("Root", 0), COALESCE("Orderid", 0) FROM "benming_ch_ProdCat" ORDER BY "Root", "Orderid", "id"`)
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

func (s *Server) requireAdmin(response http.ResponseWriter, request *http.Request) bool {
	if _, ok := s.adminUsername(request); ok {
		return true
	}
	writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "未登录"})
	return false
}

func (s *Server) adminUsername(request *http.Request) (string, bool) {
	cookie, err := request.Cookie("bilvie_admin")
	if err != nil || cookie.Value == "" {
		return "", false
	}
	s.sessionsMu.RLock()
	username, ok := s.sessions[cookie.Value]
	s.sessionsMu.RUnlock()
	return username, ok
}

func (s *Server) createSession(username string) (string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	s.sessionsMu.Lock()
	s.sessions[token] = username
	s.sessionsMu.Unlock()
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
