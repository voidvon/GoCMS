package site

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"bilvie/internal/auth"
)

type Server struct {
	database     *sql.DB
	siteRoot     string
	fileServe    http.Handler
	frontendRoot string
	assetsRoot   string
	themeRoot    string
	publication  *publication
}

type Product struct {
	ID       int64  `json:"id"`
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

type SearchResult struct {
	Query    string    `json:"query"`
	Page     int       `json:"page"`
	PageSize int       `json:"page_size"`
	Total    int64     `json:"total"`
	Items    []Product `json:"items"`
}

var htmlTagPattern = regexp.MustCompile(`(?s)<[^>]*>`)

func New(database *sql.DB, siteRoot string) (*Server, error) {
	if database != nil {
		if _, err := database.Exec(`
			CREATE TABLE IF NOT EXISTS "bilvie_admin_session" (
				"token" TEXT PRIMARY KEY,
				"username" TEXT NOT NULL,
				"expires_at" INTEGER NOT NULL
			)`); err != nil {
			return nil, fmt.Errorf("create admin session table: %w", err)
		}
		if _, err := database.Exec(`CREATE INDEX IF NOT EXISTS "idx_bilvie_admin_session_expiry" ON "bilvie_admin_session" ("expires_at")`); err != nil {
			return nil, fmt.Errorf("create admin session index: %w", err)
		}
	}
	return &Server{
		database:  database,
		siteRoot:  siteRoot,
		fileServe: http.FileServer(http.Dir(siteRoot)),
	}, nil
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serveHTTP)
}

func (s *Server) serveHTTP(response http.ResponseWriter, request *http.Request) {
	cleanPath := request.URL.Path
	if cleanPath != "/" {
		cleanPath = strings.TrimSuffix(cleanPath, "/")
	}
	if s.frontendRoot != "" && (cleanPath == "/admin" || strings.HasPrefix(cleanPath, "/admin/")) {
		s.serveAdminApp(response, request)
		return
	}
	switch strings.ToLower(cleanPath) {
	case "/api/admin/publish":
		s.adminPublish(response, request)
	case "/api/health":
		s.health(response, request)
	case "/api/admin/login":
		s.adminLogin(response, request)
	case "/api/admin/logout":
		s.adminLogout(response, request)
	case "/api/admin/session":
		s.adminSession(response, request)
	case "/api/admin/stats":
		s.adminStats(response, request)
	case "/api/admin/products":
		s.adminProducts(response, request)
	case "/api/admin/news":
		s.adminNews(response, request)
	case "/api/admin/news-categories":
		s.adminNewsCategories(response, request)
	case "/api/admin/messages":
		s.adminMessages(response, request)
	case "/api/admin/categories":
		s.adminCategories(response, request)
	case "/api/search":
		s.searchJSON(response, request)
	case "/api/products":
		s.productsJSON(response, request)
	case "/api/messages":
		s.messages(response, request, false)
	case "/search.asp":
		s.searchHTML(response, request)
	case "/ajaxcode/prodmsg.asp", "/ajaxcode/msg.asp":
		s.messages(response, request, true)
	case "/bil/login.asp", "/admin/login":
		s.loginPage(response, request)
	case "/bil/check.asp", "/admin/login/check":
		s.checkLogin(response, request)
	case "/bil/index.asp", "/admin":
		s.adminPage(response, request)
	default:
		lowerPath := strings.ToLower(cleanPath)
		if strings.HasPrefix(lowerPath, "/api/admin/products/") {
			s.adminProduct(response, request, cleanPath[len("/api/admin/products/"):])
			return
		}
		if strings.HasPrefix(lowerPath, "/api/admin/news/") {
			s.adminNewsItem(response, request, cleanPath[len("/api/admin/news/"):])
			return
		}
		if strings.HasPrefix(lowerPath, "/api/admin/messages/") {
			s.adminMessage(response, request, cleanPath[len("/api/admin/messages/"):])
			return
		}
		if strings.HasPrefix(lowerPath, "/api/admin/categories/") {
			s.adminCategory(response, request, cleanPath[len("/api/admin/categories/"):])
			return
		}
		if strings.HasPrefix(strings.ToLower(cleanPath), "/api/products/") {
			s.productJSON(response, request, cleanPath[len("/api/products/"):])
			return
		}
		s.staticFile(response, request)
	}
}

