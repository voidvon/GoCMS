package site

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gocms/internal/db"
)

const (
	accessLogChromeUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"
	accessLogIPhoneUA = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"
	accessLogGoogleUA = "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
	accessLogCurlUA   = "curl/8.4.0"
)

// The admin frontend renders access log times in the browser zone, so the API
// must return an unambiguous instant instead of a server-local wall clock.
func TestFormatVisitedReturnsRFC3339UTC(t *testing.T) {
	config := newAccessLogConfig()
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{name: "utc timestamp", raw: "2026-10-18 12:33:24", want: "2026-10-18T12:33:24Z"},
		{name: "padded", raw: "  2026-01-02 03:04:05  ", want: "2026-01-02T03:04:05Z"},
		{name: "empty", raw: "  ", want: ""},
		{name: "unparsable stays as-is", raw: "not-a-time", want: "not-a-time"},
	}
	for _, testCase := range cases {
		if got := config.formatVisited(testCase.raw); got != testCase.want {
			t.Errorf("%s: formatVisited(%q) = %q, want %q", testCase.name, testCase.raw, got, testCase.want)
		}
	}
}

func TestSummarizeUserAgent(t *testing.T) {
	cases := []struct {
		name       string
		agent      string
		wantKind   string
		wantLabel  string
		wantDevice string
	}{
		{"chrome", accessLogChromeUA, "browser", "Chrome 141", "desktop"},
		{"iphone", accessLogIPhoneUA, "browser", "Safari", "mobile"},
		{"ipad", "Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/604.1", "browser", "Safari", "tablet"},
		{"googlebot", accessLogGoogleUA, "bot", "Googlebot", "other"},
		{"bingbot", "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)", "bot", "Bingbot", "other"},
		{"curl", accessLogCurlUA, "tool", "curl", "other"},
		{"go-client", "Go-http-client/1.1", "tool", "Go HTTP Client", "other"},
		{"empty", "", "other", "无 User-Agent", "other"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			info := summarizeUserAgent(testCase.agent)
			if info.Kind != testCase.wantKind || info.Label != testCase.wantLabel || info.Device != testCase.wantDevice {
				t.Fatalf("summarizeUserAgent(%q) = %+v, want kind=%s label=%s device=%s",
					testCase.agent, info, testCase.wantKind, testCase.wantLabel, testCase.wantDevice)
			}
		})
	}
}

