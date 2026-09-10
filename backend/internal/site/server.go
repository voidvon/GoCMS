package site

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"gocms/internal/db"
	"gocms/internal/templateconfig"
	"gocms/internal/theme"
)

type Server struct {
	database     *sql.DB
	siteRoot     string
	fileServe    http.Handler
	frontendRoot string
	frontendFS   fs.FS
	assetsRoot   string
	themeRoot    string
	templateRoot string
	themeBase    string
	themeData    string
	activeTheme  theme.Definition
	themeMu      sync.RWMutex
	publication  *publication
	updateMu     sync.Mutex
	updateActive bool
}

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
		if err := db.EnsureAdminUsers(context.Background(), database); err != nil {
			return nil, err
		}
		if err := templateconfig.Ensure(context.Background(), database); err != nil {
			return nil, err
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
	case "/search":
		target := "/search.html"
		if request.URL.RawQuery != "" {
			target += "?" + request.URL.RawQuery
		}
		http.Redirect(response, request, target, http.StatusSeeOther)
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
	if err := s.populateContentURLs(request.Context(), &result); err != nil {
		http.Error(response, "content route error", http.StatusInternalServerError)
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

func (s *Server) staticFile(response http.ResponseWriter, request *http.Request) {
	lowerPath := strings.ToLower(request.URL.Path)
	privatePath := path.Clean("/" + strings.ReplaceAll(lowerPath, `\`, "/"))
	if privatePath == "/assets/theme" || strings.HasPrefix(privatePath, "/assets/theme/") {
		http.NotFound(response, request)
		return
	}
	for _, blocked := range []string{"/database", "/data", "/cmd", "/internal", "/.git"} {
		if strings.HasPrefix(lowerPath, blocked) {
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
