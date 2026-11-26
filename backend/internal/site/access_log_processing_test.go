package site

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"gocms/internal/db"
)

func TestAccessLogClearJobPreservesNewRecords(t *testing.T) {
	server, database, token, _ := newSiteAdminTestServer(t)
	if _, err := database.Exec(`WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<450)
		INSERT INTO gocms_access_log (site_id, path) SELECT 1, '/match' FROM n`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO gocms_access_log (site_id, path) VALUES (1, '/keep'), (2, '/match')`); err != nil {
		t.Fatal(err)
	}
	response := siteAdminRequest(server, token, http.MethodPost, accessLogClearRoute+"?path=match", "1", "")
	if response.Code != http.StatusAccepted {
		t.Fatalf("start clear = %d, %s", response.Code, response.Body)
	}
	if _, err := database.Exec(`INSERT INTO gocms_access_log (site_id, path) VALUES (1, '/match')`); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; {
		response := siteAdminRequest(server, token, http.MethodGet, accessLogClearRoute, "1", "")
		var status accessLogClearStatus
		if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil || response.Code != http.StatusOK {
			t.Fatalf("get clear status = %d %s, %v", response.Code, response.Body, err)
		}
		if status.State == "completed" && status.Deleted == 450 {
			break
		}
		if status.State != "running" || time.Now().After(deadline) {
			t.Fatalf("unexpected clear state %+v", status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM gocms_access_log`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("remaining count=%d, err=%v", count, err)
	}
}

func TestAccessLogWriterDoesNotWaitForSQLiteAndDrains(t *testing.T) {
	ctx := context.Background()
	business, err := db.Open(filepath.Join(t.TempDir(), "site.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer business.Close()
	if err := db.CreateSchema(ctx, business); err != nil {
		t.Fatal(err)
	}
	server, err := New(business, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := server.StartAccessLogWriter(ctx); err != nil {
		t.Fatal(err)
	}
	defer server.CloseAccessLogWriter(ctx)
	connection, err := business.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	request := httptest.NewRequest(http.MethodGet, "http://example.test/missing", nil)
	handler := server.accessLogMiddleware(http.HandlerFunc(http.NotFound))
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), request)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("request waited for the occupied business database connection")
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	if err := server.CloseAccessLogWriter(ctx); err != nil {
		t.Fatal(err)
	}
	var businessCount int
	if err := business.QueryRow(`SELECT COUNT(*) FROM gocms_access_log`).Scan(&businessCount); err != nil || businessCount != 0 {
		t.Fatalf("business access log count=%d, err=%v", businessCount, err)
	}
	logs, err := db.OpenAccessLogs(ctx, business, server.accessLogs.cutoff())
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	var status int
	if err := logs.QueryRow(`SELECT status FROM gocms_access_log WHERE path='/missing'`).Scan(&status); err != nil || status != http.StatusNotFound {
		t.Fatalf("persisted status=%d, err=%v", status, err)
	}
}

func TestAccessLogWriterReclassifiesRetainedCrawlers(t *testing.T) {
	ctx := context.Background()
	business, err := db.Open(filepath.Join(t.TempDir(), "site.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer business.Close()
	if err := db.CreateSchema(ctx, business); err != nil {
		t.Fatal(err)
	}
	server, err := New(business, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	logs, err := db.OpenAccessLogs(ctx, business, server.accessLogs.cutoff())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	recent := now.Add(-time.Hour).Format(accessLogTimestampLayout)
	expired := now.AddDate(0, 0, -7).Format(accessLogTimestampLayout)
	_, err = logs.ExecContext(ctx, `INSERT INTO gocms_access_log
		(path, ip, user_agent, agent_kind, device_kind, visited_at) VALUES
		('/discover', '192.0.2.1', 'SITELINKDISCOVERY/0.1', 'other', 'other', ?1),
		('/scan', '192.0.2.2', ?3, 'browser', 'desktop', ?1),
		('/homepage', '192.0.2.3', ?4, 'other', 'other', ?1),
		('/normal', '192.0.2.4', ?5, 'browser', 'desktop', ?1),
		('/expired', '192.0.2.5', 'SiteLinkDiscovery/0.1', 'other', 'other', ?2)`,
		recent, expired, accessLogCortexUA, accessLogUptimeUA, accessLogChromeUA)
	closeErr := logs.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("seed existing access log database: insert=%v close=%v", err, closeErr)
	}
	if err := server.StartAccessLogWriter(ctx); err != nil {
		t.Fatal(err)
	}
	defer server.CloseAccessLogWriter(ctx)

	crawlers, total, err := server.listAccessLogs(ctx, 1, AccessLogFilter{AgentKind: "bot"}, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(crawlers) != 2 ||
		crawlers[0].AgentLabel != "Cortex Xpanse" || crawlers[0].DeviceKind != "other" ||
		crawlers[1].AgentLabel != "SiteLinkDiscovery" {
		t.Fatalf("unexpected crawler logs: total=%d rows=%+v", total, crawlers)
	}
	summary, err := server.accessLogSummaryForWindow(ctx, 1, true, "24h")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Metrics.RecentNewUsers != 2 || summary.Metrics.RecentUsers != 2 ||
		summary.Metrics.RecentVisits != 4 || len(summary.TopPages) != 2 {
		t.Fatalf("unexpected dashboard summary: %+v", summary)
	}
}

func TestAccessLogQueueReservesCapacityForErrors(t *testing.T) {
	writer := &accessLogWriter{queue: make(chan accessLogEvent, accessLogQueueSize)}
	for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusInternalServerError} {
		for range accessLogQueueSize {
			writer.enqueue(accessLogEvent{status: status})
		}
	}
	if got := len(writer.queue); got != accessLogQueueSize {
		t.Fatalf("queue length=%d, want %d", got, accessLogQueueSize)
	}
	var counts [3]int
	for range accessLogQueueSize {
		event := <-writer.queue
		counts[event.priority()]++
	}
	if counts[1] == 0 || counts[2] == 0 {
		t.Fatalf("queue full of ordinary traffic left no room for errors: %v", counts)
	}
	for priority, queued := range counts {
		if dropped := writer.dropped[priority].Load(); dropped != uint64(accessLogQueueSize-queued) {
			t.Errorf("priority %d dropped=%d, queued=%d", priority, dropped, queued)
		}
	}
}