func TestAccessLogRecordsPublicTraffic(t *testing.T) {
	server, database, _, siteDir := newSiteAdminTestServer(t)
	if err := os.WriteFile(filepath.Join(siteDir, "index.html"), []byte("<html><body>home</body></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(siteDir, "logo.png"), []byte{0x89, 'P', 'N', 'G'}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE "gocms_site" SET "domain" = 'example.test' WHERE "is_default" = 1`); err != nil {
		t.Fatal(err)
	}
	// A standalone language domain owned by the same site must count as internal.
	if _, err := database.Exec(`UPDATE "gocms_language" SET "site_mode" = 'standalone', "domain" = 'cn.example.test' WHERE "code" = 'zh-CN'`); err != nil {
		t.Fatal(err)
	}

	publicRequest := func(host, path, referer, agent string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Host = host
		if referer != "" {
			request.Header.Set("Referer", referer)
		}
		if agent != "" {
			request.Header.Set("User-Agent", agent)
		}
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		return response
	}

	if status := publicRequest("example.test", "/", "", accessLogChromeUA).Code; status != http.StatusOK {
		t.Fatalf("home page status = %d, want 200", status)
	}
	publicRequest("example.test", "/", "", accessLogIPhoneUA)
	publicRequest("example.test", "/", "", accessLogGoogleUA)
	publicRequest("example.test", "/", "", accessLogCurlUA)
	if status := publicRequest("example.test", "/logo.png", "", accessLogChromeUA).Code; status != http.StatusOK {
		t.Fatalf("asset status = %d, want 200", status)
	}
	if status := publicRequest("example.test", "/missing-page", "https://example.test/", accessLogChromeUA).Code; status != http.StatusNotFound {
		t.Fatalf("missing page status = %d, want 404", status)
	}
	// A same-host Referer that points at a non-existent path is a spoofed
	// scanner header, not an internal navigation.
	if status := publicRequest("example.test", "/missing-invalid", "https://example.test/.aws/credentials", accessLogChromeUA).Code; status != http.StatusNotFound {
		t.Fatalf("invalid referer status = %d, want 404", status)
	}
	if status := publicRequest("example.test", "/css/missing.css", "https://external.test/page", accessLogChromeUA).Code; status != http.StatusNotFound {
		t.Fatalf("missing asset status = %d, want 404", status)
	}
	publicRequest("example.test", "/api/health", "", accessLogChromeUA)
	publicRequest("example.test", "/admin/", "", accessLogChromeUA)
	publicRequest("cn.example.test", "/", "https://www.example.test/", accessLogChromeUA)
	// A referer from the same host counts as internal even when the host is not
	// a registered domain of the site.
	publicRequest("other.test", "/", "http://other.test/", accessLogChromeUA)

	count := func(query string, args ...any) int64 {
		t.Helper()
		var total int64
		if err := database.QueryRow(query, args...).Scan(&total); err != nil {
			t.Fatal(err)
		}
		return total
	}
	table := `"` + db.AccessLogTable + `"`
	checks := []struct {
		label string
		query string
		args  []any
		want  int64
	}{
		{"all rows", `SELECT COUNT(*) FROM ` + table, nil, 9},
		{"single site", `SELECT COUNT(*) FROM ` + table + ` WHERE "site_id" = 1`, nil, 9},
		{"page views", `SELECT COUNT(*) FROM ` + table + ` WHERE "kind" = 'page'`, nil, 6},
		{"errors", `SELECT COUNT(*) FROM ` + table + ` WHERE "kind" = 'error' AND "status" = 404`, nil, 3},
		{"successful asset skipped", `SELECT COUNT(*) FROM ` + table + ` WHERE "path" = '/logo.png'`, nil, 0},
		{"api skipped", `SELECT COUNT(*) FROM ` + table + ` WHERE "path" LIKE '/api/%'`, nil, 0},
		{"admin skipped", `SELECT COUNT(*) FROM ` + table + ` WHERE "path" LIKE '/admin%'`, nil, 0},
		{"redirects skipped", `SELECT COUNT(*) FROM ` + table + ` WHERE "status" >= 300 AND "status" < 400`, nil, 0},
		{"browser rows", `SELECT COUNT(*) FROM ` + table + ` WHERE "agent_kind" = 'browser'`, nil, 7},
		{"bot rows", `SELECT COUNT(*) FROM ` + table + ` WHERE "agent_kind" = 'bot'`, nil, 1},
		{"tool rows", `SELECT COUNT(*) FROM ` + table + ` WHERE "agent_kind" = 'tool'`, nil, 1},
		{"mobile rows", `SELECT COUNT(*) FROM ` + table + ` WHERE "device_kind" = 'mobile'`, nil, 1},
		{"desktop rows", `SELECT COUNT(*) FROM ` + table + ` WHERE "device_kind" = 'desktop'`, nil, 6},
		{"internal referer", `SELECT COUNT(*) FROM ` + table + ` WHERE "referer_kind" = 'internal'`, nil, 3},
		{"external referer", `SELECT COUNT(*) FROM ` + table + ` WHERE "referer_kind" = 'external'`, nil, 1},
		{"invalid referer", `SELECT COUNT(*) FROM ` + table + ` WHERE "referer_kind" = 'invalid'`, nil, 1},
		{"recorded ip", `SELECT COUNT(*) FROM ` + table + ` WHERE "ip" = '192.0.2.1'`, nil, 9},
		{"recorded content type", `SELECT COUNT(*) FROM ` + table + ` WHERE "status" = 404 AND "content_type" <> ''`, nil, 3},
	}
	for _, check := range checks {
		if got := count(check.query, check.args...); got != check.want {
			t.Errorf("%s = %d, want %d", check.label, got, check.want)
		}
	}
}

type accessLogRow struct {
	siteID      int64
	path        string
	status      int
	kind        string
	dayKey      string
	visitedAt   string
	ip          string
	referer     string
	refererKind string
	agentKind   string
	agentLabel  string
	deviceKind  string
}

func TestAccessLogAdminEndpoints(t *testing.T) {
	server, database, token, _ := newSiteAdminTestServer(t)
	second, err := database.Exec(`INSERT INTO "gocms_site" ("name", "code", "domain", "is_default") VALUES ('Second', 'second', 'second.test', 0)`)
	if err != nil {
		t.Fatal(err)
	}
	secondID, err := second.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	today := now.In(time.Local).Format("2006-01-02")
	recentUTC := now.UTC().Format("2006-01-02 15:04:05")
	// Distinct timestamps so the dashboard's newest-first error ordering is
	// observable: /b is the most frequent error, /c is the most recent one.
	earlierUTC := now.Add(-3 * time.Hour).UTC().Format("2006-01-02 15:04:05")
	oldUTC := now.AddDate(0, 0, -2).UTC().Format("2006-01-02 15:04:05")
	oldDay := now.AddDate(0, 0, -2).In(time.Local).Format("2006-01-02")

	insert := func(row accessLogRow) {
		t.Helper()
		if row.ip == "" {
			row.ip = "203.0.113.10"
		}
		if row.refererKind == "" {
			row.refererKind = "none"
		}
		if row.agentKind == "" {
			row.agentKind = "browser"
		}
		if row.agentLabel == "" {
			row.agentLabel = "Chrome 141"
		}
		if row.deviceKind == "" {
			row.deviceKind = "desktop"
		}
		if row.dayKey == "" {
			row.dayKey = today
		}
		if row.visitedAt == "" {
			row.visitedAt = recentUTC
		}
		if row.kind == "" {
			row.kind = "page"
			if row.status >= 400 {
				row.kind = "error"
			}
		}
		if _, err := database.Exec(`INSERT INTO "`+db.AccessLogTable+`"
			("site_id", "day_key", "path", "query", "method", "status", "kind", "content_type", "ip",
			 "referer", "referer_host", "referer_kind", "user_agent", "agent_kind", "agent_label", "device_kind", "visited_at")
			VALUES (?, ?, ?, '', 'GET', ?, ?, 'text/html', ?, '', '', ?, 'ua', ?, ?, ?, ?)`,
			row.siteID, row.dayKey, row.path, row.status, row.kind, row.ip, row.refererKind, row.agentKind, row.agentLabel, row.deviceKind, row.visitedAt); err != nil {
			t.Fatal(err)
		}
	}

	insert(accessLogRow{siteID: 1, path: "/a", status: 200})
	insert(accessLogRow{siteID: 1, path: "/b", status: 404, refererKind: "internal", ip: "203.0.113.11", visitedAt: earlierUTC})
	insert(accessLogRow{siteID: 1, path: "/b", status: 404, refererKind: "internal", ip: "203.0.113.12", visitedAt: earlierUTC})
	insert(accessLogRow{siteID: 1, path: "/c", status: 500, agentKind: "bot", agentLabel: "Googlebot", deviceKind: "other"})
	insert(accessLogRow{siteID: 1, path: "/old", status: 200, dayKey: oldDay, visitedAt: oldUTC})
	insert(accessLogRow{siteID: secondID, path: "/x", status: 404})

	type listResponse struct {
		Items    []AccessLogItem `json:"items"`
		Total    int64           `json:"total"`
		Page     int             `json:"page"`
		PageSize int             `json:"page_size"`
		SiteID   int64           `json:"site_id"`
	}
	list := func(siteHeader, query string) listResponse {
		t.Helper()
		response := siteAdminRequest(server, token, http.MethodGet, "/api/admin/access-logs"+query, siteHeader, "")
		if response.Code != http.StatusOK {
			t.Fatalf("GET /api/admin/access-logs%s (site %s) = %d, body %s", query, siteHeader, response.Code, response.Body.String())
		}
		var decoded listResponse
		if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
			t.Fatal(err)
		}
		return decoded
	}

	if got := list("1", "?page_size=100"); got.Total != 5 || got.SiteID != 1 || len(got.Items) != 5 {
		t.Fatalf("site 1 list = %+v, want 5 rows on site 1", got)
	}
	if got := list("2", "?page_size=100"); got.Total != 1 || got.SiteID != secondID {
		t.Fatalf("site 2 list = %+v, want 1 row on site %d", got, secondID)
	}
	for _, item := range list("1", "?page_size=100").Items {
		if item.SiteID != 1 {
			t.Fatalf("site 1 list leaked site %d row %s", item.SiteID, item.Path)
		}
	}
	filters := []struct {
		query string
		want  int64
	}{
		{"?status_mode=404", 2},
		{"?kind=error", 3},
		{"?kind=page", 2},
		{"?agent_kind=browser", 4},
		{"?agent_kind=bot", 1},
		{"?referer_kind=internal", 2},
		{"?referer_kind=none", 3},
		{"?window=today", 4},
		{"?path=/b", 2},
		{"?status=500", 1},
		{"?q=googlebot", 1},
	}
	for _, filter := range filters {
		if got := list("1", filter.query+"&page_size=100").Total; got != filter.want {
			t.Errorf("filter %s total = %d, want %d", filter.query, got, filter.want)
		}
	}
	// Pagination must slice, not truncate the whole result.
	if got := list("1", "?page_size=2&page=2"); len(got.Items) != 2 || got.Total != 5 || got.Page != 2 {
		t.Fatalf("pagination = %+v, want page 2 of 5 rows with 2 items", got)
	}

	errorResponse := siteAdminRequest(server, token, http.MethodGet, "/api/admin/access-logs/errors", "1", "")
	if errorResponse.Code != http.StatusOK {
		t.Fatalf("GET errors = %d, body %s", errorResponse.Code, errorResponse.Body.String())
	}
	var errors struct {
		Items []AccessLogErrorStat `json:"items"`
		Total int                  `json:"total"`
	}
	if err := json.Unmarshal(errorResponse.Body.Bytes(), &errors); err != nil {
		t.Fatal(err)
	}
	if errors.Total != 2 || len(errors.Items) != 2 {
		t.Fatalf("error groups = %+v, want 2 groups", errors)
	}
	// The error-group endpoint stays ranked by hit count (the traffic log's
	// "worst first" view); only the dashboard summary ranks by newest first.
	if errors.Items[0].Path != "/b" || errors.Items[0].Status != 404 || errors.Items[0].Hits != 2 || errors.Items[0].InternalHits != 2 {
		t.Fatalf("first error group = %+v, want /b 404 with 2 internal hits", errors.Items[0])
	}
	if errors.Items[1].Path != "/c" || errors.Items[1].Status != 500 {
		t.Fatalf("second error group = %+v, want /c 500", errors.Items[1])
	}

	statsResponse := siteAdminRequest(server, token, http.MethodGet, "/api/admin/stats", "1", "")
	if statsResponse.Code != http.StatusOK {
		t.Fatalf("GET stats = %d, body %s", statsResponse.Code, statsResponse.Body.String())
	}
	var stats AdminStats
	if err := json.Unmarshal(statsResponse.Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.Access.Metrics.TotalRequests != 5 {
		t.Errorf("stats access total_requests = %d, want 5", stats.Access.Metrics.TotalRequests)
	}
	if stats.Access.Metrics.TodayVisits != 1 {
		t.Errorf("stats access today_visits = %d, want 1", stats.Access.Metrics.TodayVisits)
	}
	if stats.Access.Metrics.TotalErrors != 3 {
		t.Errorf("stats access total_errors = %d, want 3", stats.Access.Metrics.TotalErrors)
	}
	if stats.Access.Metrics.TotalNotFound != 2 {
		t.Errorf("stats access total_404_errors = %d, want 2", stats.Access.Metrics.TotalNotFound)
	}
	if stats.Access.Metrics.InternalNotFound != 2 {
		t.Errorf("stats access internal_404_errors = %d, want 2", stats.Access.Metrics.InternalNotFound)
	}
	if stats.Access.Metrics.InternalNotFoundPaths != 1 {
		t.Errorf("stats access internal_404_paths = %d, want 1", stats.Access.Metrics.InternalNotFoundPaths)
	}
	if stats.Access.Metrics.TotalPages != 2 {
		t.Errorf("stats access total_pages = %d, want 2", stats.Access.Metrics.TotalPages)
	}
	if stats.Access.Metrics.RetentionDays != defaultAccessLogRetentionDays {
		t.Errorf("stats access retention_days = %d, want %d", stats.Access.Metrics.RetentionDays, defaultAccessLogRetentionDays)
	}
	if stats.Access.Metrics.DayKey != today {
		t.Errorf("stats access day_key = %q, want %q", stats.Access.Metrics.DayKey, today)
	}
	if len(stats.Access.TopPages) == 0 || stats.Access.TopPages[0].Path != "/a" {
		t.Errorf("stats top pages = %+v, want /a first", stats.Access.TopPages)
	}
	// Newest first: /c was visited just now, /b three hours ago, even though /b
	// has more hits.
	if len(stats.Access.TopErrors) < 2 || stats.Access.TopErrors[0].Path != "/c" || stats.Access.TopErrors[1].Path != "/b" {
		t.Errorf("stats top errors = %+v, want newest /c first then /b", stats.Access.TopErrors)
	}
	if len(stats.Access.StatusBreakdown) != 2 {
		t.Errorf("stats status breakdown = %+v, want 404 and 500", stats.Access.StatusBreakdown)
	}

	clearResponse := siteAdminRequest(server, token, http.MethodDelete, "/api/admin/access-logs?status_mode=404", "1", "")
	if clearResponse.Code != http.StatusOK {
		t.Fatalf("DELETE access-logs = %d, body %s", clearResponse.Code, clearResponse.Body.String())
	}
	var cleared struct {
		Deleted int64 `json:"deleted"`
	}
	if err := json.Unmarshal(clearResponse.Body.Bytes(), &cleared); err != nil {
		t.Fatal(err)
	}
	if cleared.Deleted != 2 {
		t.Errorf("deleted = %d, want 2", cleared.Deleted)
	}
	if got := list("1", "?page_size=100").Total; got != 3 {
		t.Errorf("after clear site 1 total = %d, want 3", got)
	}
	if got := list("2", "?page_size=100").Total; got != 1 {
		t.Errorf("after clear site 2 total = %d, want 1 (other sites untouched)", got)
	}
}

func TestAccessLogPermissions(t *testing.T) {
	server, database, superToken, _ := newSiteAdminTestServer(t)
	now := time.Now()
	if _, err := database.Exec(`INSERT INTO "`+db.AccessLogTable+`"
		("site_id", "day_key", "path", "status", "kind", "ip", "visited_at")
		VALUES (1, ?, '/perm', 404, 'error', '203.0.113.30', ?)`,
		now.In(time.Local).Format("2006-01-02"), now.UTC().Format("2006-01-02 15:04:05")); err != nil {
		t.Fatal(err)
	}

	seedGroup := func(name string, permissions string) string {
		t.Helper()
		result, err := database.Exec(`INSERT INTO "gocms_admin_group" ("name", "permissions", "site_ids", "site_permissions")
			VALUES (?, '[]', '[1]', ?)`, name, permissions)
		if err != nil {
			t.Fatal(err)
		}
		groupID, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		username := name + "-user"
		if _, err := database.Exec(`INSERT INTO "gocms_admin_user" ("username", "password_hash", "group_id", "site_ids", "is_super")
			VALUES (?, '', ?, '[1]', 0)`, username, groupID); err != nil {
			t.Fatal(err)
		}
		sessionToken, err := server.createSession(username)
		if err != nil {
			t.Fatal(err)
		}
		return sessionToken
	}

	readerToken := seedGroup("traffic-reader", `{"1": ["traffic"]}`)
	cleanerToken := seedGroup("traffic-cleaner", `{"1": ["traffic", "traffic.clear"]}`)

	if response := siteAdminRequest(server, readerToken, http.MethodGet, "/api/admin/access-logs", "1", ""); response.Code != http.StatusOK {
		t.Fatalf("reader GET = %d, body %s", response.Code, response.Body.String())
	}
	if response := siteAdminRequest(server, readerToken, http.MethodGet, "/api/admin/access-logs/errors", "1", ""); response.Code != http.StatusOK {
		t.Fatalf("reader GET errors = %d, body %s", response.Code, response.Body.String())
	}
	if response := siteAdminRequest(server, readerToken, http.MethodGet, "/api/admin/stats", "1", ""); response.Code != http.StatusOK {
		t.Fatalf("reader GET stats = %d, body %s", response.Code, response.Body.String())
	}
	if response := siteAdminRequest(server, readerToken, http.MethodDelete, "/api/admin/access-logs", "1", ""); response.Code != http.StatusForbidden {
		t.Fatalf("reader DELETE = %d, want 403", response.Code)
	}
	if response := siteAdminRequest(server, cleanerToken, http.MethodDelete, "/api/admin/access-logs?status_mode=404", "1", ""); response.Code != http.StatusOK {
		t.Fatalf("cleaner DELETE = %d, body %s", response.Code, response.Body.String())
	}
	// The clear action is recorded in the admin operation log.
	var operations int64
	if err := database.QueryRow(`SELECT COUNT(*) FROM "gocms_admin_operation" WHERE "path" LIKE '/api/admin/access-logs%'`).Scan(&operations); err != nil {
		t.Fatal(err)
	}
	if operations == 0 {
		t.Errorf("clearing access logs was not recorded in the operation log")
	}
	// Super administrators keep full access.
	if response := siteAdminRequest(server, superToken, http.MethodGet, "/api/admin/access-logs", "1", ""); response.Code != http.StatusOK {
		t.Fatalf("super GET = %d, body %s", response.Code, response.Body.String())
	}
}

func TestAccessLogCleanup(t *testing.T) {
	server, database, _, _ := newSiteAdminTestServer(t)
	now := time.Now()
	insert := func(visitedAt string) {
		t.Helper()
		if _, err := database.Exec(`INSERT INTO "`+db.AccessLogTable+`"
			("site_id", "day_key", "path", "status", "kind", "ip", "visited_at")
			VALUES (1, ?, '/cleanup', 404, 'error', '203.0.113.40', ?)`,
			now.In(time.Local).Format("2006-01-02"), visitedAt); err != nil {
			t.Fatal(err)
		}
	}
	insert(now.AddDate(0, 0, -10).UTC().Format("2006-01-02 15:04:05"))
	insert(now.Add(-time.Hour).UTC().Format("2006-01-02 15:04:05"))

	deleted, err := server.cleanupAccessLogs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("cleanup deleted = %d, want 1", deleted)
	}
	var remaining int64
	if err := database.QueryRow(`SELECT COUNT(*) FROM "` + db.AccessLogTable + `"`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatalf("remaining rows = %d, want 1", remaining)
	}
}

