package site

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"gocms/internal/auth"
	"gocms/internal/sitehost"
	"gocms/internal/templateconfig"
	"gocms/internal/theme"
)

type Server struct {
	database               *sql.DB
	siteRoot               string
	fileServe              http.Handler
	frontendRoot           string
	frontendFS             fs.FS
	assetsRoot             string
	themeRoot              string
	templateRoot           string
	themeBase              string
	themeData              string
	themeFallbackTemplates string
	activeTheme            theme.Definition
	themeMu                sync.RWMutex
	publicHost             string
	publication            *publication
	updateMu               sync.Mutex
	updateActive           bool
}

var htmlTagPattern = regexp.MustCompile(`(?s)<[^>]*>`)

func New(database *sql.DB, siteRoot string) (*Server, error) {
	if database != nil {
		if _, err := database.Exec(`
			CREATE TABLE IF NOT EXISTS "gocms_admin_session" (
				"token" TEXT PRIMARY KEY,
				"username" TEXT NOT NULL,
				"expires_at" INTEGER NOT NULL
			)`); err != nil {
			return nil, fmt.Errorf("create admin session table: %w", err)
		}
		if _, err := database.Exec(`CREATE INDEX IF NOT EXISTS "idx_gocms_admin_session_expiry" ON "gocms_admin_session" ("expires_at")`); err != nil {
			return nil, fmt.Errorf("create admin session index: %w", err)
		}
		if err := templateconfig.Ensure(context.Background(), database); err != nil {
			return nil, err
		}
	}
	return &Server{
		database:   database,
		siteRoot:   siteRoot,
		fileServe:  http.FileServer(http.Dir(siteRoot)),
		publicHost: sitehost.FromDatabase(context.Background(), database),
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
	if (s.frontendRoot != "" || s.frontendFS != nil) && (cleanPath == "/admin" || strings.HasPrefix(cleanPath, "/admin/")) {
		s.serveAdminApp(response, request)
		return
	}
	switch strings.ToLower(cleanPath) {
	case "/api/admin/update/check":
		s.adminUpdateCheck(response, request)
	case "/api/admin/update":
		s.adminUpdate(response, request)
	case "/api/admin/publish":
		s.adminPublish(response, request)
	case "/api/admin/publish/sitemap":
		s.adminSitemap(response, request)
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
	case "/api/admin/content":
		s.adminContent(response, request)
	case "/api/admin/messages":
		s.adminMessages(response, request)
	case "/api/admin/categories":
		s.adminCategories(response, request)
	case "/api/admin/theme/activate":
		s.adminThemeActivate(response, request)
	case "/api/admin/theme/import":
		s.adminThemeImport(response, request)
	case "/api/admin/theme/export":
		s.adminThemeExport(response, request)
	case "/api/admin/theme":
		s.adminTheme(response, request)
	case "/api/search":
		s.searchJSON(response, request)
	case "/api/content":
		s.contentJSON(response, request)
	case "/api/messages":
		s.messages(response, request)
	case "/search.asp":
		s.searchHTML(response, request)
	case "/bil/login.asp", "/admin/login":
		s.loginPage(response, request)
	case "/bil/check.asp", "/admin/login/check":
		s.checkLogin(response, request)
	case "/bil/index.asp", "/admin":
		s.adminPage(response, request)
	default:
		lowerPath := strings.ToLower(cleanPath)
		if strings.HasPrefix(lowerPath, "/api/admin/content/") {
			s.adminContentItem(response, request, cleanPath[len("/api/admin/content/"):])
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
		if strings.HasPrefix(lowerPath, "/api/admin/theme/assignments/") {
			s.adminThemeAssignment(response, request, cleanPath[len("/api/admin/theme/assignments/"):])
			return
		}
		if strings.HasPrefix(strings.ToLower(cleanPath), "/api/content/") {
			s.contentJSONItem(response, request, cleanPath[len("/api/content/"):])
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
	var contentCount int64
	if err := s.database.QueryRowContext(request.Context(), `SELECT COUNT(*) FROM "gocms_content"`).Scan(&contentCount); err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"ok":       true,
		"database": "sqlite",
		"contents": contentCount,
	})
}

func (s *Server) searchJSON(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(response)
		return
	}
	query := strings.TrimSpace(request.URL.Query().Get("q"))
	result, err := s.queryContent(request.Context(), query, 0, positiveInt(request.URL.Query().Get("page"), 1), positiveInt(request.URL.Query().Get("page_size"), 20), true)
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (s *Server) messages(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(response)
		return
	}
	if err := request.ParseForm(); err != nil {
		http.Error(response, "invalid form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(request.FormValue("name"))
	title := strings.TrimSpace(request.FormValue("title"))
	phone := strings.TrimSpace(request.FormValue("phone"))
	if name == "" || title == "" || phone == "" {
		http.Error(response, "name, title and phone are required", http.StatusBadRequest)
		return
	}
	contentID := parseIntOrZero(request.FormValue("content_id"))
	_, err := s.database.ExecContext(request.Context(), `
		INSERT INTO "gocms_message" ("title", "name", "phone", "mobile", "fax", "email", "content", "created_at", "address", "state", "content_id")
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		title, name, phone, request.FormValue("mobile"), request.FormValue("fax"), request.FormValue("email"),
		request.FormValue("content"), time.Now().Format("2006-01-02 15:04:05"), request.FormValue("address"), contentID)
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
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
	var contents, messages int64
	_ = s.database.QueryRowContext(request.Context(), `SELECT COUNT(*) FROM "gocms_content"`).Scan(&contents)
	_ = s.database.QueryRowContext(request.Context(), `SELECT COUNT(*) FROM "gocms_message"`).Scan(&messages)
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(response, `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><title>后台管理</title></head><body><h1>后台管理</h1><p>内容：%d　留言：%d</p><p><a href="/">查看网站</a></p></body></html>`, contents, messages)
}

func (s *Server) authenticated(request *http.Request) bool {
	_, ok := s.adminUsername(request)
	return ok
}

func (s *Server) staticFile(response http.ResponseWriter, request *http.Request) {
	lowerPath := strings.ToLower(request.URL.Path)
	privatePath := path.Clean("/" + strings.ReplaceAll(lowerPath, `\`, "/"))
	if privatePath == "/assets/theme" || strings.HasPrefix(privatePath, "/assets/theme/") {
		http.NotFound(response, request)
		return
	}
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