func (s *Server) health(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(response)
		return
	}
	var productCount, newsCount int64
	if err := s.database.QueryRowContext(request.Context(), `SELECT COUNT(*) FROM "benming_ch_prod"`).Scan(&productCount); err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	if err := s.database.QueryRowContext(request.Context(), `SELECT COUNT(*) FROM "benming_ch_news"`).Scan(&newsCount); err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"ok":       true,
		"database": "sqlite",
		"products": productCount,
		"news":     newsCount,
	})
}

func (s *Server) searchJSON(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(response)
		return
	}
	query := strings.TrimSpace(request.URL.Query().Get("q"))
	result, err := s.searchProducts(request, query, request.URL.Query().Get("page"), request.URL.Query().Get("page_size"))
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (s *Server) productsJSON(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(response)
		return
	}
	query := strings.TrimSpace(request.URL.Query().Get("q"))
	result, err := s.searchProducts(request, query, request.URL.Query().Get("page"), request.URL.Query().Get("page_size"))
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (s *Server) productJSON(response http.ResponseWriter, request *http.Request, rawID string) {
	if request.Method != http.MethodGet {
		methodNotAllowed(response)
		return
	}
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id < 1 {
		http.Error(response, "invalid product id", http.StatusBadRequest)
		return
	}
	product, err := s.product(request, id)
	if err == sql.ErrNoRows {
		http.NotFound(response, request)
		return
	}
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(response, http.StatusOK, product)
}