func TestAccessLogDisabledWithoutDatabase(t *testing.T) {
	server, err := New(nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/nope", nil)
	request.Host = "example.test"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 without a database", response.Code)
	}
}

func TestAccessLogRefererSiteResolution(t *testing.T) {
	server, database, _, siteDir := newSiteAdminTestServer(t)
	if err := os.WriteFile(filepath.Join(siteDir, "index.html"), []byte("<html><body>home</body></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE "gocms_site" SET "domain" = 'example.test' WHERE "is_default" = 1`); err != nil {
		t.Fatal(err)
	}
	// Standalone language domain of the same site, and a second site of the
	// same install that must stay external.
	if _, err := database.Exec(`UPDATE "gocms_language" SET "site_mode" = 'standalone', "domain" = 'cn.example.test' WHERE "code" = 'zh-CN'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO "gocms_site" ("name", "code", "domain", "is_default") VALUES ('Other', 'other', 'other.test', 0)`); err != nil {
		t.Fatal(err)
	}

	visit := func(referer string) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Host = "example.test"
		request.Header.Set("Referer", referer)
		request.Header.Set("User-Agent", accessLogChromeUA)
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", response.Code)
		}
	}
	visit("https://cn.example.test/")
	visit("https://other.test/")
	visit("https://www.spiraxsteam.com/")

	kindFor := func(refererHost string) string {
		t.Helper()
		var kind string
		if err := database.QueryRow(`SELECT "referer_kind" FROM "`+db.AccessLogTable+`" WHERE "referer_host" = ?`, refererHost).Scan(&kind); err != nil {
			t.Fatal(err)
		}
		return kind
	}
	if kind := kindFor("cn.example.test"); kind != "internal" {
		t.Errorf("独立语言域名 referer = %q, want internal", kind)
	}
	if kind := kindFor("other.test"); kind != "external" {
		t.Errorf("其他站点 referer = %q, want external", kind)
	}
	if kind := kindFor("spiraxsteam.com"); kind != "external" {
		t.Errorf("外部域名 referer = %q, want external", kind)
	}
}

