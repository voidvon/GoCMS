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

	"gocms/internal/db"
	"gocms/internal/templateconfig"
	"gocms/internal/templatelabel"
	"gocms/internal/theme"
)

type Server struct {
	database         *sql.DB
	siteRoot         string
	fileServe        http.Handler
	frontendRoot     string
	frontendFS       fs.FS
	frontendDevProxy http.Handler
	assetsRoot       string
	themeRoot        string
	templateRoot     string
	themeBase        string
	themeData        string
	activeTheme      theme.Definition
	themeMu          sync.RWMutex
	publication      *publication
	updateMu         sync.Mutex
	updateActive     bool
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
		if err := db.EnsureAuditLog(context.Background(), database); err != nil {
			return nil, err
		}
		if err := db.EnsureApiKeys(context.Background(), database); err != nil {
			return nil, err
		}
		if err := templateconfig.Ensure(context.Background(), database); err != nil {
			return nil, err
		}
		if err := templatelabel.Ensure(context.Background(), database); err != nil {
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
	if (s.frontendDevProxy != nil || s.frontendRoot != "" || s.frontendFS != nil) && (cleanPath == "/admin" || strings.HasPrefix(cleanPath, "/admin/")) {
		s.serveAdminApp(response, request)
		return
	}
	lowerAdminPath := strings.ToLower(cleanPath)
	if strings.HasPrefix(lowerAdminPath, "/api/admin/") && lowerAdminPath != "/api/admin/login" && lowerAdminPath != "/api/admin/logout" {
		if !s.authorizeAdminRoute(response, request, lowerAdminPath) {
			return
		}
		if request.Method == http.MethodPost || request.Method == http.MethodPut || request.Method == http.MethodPatch || request.Method == http.MethodDelete {
			user, _ := request.Context().Value(contentScopeKey{}).(*AdminUser)
			if user != nil {
				capture := &operationResponse{ResponseWriter: response}
				response = capture
				defer s.recordOperation(user.Username, request, capture)
			}
		}
	}
	switch lowerAdminPath {
	case "/api/admin/logs":
		s.adminOperationLogs(response, request)
	case "/api/admin/logins":
		s.adminLoginLogs(response, request)
	case "/api/admin/logs/clear":
		s.adminClearLogs(response, request)
	case "/api/admin/users":
		s.adminUsers(response, request)
	case "/api/admin/groups":
		s.adminGroups(response, request)
	case "/api/admin/update/check":
		s.adminUpdateCheck(response, request)
	case "/api/admin/update":
		s.adminUpdate(response, request)
	case "/api/admin/publish":
		s.adminPublish(response, request)
	case "/api/admin/publish/llms":
		s.adminLLMS(response, request)
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
	case "/api/admin/media":
		s.adminMedia(response, request)
	case "/api/admin/messages", "/api/admin/feedback":
		s.adminFeedbacks(response, request)
	case "/api/admin/feedback/batch-delete":
		s.adminFeedbackBatchDelete(response, request)
	case "/api/admin/feedback-classes":
		s.adminFeedbackClasses(response, request)
	case "/api/admin/feedback-fields":
		s.adminFeedbackFields(response, request)
	case "/api/admin/model-tables":
		s.adminModelTables(response, request)
	case "/api/admin/model-fields":
		s.adminModelFields(response, request)
	case "/api/admin/models":
		s.adminModels(response, request)
	case "/api/admin/categories":
		s.adminCategories(response, request)
	case "/api/admin/languages":
		s.adminLanguages(response, request)
	case "/api/admin/api-keys":
		s.adminApiKeys(response, request)
	case "/api/admin/theme/activate":
		s.adminThemeActivate(response, request)
	case "/api/admin/template/activate", "/api/admin/templates/activate":
		s.adminThemeActivate(response, request)
	case "/api/admin/theme/import":
		s.adminThemeImport(response, request)
	case "/api/admin/template/import", "/api/admin/templates/import":
		s.adminThemeImport(response, request)
	case "/api/admin/theme/export":
		s.adminThemeExport(response, request)
	case "/api/admin/template/export", "/api/admin/templates/export":
		s.adminThemeExport(response, request)
	case "/api/admin/theme/files", "/api/admin/template/files", "/api/admin/templates/files":
		s.adminThemeFiles(response, request)
	case "/api/admin/theme/label-templates":
		s.adminThemeLabelTemplates(response, request)
	case "/api/admin/template/label-templates", "/api/admin/templates/label-templates":
		s.adminThemeLabelTemplates(response, request)
	case "/api/admin/theme/label-categories":
		s.adminThemeLabelCategories(response, request)
	case "/api/admin/template/label-categories", "/api/admin/templates/label-categories":
		s.adminThemeLabelCategories(response, request)
	case "/api/admin/theme":
		s.adminTheme(response, request)
	case "/api/admin/template", "/api/admin/templates":
		s.adminTheme(response, request)
	case "/api/search":
		s.searchJSON(response, request)
	case "/api/content":
		s.contentJSON(response, request)
	case "/api/messages", "/api/feedback":
		s.feedbackSubmit(response, request)
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
		if strings.HasPrefix(lowerPath, "/api/admin/media/") {
			s.adminMediaItem(response, request, cleanPath[len("/api/admin/media/"):])
			return
		}
		if strings.HasPrefix(lowerPath, "/api/admin/messages/") {
			s.adminFeedbackItem(response, request, cleanPath[len("/api/admin/messages/"):])
			return
		}
		if strings.HasPrefix(lowerPath, "/api/admin/feedback/") {
			s.adminFeedbackItem(response, request, cleanPath[len("/api/admin/feedback/"):])
			return
		}
		if strings.HasPrefix(lowerPath, "/api/admin/feedback-classes/") {
			s.adminFeedbackClassItem(response, request, cleanPath[len("/api/admin/feedback-classes/"):])
			return
		}
		if strings.HasPrefix(lowerPath, "/api/admin/feedback-fields/") {
			s.adminFeedbackFieldItem(response, request, cleanPath[len("/api/admin/feedback-fields/"):])
			return
		}
		if strings.HasPrefix(lowerPath, "/api/admin/model-tables/") {
			s.adminModelTableItem(response, request, cleanPath[len("/api/admin/model-tables/"):])
			return
		}
		if strings.HasPrefix(lowerPath, "/api/admin/model-fields/") {
			s.adminModelFieldItem(response, request, cleanPath[len("/api/admin/model-fields/"):])
			return
		}
		if strings.HasPrefix(lowerPath, "/api/admin/models/") {
			s.adminModelItem(response, request, cleanPath[len("/api/admin/models/"):])
			return
		}
		if strings.HasPrefix(lowerPath, "/api/admin/categories/") {
			s.adminCategory(response, request, cleanPath[len("/api/admin/categories/"):])
			return
		}
		if strings.HasPrefix(lowerPath, "/api/admin/languages/") {
			s.adminLanguageItem(response, request, cleanPath[len("/api/admin/languages/"):])
			return
		}
		if strings.HasPrefix(lowerPath, "/api/admin/api-keys/") {
			s.adminApiKeyRoute(response, request, cleanPath[len("/api/admin/api-keys/"):])
			return
		}
		if strings.HasPrefix(lowerPath, "/api/admin/theme/assignments/") {
			s.adminThemeAssignment(response, request, cleanPath[len("/api/admin/theme/assignments/"):])
			return
		}
		for _, prefix := range []string{"/api/admin/template/assignments/", "/api/admin/templates/assignments/"} {
			if strings.HasPrefix(lowerPath, prefix) {
				s.adminThemeAssignment(response, request, cleanPath[len(prefix):])
				return
			}
		}
		if strings.HasPrefix(lowerPath, "/api/admin/theme/label-templates/") {
			s.adminThemeLabelTemplate(response, request, cleanPath[len("/api/admin/theme/label-templates/"):])
			return
		}
		for _, prefix := range []string{"/api/admin/template/label-templates/", "/api/admin/templates/label-templates/"} {
			if strings.HasPrefix(lowerPath, prefix) {
				s.adminThemeLabelTemplate(response, request, cleanPath[len(prefix):])
				return
			}
		}
		if strings.HasPrefix(lowerPath, "/api/admin/theme/label-categories/") {
			s.adminThemeLabelCategory(response, request, cleanPath[len("/api/admin/theme/label-categories/"):])
			return
		}
		for _, prefix := range []string{"/api/admin/template/label-categories/", "/api/admin/templates/label-categories/"} {
			if strings.HasPrefix(lowerPath, prefix) {
				s.adminThemeLabelCategory(response, request, cleanPath[len(prefix):])
				return
			}
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
	lang := strings.TrimSpace(request.URL.Query().Get("lang"))
	result, err := s.queryContent(request.Context(), query, 0, lang, positiveInt(request.URL.Query().Get("page"), 1), positiveInt(request.URL.Query().Get("page_size"), 20), true)
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
	s.feedbackSubmit(response, request)
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
