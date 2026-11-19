package site

import (
	"encoding/json"
	"net/http"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestDashboardTimeWindowsAndRefresh(t *testing.T) {
	server, database, token, _ := newSiteAdminTestServer(t)
	// Keep local noon stable at any execution time. Calendar-day tests must
	// exercise a configured statistics zone rather than SQLite's UTC date.
	now := time.Now()
	zone := time.FixedZone("statistics", (12-now.UTC().Hour())*60*60)
	server.ConfigureAccessLogs(zone, 3)
	local := now.In(zone)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, zone)
	yesterday := today.AddDate(0, 0, -1)
	if _, err := database.Exec(`INSERT INTO gocms_site (id, name, code) VALUES (2, 'Other', 'other')`); err != nil {
		t.Fatal(err)
	}
	insert := func(siteID int64, at time.Time, path, ip string, status int, agent, referer string) {
		t.Helper()
		kind := "page"
		if status >= 400 {
			kind = "error"
		}
		if _, err := database.Exec(`INSERT INTO gocms_access_log
			(site_id, day_key, host, path, status, kind, ip, agent_kind, device_kind, referer_kind, visited_at)
			VALUES (?, ?, 'example.test', ?, ?, ?, ?, ?, 'desktop', ?, ?)`,
			siteID, at.In(zone).Format("2006-01-02"), path, status, kind, ip, agent, referer, at.UTC().Format("2006-01-02 15:04:05")); err != nil {
			t.Fatal(err)
		}
	}
	insert(1, yesterday.Add(-time.Hour), "/before", "returning", 200, "browser", "none")
	insert(1, yesterday, "/y-boundary", "y-boundary", 200, "browser", "none")
	insert(1, yesterday.Add(time.Hour), "/y-early", "y-new", 200, "browser", "none")
	insert(1, yesterday.Add(23*time.Hour), "/y-late", "y-new", 200, "browser", "none")
	insert(1, today, "/today-boundary", "today-new", 200, "browser", "none")
	insert(1, today.Add(2*time.Hour), "/today-repeat", "returning", 200, "browser", "none")
	insert(1, today.Add(3*time.Hour), "/bot", "bot", 200, "bot", "none")
	insert(1, today.Add(4*time.Hour), "/today-error", "error", 500, "browser", "none")
	insert(1, yesterday.Add(22*time.Hour), "/y-error", "error", 404, "browser", "internal")
	insert(2, today, "/other-site", "other", 500, "browser", "none")
	get := func(path string, destination any) {
		t.Helper()
		response := siteAdminRequest(server, token, http.MethodGet, path, "1", "")
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", path, response.Code, response.Body.String())
		}
		if err := json.Unmarshal(response.Body.Bytes(), destination); err != nil {
			t.Fatal(err)
		}
	}
	var baseline AdminStats
	get("/api/admin/stats", &baseline)
	for _, tc := range []struct {
		window                               string
		requests, visits, users, newUsers    int64
		errors, notFound, internal404, pages int64
		popular, errorPaths                  []string
	}{
		{"24h", 6, 4, 3, 1, 2, 1, 1, 4, []string{"/today-boundary", "/today-repeat", "/y-late"}, []string{"/today-error", "/y-error"}},
		{"today", 4, 3, 2, 1, 1, 0, 0, 3, []string{"/today-boundary", "/today-repeat"}, []string{"/today-error"}},
		{"yesterday", 4, 3, 2, 2, 1, 1, 1, 3, []string{"/y-boundary", "/y-early", "/y-late"}, []string{"/y-error"}},
	} {
		t.Run(tc.window, func(t *testing.T) {
			var full, compact AdminStats
			get("/api/admin/stats?window="+tc.window, &full)
			get("/api/admin/stats?include_top=false&window="+tc.window, &compact)
			m := compact.Access.Metrics
			if m.TotalRequests != tc.requests || m.RecentVisits != tc.visits || m.RecentUsers != tc.users || m.RecentNewUsers != tc.newUsers || m.TotalErrors != tc.errors || m.RecentErrors != tc.errors || m.TotalNotFound != tc.notFound || m.InternalNotFound != tc.internal404 || m.TotalPages != tc.pages {
				t.Fatalf("unexpected %s metrics: %+v", tc.window, m)
			}
			if !reflect.DeepEqual(full.Access.Metrics, m) || !reflect.DeepEqual(full.Access.StatusBreakdown, compact.Access.StatusBreakdown) {
				t.Fatalf("full and compact snapshots disagree: full=%+v compact=%+v", full.Access, compact.Access)
			}
			if full.Contents != baseline.Contents || full.Messages != baseline.Messages {
				t.Fatal("time range changed current content/message counts")
			}
			var statusHits int64
			for _, status := range compact.Access.StatusBreakdown {
				statusHits += status.Hits
			}
			if statusHits != tc.errors {
				t.Fatalf("status distribution included errors from another range: %+v", compact.Access.StatusBreakdown)
			}
			var pages struct {
				Items []AccessLogPageStat
				Total int
			}
			var errors struct {
				Items []AccessLogErrorStat
				Total int
			}
			get("/api/admin/access-logs/pages?window="+tc.window, &pages)
			get("/api/admin/access-logs/errors?window="+tc.window+"&sort=recent", &errors)
			pagePaths, errorPaths := []string{}, []string{}
			for _, page := range pages.Items {
				pagePaths = append(pagePaths, page.Path)
			}
			for _, item := range errors.Items {
				errorPaths = append(errorPaths, item.Path)
			}
			sort.Strings(pagePaths)
			sort.Strings(errorPaths)
			if !reflect.DeepEqual(pagePaths, tc.popular) || !reflect.DeepEqual(errorPaths, tc.errorPaths) || pages.Total != len(tc.popular) || errors.Total != len(tc.errorPaths) {
				t.Fatalf("rankings escaped selected range: pages=%+v errors=%+v", pages, errors)
			}
			if !reflect.DeepEqual(full.Access.TopPages, pages.Items) || !reflect.DeepEqual(full.Access.TopErrors, errors.Items) {
				t.Fatal("summary rankings and paginated lists disagree")
			}
			firstPage := pages.Items[1]
			get("/api/admin/access-logs/pages?window="+tc.window+"&limit=1&offset=1", &pages)
			if pages.Total != len(tc.popular) || len(pages.Items) != 1 || !reflect.DeepEqual(pages.Items[0], firstPage) {
				t.Fatalf("range lost on pagination: %+v", pages)
			}
		})
	}
	// All three warm range caches stay independent; refresh must expose a new
	// arrival immediately, rather than waiting for the 20-second cache TTL.
	insert(1, now, "/fresh", "fresh", 200, "browser", "none")
	var stats AdminStats
	get("/api/admin/stats?include_top=false&window=24h", &stats)
	if stats.Access.Metrics.TotalRequests != 6 {
		t.Fatal("range cache was overwritten by another selection")
	}
	get("/api/admin/stats?include_top=false&window=24h&refresh=true", &stats)
	if stats.Access.Metrics.TotalRequests != 7 {
		t.Fatalf("manual refresh reused cached counts: %+v", stats.Access.Metrics)
	}
	get("/api/admin/stats?include_top=false&window=today", &stats)
	if stats.Access.Metrics.TotalRequests != 5 {
		t.Fatal("refresh did not invalidate the other range for this site")
	}
	get("/api/admin/stats?include_top=false&window=yesterday", &stats)
	if stats.Access.Metrics.TotalRequests != 4 {
		t.Fatal("today's arrival changed yesterday's counts")
	}
	response := siteAdminRequest(server, token, http.MethodGet, "/api/admin/stats?window=unsupported", "1", "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unsupported window accepted: %d", response.Code)
	}
}

func TestDashboardRefreshReportsAggregationFailure(t *testing.T) {
	server, database, token, _ := newSiteAdminTestServer(t)
	if _, err := server.dashboardAccessSummaryForWindow(t.Context(), 1, "24h"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`ALTER TABLE gocms_access_log RENAME TO temporarily_unavailable_log`); err != nil {
		t.Fatal(err)
	}
	response := siteAdminRequest(server, token, http.MethodGet, "/api/admin/stats?include_top=false&window=24h&refresh=true", "1", "")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("failed refresh reported zero traffic as success: %d %s", response.Code, response.Body.String())
	}
}