func TestAccessLogFilterAcceptsInvalidReferer(t *testing.T) {
	filter := normalizeAccessLogFilter(AccessLogFilter{RefererKind: "invalid"})
	if filter.RefererKind != "invalid" {
		t.Fatalf("referer kind = %q, want invalid", filter.RefererKind)
	}
}

func TestAccessLogRefererFetchMetadata(t *testing.T) {
	server, database, _, siteDir := newSiteAdminTestServer(t)
	if err := os.WriteFile(filepath.Join(siteDir, "index.html"), []byte("<html>home</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE "gocms_site" SET "domain" = 'example.test' WHERE "is_default" = 1`); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name      string
		path      string
		referer   string
		fetchSite string
		want      string
	}{
		{"cross-site wins", "/missing-cross", "https://example.test/", "cross-site", "external"},
		{"same-origin valid page", "/missing-valid", "https://example.test/", "same-origin", "internal"},
		{"same-origin missing page", "/missing-fake", "https://example.test/nope", "same-origin", "invalid"},
		{"explicit none", "/missing-none", "https://example.test/", "none", "none"},
		{"no header falls back to filesystem", "/missing-fallback", "https://example.test/", "", "internal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, tc.path, nil)
			request.Host = "example.test"
			request.Header.Set("User-Agent", accessLogChromeUA)
			request.Header.Set("Referer", tc.referer)
			if tc.fetchSite != "" {
				request.Header.Set("Sec-Fetch-Site", tc.fetchSite)
			}
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", response.Code)
			}
			var got string
			if err := database.QueryRow(`SELECT "referer_kind" FROM "`+db.AccessLogTable+`" WHERE "path" = ? ORDER BY "id" DESC LIMIT 1`, tc.path).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("referer_kind = %q, want %q", got, tc.want)
			}
		})
	}
}
