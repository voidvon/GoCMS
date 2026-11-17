package site

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"gocms/internal/db"
)

func TestDashboardStatsCompactAndSiteIsolation(t *testing.T) {
	server, database, token, _ := newSiteAdminTestServer(t)
	if _, err := database.Exec(`INSERT INTO gocms_site (name, code, domain) VALUES ('Other', 'other', 'other.test');
		INSERT INTO gocms_access_log (site_id, day_key, host, path, status, kind, ip, agent_kind)
		VALUES (1, date('now'), 'example.test', '/article', 200, 'page', '203.0.113.1', 'browser'),
		       (1, date('now'), 'example.test', '/missing', 404, 'error', '203.0.113.1', 'browser'),
		       (2, date('now'), 'other.test', '/other', 500, 'error', '203.0.113.2', 'browser')`); err != nil {
		t.Fatal(err)
	}
	get := func(siteID, query string) AdminStats {
		t.Helper()
		response := siteAdminRequest(server, token, http.MethodGet, "/api/admin/stats"+query, siteID, "")
		if response.Code != http.StatusOK {
			t.Fatalf("stats = %d: %s", response.Code, response.Body.String())
		}
		var stats AdminStats
		if err := json.Unmarshal(response.Body.Bytes(), &stats); err != nil {
			t.Fatal(err)
		}
		return stats
	}
	full := get("1", "")
	compact := get("1", "?include_top=false")
	if !reflect.DeepEqual(full.Access.Metrics, compact.Access.Metrics) || !reflect.DeepEqual(full.Access.StatusBreakdown, compact.Access.StatusBreakdown) {
		t.Fatalf("compact stats changed headline metrics: full=%+v compact=%+v", full.Access, compact.Access)
	}
	if len(full.Access.TopPages) != 1 || len(full.Access.TopErrors) != 1 || len(compact.Access.TopPages) != 0 || len(compact.Access.TopErrors) != 0 {
		t.Fatalf("unexpected rankings: full=%+v compact=%+v", full.Access, compact.Access)
	}
	if got := get("2", "?include_top=false").Access.Metrics; got.TotalRequests != 1 || got.TotalErrors != 1 || got.TotalNotFound != 0 {
		t.Fatalf("other site's metrics = %+v", got)
	}
	// Log arrivals may lag by the TTL; legacy full snapshots stay fresh.
	if _, err := database.Exec(`INSERT INTO gocms_access_log (site_id, path, status, kind) VALUES (1, '/new', 404, 'error')`); err != nil {
		t.Fatal(err)
	}
	if got := get("1", "?include_top=false").Access.Metrics.TotalRequests; got != 2 {
		t.Fatalf("compact cache missed: requests=%d", got)
	}
	if got := get("1", "").Access.Metrics.TotalRequests; got != 3 {
		t.Fatalf("legacy snapshot was cached: requests=%d", got)
	}
	// A warm cache never bypasses site authorization.
	if _, err := database.Exec(`INSERT INTO gocms_admin_user (username, password_hash, site_ids, is_super)
		VALUES ('limited-dashboard', '', '[1]', 0)`); err != nil {
		t.Fatal(err)
	}
	limitedToken, err := server.createSession("limited-dashboard")
	if err != nil {
		t.Fatal(err)
	}
	if response := siteAdminRequest(server, limitedToken, http.MethodGet, "/api/admin/stats?include_top=false", "2", ""); response.Code != http.StatusForbidden {
		t.Fatalf("warm cache leaked to restricted user: %d %s", response.Code, response.Body.String())
	}
}