func (s *Server) searchProducts(request *http.Request, query, rawPage, rawPageSize string) (SearchResult, error) {
	page := positiveInt(rawPage, 1)
	pageSize := positiveInt(rawPageSize, 6)
	if pageSize > 50 {
		pageSize = 50
	}
	where := `"show" = 1`
	args := make([]any, 0, 5)
	if query != "" && query != "输入产品名称" {
		where += ` AND ("prodName" LIKE ? OR "prodCode" LIKE ? OR "key" LIKE ?)`
		pattern := "%" + query + "%"
		args = append(args, pattern, pattern, pattern)
	} else {
		query = ""
	}

	var total int64
	if err := s.database.QueryRowContext(request.Context(), `SELECT COUNT(*) FROM "benming_ch_prod" WHERE `+where, args...).Scan(&total); err != nil {
		return SearchResult{}, err
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.database.QueryContext(request.Context(), `
		SELECT "id", COALESCE("prodName", ''), COALESCE("prodCode", ''), COALESCE("CatId", 0),
		       COALESCE("remark", ''), COALESCE("itemize", ''), COALESCE("smallpic", ''),
		       COALESCE("bigpic", ''), COALESCE("key", ''), COALESCE("orderid", 0),
		       COALESCE("tjhome", 0), COALESCE("show", 0)
		FROM "benming_ch_prod"
		WHERE `+where+`
		ORDER BY "id" DESC
		LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return SearchResult{}, err
	}
	defer rows.Close()

	items := make([]Product, 0, pageSize)
	for rows.Next() {
		var item Product
		if err := rows.Scan(&item.ID, &item.Name, &item.Code, &item.Category, &item.Remark, &item.Content, &item.SmallPic, &item.BigPic, &item.Keywords, &item.OrderID, &item.Featured, &item.Visible); err != nil {
			return SearchResult{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return SearchResult{}, err
	}
	return SearchResult{Query: query, Page: page, PageSize: pageSize, Total: total, Items: items}, nil
}

func (s *Server) product(request *http.Request, id int64) (Product, error) {
	var item Product
	err := s.database.QueryRowContext(request.Context(), `
		SELECT "id", COALESCE("prodName", ''), COALESCE("prodCode", ''), COALESCE("CatId", 0),
		       COALESCE("remark", ''), COALESCE("itemize", ''), COALESCE("smallpic", ''),
		       COALESCE("bigpic", ''), COALESCE("key", ''), COALESCE("orderid", 0),
		       COALESCE("tjhome", 0), COALESCE("show", 0)
		FROM "benming_ch_prod" WHERE "id" = ? AND "show" = 1`, id).
		Scan(&item.ID, &item.Name, &item.Code, &item.Category, &item.Remark, &item.Content, &item.SmallPic, &item.BigPic, &item.Keywords, &item.OrderID, &item.Featured, &item.Visible)
	return item, err
}

func (s *Server) searchHTML(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodPost {
		methodNotAllowed(response)
		return
	}
	if err := request.ParseForm(); err != nil {
		http.Error(response, "invalid form", http.StatusBadRequest)
		return
	}
	query := strings.TrimSpace(request.FormValue("ProductsName"))
	result, err := s.searchProducts(request, query, request.FormValue("page"), "6")
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}

	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(response, `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>产品搜索</title><link rel="stylesheet" href="/css/c.css"><style>body{font-family:Arial,"Microsoft YaHei",sans-serif;margin:0;color:#333}.wrap{width:min(980px,calc(100% - 32px));margin:32px auto}.search{display:flex;gap:8px;margin:20px 0}.search input{flex:1;padding:10px;border:1px solid #ccc}.search button{padding:10px 20px}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(300px,1fr));gap:16px}.item{display:flex;gap:12px;border-bottom:1px dashed #ccc;padding:12px 0}.item img{width:120px;height:90px;object-fit:contain}.item h2{font-size:16px;margin:0 0 8px}.item p{font-size:13px;line-height:1.6;margin:0}.pages{margin:24px 0}.pages a{margin-right:12px}</style></head><body><main class="wrap"><a href="/">返回首页</a><h1>产品搜索</h1><form class="search" method="post" action="/search.asp?action=search"><input name="ProductsName" value="`)
	fmt.Fprint(response, html.EscapeString(query))
	fmt.Fprint(response, `" placeholder="输入产品名称"><button type="submit">搜索</button></form>`)
	if query != "" {
		fmt.Fprintf(response, `<p>“%s”共找到 %d 条产品</p>`, html.EscapeString(query), result.Total)
	} else {
		fmt.Fprintf(response, `<p>共 %d 条产品</p>`, result.Total)
	}
	fmt.Fprint(response, `<section class="grid">`)
	for _, item := range result.Items {
		image := item.SmallPic
		if image == "" {
			image = "/images/index_NewsPic.jpg"
		}
		productURL, err := s.productDetailURL(request.Context(), item.Category, item.ID)
		if err != nil {
			http.Error(response, "product route error", http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(response, `<article class="item"><img src="%s" alt="%s"><div><h2><a href="%s">%s</a></h2><p>%s</p></div></article>`, html.EscapeString(image), html.EscapeString(item.Name), html.EscapeString(productURL), html.EscapeString(item.Name), html.EscapeString(snippet(item.Remark, 150)))
	}
	fmt.Fprint(response, `</section><nav class="pages">`)
	lastPage := (result.Total + int64(result.PageSize) - 1) / int64(result.PageSize)
	if result.Page > 1 {
		fmt.Fprintf(response, `<a href="/search.asp?action=search&ProductsName=%s&page=%d">上一页</a>`, html.EscapeString(query), result.Page-1)
	}
	if int64(result.Page) < lastPage {
		fmt.Fprintf(response, `<a href="/search.asp?action=search&ProductsName=%s&page=%d">下一页</a>`, html.EscapeString(query), result.Page+1)
	}
	fmt.Fprint(response, `</nav></main></body></html>`)
}

func (s *Server) messages(response http.ResponseWriter, request *http.Request, legacy bool) {
	if request.Method != http.MethodPost {
		methodNotAllowed(response)
		return
	}
	if err := request.ParseForm(); err != nil {
		http.Error(response, "invalid form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(firstValue(request.FormValue("name"), request.FormValue("linkren")))
	title := strings.TrimSpace(firstValue(request.FormValue("title"), request.FormValue("Title")))
	phone := strings.TrimSpace(request.FormValue("phone"))
	if name == "" || title == "" || phone == "" {
		http.Error(response, "name, title and phone are required", http.StatusBadRequest)
		return
	}
	prodID := parseIntOrZero(request.FormValue("prodid"))
	_, err := s.database.ExecContext(request.Context(), `
		INSERT INTO "benming_ch_Msg" ("Title", "linkren", "phone", "mobile", "fax", "email", "content", "date", "address", "state", "prodid")
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		title, name, phone, request.FormValue("mobile"), request.FormValue("fax"), request.FormValue("email"),
		request.FormValue("content"), time.Now().Format("2006-01-02 15:04:05"), request.FormValue("address"), prodID)
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	if legacy {
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(response, "true")
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) loginPage(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(response)
		return
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(response, `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>后台登录</title><style>body{font-family:Arial,"Microsoft YaHei",sans-serif;max-width:360px;margin:80px auto;padding:0 20px}label{display:block;margin:12px 0 4px}input{box-sizing:border-box;width:100%;padding:10px}button{margin-top:18px;padding:10px 20px}</style></head><body><h1>后台登录</h1><form method="post" action="/bil/check.asp"><label>用户名</label><input name="userid" autocomplete="username" required><label>密码</label><input name="password" type="password" autocomplete="current-password" required><button type="submit">登录</button></form></body></html>`)
}

func (s *Server) checkLogin(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(response)
		return
	}
	if err := request.ParseForm(); err != nil {
		http.Error(response, "invalid form", http.StatusBadRequest)
		return
	}
	username := strings.TrimSpace(request.FormValue("userid"))
	password := request.FormValue("password")
	var id int64
	var storedPassword string
	if err := s.database.QueryRowContext(request.Context(), `SELECT "Id", COALESCE("PassWord", '') FROM "benming_master" WHERE "UserName" = ?`, username).Scan(&id, &storedPassword); err != nil || !auth.ComparePassword(password, storedPassword) {
		http.Error(response, "用户名或密码不正确", http.StatusUnauthorized)
		return
	}
	token, err := s.createSession(username)
	if err != nil {
		http.Error(response, "session error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(response, sessionCookie(token, 86400))
	_, _ = s.database.ExecContext(request.Context(), `UPDATE "benming_master" SET "LastLogin" = ?, "LastLoginIp" = ? WHERE "Id" = ?`, time.Now().Format("2006-01-02 15:04:05"), request.RemoteAddr, id)
	http.Redirect(response, request, "/bil/index.asp", http.StatusSeeOther)
}

func (s *Server) adminPage(response http.ResponseWriter, request *http.Request) {
	if !s.authenticated(request) {
		http.Redirect(response, request, "/bil/login.asp", http.StatusSeeOther)
		return
	}
	var products, news, messages int64
	_ = s.database.QueryRowContext(request.Context(), `SELECT COUNT(*) FROM "benming_ch_prod"`).Scan(&products)
	_ = s.database.QueryRowContext(request.Context(), `SELECT COUNT(*) FROM "benming_ch_news"`).Scan(&news)
	_ = s.database.QueryRowContext(request.Context(), `SELECT COUNT(*) FROM "benming_ch_Msg"`).Scan(&messages)
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(response, `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><title>后台管理</title></head><body><h1>后台管理</h1><p>产品：%d　新闻：%d　留言：%d</p><p><a href="/">查看网站</a></p></body></html>`, products, news, messages)
}

func (s *Server) authenticated(request *http.Request) bool {
	_, ok := s.adminUsername(request)
	return ok
}

func (s *Server) staticFile(response http.ResponseWriter, request *http.Request) {
	lowerPath := strings.ToLower(request.URL.Path)
	for _, blocked := range []string{"/database", "/data", "/cmd", "/internal", "/.git", "/conn", "/inc", "/manage", "/bil/cn", "/bil/system", "/uploadfile"} {
		if strings.HasPrefix(lowerPath, blocked) {
			http.NotFound(response, request)
			return
		}
	}
	for _, extension := range []string{".asp", ".asa", ".inc", ".mdb", ".accdb", ".sqlite", ".db", ".go", ".sum", ".mod", ".md", ".ini", ".config"} {
		if strings.HasSuffix(lowerPath, extension) {
			http.NotFound(response, request)
			return
		}
	}
	clean := path.Clean("/" + request.URL.Path)
	if strings.Contains(clean, "/../") || clean == "/.." {
		http.NotFound(response, request)
		return
	}
	if s.serveResource(response, request) {
		return
	}
	s.fileServe.ServeHTTP(response, request)
}

func snippet(value string, limit int) string {
	value = html.UnescapeString(htmlTagPattern.ReplaceAllString(value, ""))
	value = strings.Join(strings.Fields(value), " ")
	if len([]rune(value)) <= limit {
		return value
	}
	return string([]rune(value)[:limit]) + "..."
}

func firstValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func positiveInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}

func parseIntOrZero(value string) int64 {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed < 0 {
		return 0
	}
	return parsed
}

func methodNotAllowed(response http.ResponseWriter) {
	response.Header().Set("Allow", "GET, POST")
	http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