func TestDashboardStatsMutationInvalidation(t *testing.T) {
	server, database, token, _ := newSiteAdminTestServer(t)
	if _, err := database.Exec(`INSERT INTO gocms_access_log (site_id, path, status, kind, visited_at)
		VALUES (1, '/missing', 404, 'error', CURRENT_TIMESTAMP),
		       (1, '/old', 500, 'error', datetime('now', '-10 days'))`); err != nil {
		t.Fatal(err)
	}
	check := func(requests, errorCount int64) {
		t.Helper()
		summary, err := server.dashboardAccessSummary(context.Background(), 1)
		if err != nil {
			t.Fatal(err)
		}
		if summary.Metrics.TotalRequests != requests || summary.Metrics.TotalErrors != errorCount {
			t.Fatalf("metrics = %+v, want %d requests, %d errors", summary.Metrics, requests, errorCount)
		}
	}
	check(2, 2)
	response := siteAdminRequest(server, token, http.MethodPost, "/api/admin/access-logs/ignores", "1", `{"path":"/missing"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("ignore = %d %s", response.Code, response.Body.String())
	}
	check(2, 1)
	response = siteAdminRequest(server, token, http.MethodDelete, "/api/admin/access-logs/ignores?path=/missing", "1", "")
	if response.Code != http.StatusOK {
		t.Fatalf("unignore = %d %s", response.Code, response.Body.String())
	}
	check(2, 2)
	if _, err := server.cleanupAccessLogs(context.Background()); err != nil {
		t.Fatal(err)
	}
	check(1, 1)
	response = siteAdminRequest(server, token, http.MethodDelete, "/api/admin/access-logs", "1", "")
	if response.Code != http.StatusOK {
		t.Fatalf("clear = %d %s", response.Code, response.Body.String())
	}
	check(0, 0)
	server.ConfigureAccessLogs(time.UTC, 7)
	summary, err := server.dashboardAccessSummary(context.Background(), 1)
	if err != nil || summary.Metrics.RetentionDays != 7 {
		t.Fatalf("retention update = %+v, %v", summary.Metrics, err)
	}
}

func TestDashboardStatsCacheCoalescesAndAllowsCancellation(t *testing.T) {
	var cache dashboardStatsCache
	var calls atomic.Int64
	started := make(chan struct{})
	release := make(chan struct{})
	load := func() (AccessLogSummary, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		return AccessLogSummary{Metrics: AccessLogMetrics{TotalRequests: 42}}, nil
	}
	results := make(chan error, 8)
	for range 8 {
		go func() {
			summary, err := cache.get(context.Background(), dashboardStatsKey{siteID: 1}, "today", 3, load)
			if err == nil && summary.Metrics.TotalRequests != 42 {
				err = errors.New("unexpected cached count")
			}
			results <- err
		}()
	}
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := cache.get(ctx, dashboardStatsKey{siteID: 1}, "today", 3, load); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("waiting request cancellation = %v", err)
	}
	close(release)
	for range 8 {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("concurrent requests ran %d aggregations, want 1", calls.Load())
	}
}

func TestDashboardStatsCacheRejectsInvalidatedComputation(t *testing.T) {
	var cache dashboardStatsCache
	started := make(chan struct{})
	release := make(chan struct{})
	result := make(chan AccessLogSummary, 1)
	go func() {
		summary, _ := cache.get(context.Background(), dashboardStatsKey{siteID: 1}, "today", 3, func() (AccessLogSummary, error) {
			close(started)
			<-release
			return AccessLogSummary{Metrics: AccessLogMetrics{TotalErrors: 10}}, nil
		})
		result <- summary
	}()
	<-started
	cache.invalidate(1)
	_, err := cache.get(context.Background(), dashboardStatsKey{siteID: 1}, "today", 3, func() (AccessLogSummary, error) {
		return AccessLogSummary{Metrics: AccessLogMetrics{TotalErrors: 0}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	if summary := <-result; summary.Metrics.TotalErrors != 0 {
		t.Fatalf("in-flight query returned pre-mutation data: %+v", summary.Metrics)
	}
}

func TestDashboardStatsCacheExpiryAndFailures(t *testing.T) {
	var cache dashboardStatsCache
	calls := 0
	load := func() (AccessLogSummary, error) {
		calls++
		if calls == 1 {
			return AccessLogSummary{}, context.Canceled
		}
		return AccessLogSummary{Metrics: AccessLogMetrics{TotalRequests: int64(calls)}}, nil
	}
	if _, err := cache.get(context.Background(), dashboardStatsKey{siteID: 1}, "today", 3, load); err == nil {
		t.Fatal("failed aggregation unexpectedly succeeded")
	}
	for range 2 {
		summary, err := cache.get(context.Background(), dashboardStatsKey{siteID: 1}, "today", 3, load)
		if err != nil || summary.Metrics.TotalRequests != 2 {
			t.Fatalf("failure was cached or successful result wasn't reused: %+v %v", summary, err)
		}
	}
	cache.entries[dashboardStatsKey{siteID: 1}].expires = time.Now().Add(-time.Second)
	if summary, _ := cache.get(context.Background(), dashboardStatsKey{siteID: 1}, "today", 3, load); summary.Metrics.TotalRequests != 3 {
		t.Fatal("expired snapshot was reused")
	}
	if summary, _ := cache.get(context.Background(), dashboardStatsKey{siteID: 1}, "tomorrow", 3, load); summary.Metrics.TotalRequests != 4 {
		t.Fatal("snapshot from previous day was reused")
	}
	if summary, _ := cache.get(context.Background(), dashboardStatsKey{siteID: 1}, "tomorrow", 7, load); summary.Metrics.TotalRequests != 5 {
		t.Fatal("snapshot from previous retention configuration was reused")
	}
}

func BenchmarkDashboardAccessSummary(b *testing.B) {
	database, err := db.Open(":memory:")
	if err != nil {
		b.Fatal(err)
	}
	defer database.Close()
	if err := db.EnsureAccessLog(context.Background(), database); err != nil {
		b.Fatal(err)
	}
	if _, err := database.Exec(`WITH RECURSIVE seq(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM seq WHERE n < 100000)
		INSERT INTO gocms_access_log (site_id, host, day_key, path, status, kind, ip, agent_kind, device_kind, visited_at)
		SELECT 1, 'example.test', date('now'), '/page/' || (n % 2000),
		       CASE WHEN n % 5 = 0 THEN 404 ELSE 200 END,
		       CASE WHEN n % 5 = 0 THEN 'error' ELSE 'page' END,
		       'ip-' || (n % 1000), CASE WHEN n % 7 = 0 THEN 'bot' ELSE 'browser' END,
		       'desktop', datetime('now', '-' || (n % 72) || ' hours') FROM seq`); err != nil {
		b.Fatal(err)
	}
	server := &Server{database: database, accessLogs: newAccessLogConfig()}
	for _, mode := range []string{"full", "compact", "cached"} {
		b.Run(mode, func(b *testing.B) {
			if mode == "cached" {
				if _, err := server.dashboardAccessSummary(context.Background(), 1); err != nil {
					b.Fatal(err)
				}
			}
			b.ResetTimer()
			for b.Loop() {
				var err error
				if mode == "cached" {
					_, err = server.dashboardAccessSummary(context.Background(), 1)
				} else {
					_, err = server.accessLogSummaryWithTop(context.Background(), 1, mode == "full")
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
