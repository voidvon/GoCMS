package site

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gocms/internal/db"
	"gocms/internal/nginxguard"
	"gocms/internal/security"
)

func TestSecurityTenantIsolationAndLifecycle(t *testing.T) {
	s, database, token, _ := newSiteAdminTestServer(t)
	if _, err := database.Exec(`UPDATE gocms_site SET domain='alpha.example.test',aliases='["alias.example.test"]' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	other, err := db.CreateSite(context.Background(), database, &db.Site{Name: "Different industry", Code: "other", Domain: "beta.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO gocms_language(site_id,code,name,is_enabled,site_mode,domain) VALUES(1,'fr','French',1,'standalone','fr.example.test')`); err != nil {
		t.Fatal(err)
	}
	call := func(method, endpoint, scope, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		w := siteAdminRequest(s, token, method, endpoint, scope, body)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, endpoint, w.Code, w.Body.String())
		}
		return w
	}
	call("PUT", "/api/admin/security/policy", "1", `{"enabled":true,"window_seconds":300,"threshold":2,"ban_scope":"site","ban_seconds":3600,"paths":["/server.key","/.git/*"]}`, 200)
	public := func(host, path, ip string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "https://"+host+path, nil)
		r.RemoteAddr = ip + ":1234"
		r.Header.Set("X-Site-Id", fmt.Sprint(other.ID))
		r.Header.Set("X-Forwarded-For", "1.1.1.1")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	ip := "203.0.113.25"
	for i, host := range []string{"alpha.example.test", "fr.example.test"} {
		if w := public(host, "/server.key", ip); w.Code != 403 {
			t.Fatalf("probe %d: %d", i, w.Code)
		}
	}
	for _, host := range []string{"alpha.example.test", "alias.example.test", "fr.example.test"} {
		if w := public(host, "/ordinary", ip); w.Code != 403 {
			t.Fatalf("ban missing for %s: %d", host, w.Code)
		}
	}
	if w := public("beta.example.test", "/ordinary", ip); w.Code == 403 {
		t.Fatal("site ban leaked across tenants")
	}
	items, total, err := security.ListEntries(context.Background(), database, 1, true, 1)
	if err != nil || total != 1 || items[0].Source != "automatic" || items[0].Network != "203.0.113.25/32" || items[0].ExpiresAt == "" {
		t.Fatalf("automatic ban: %+v %d %v", items, total, err)
	}
	for i := 0; i < 10; i++ {
		public("alpha.example.test", "/normal-missing-page", "203.0.113.26")
	}
	_, total, _ = security.ListEntries(context.Background(), database, 1, true, 1)
	if total != 1 {
		t.Fatal("ordinary 404 caused a ban")
	}
	// A site allow supersedes its deny, but cannot supersede a platform deny.
	call("POST", "/api/admin/security/entries", "1", `{"network":"203.0.113.25","kind":"allow","reason":"known monitor"}`, 201)
	if w := public("alpha.example.test", "/server.key", ip); w.Code == 403 {
		t.Fatal("site whitelist ignored")
	}
	global := call("POST", "/api/admin/security/entries?scope=global", "1", `{"network":"203.0.113.0/24","kind":"deny","reason":"platform ban"}`, 201)
	if w := public("alpha.example.test", "/ordinary", ip); w.Code != 403 {
		t.Fatal("site whitelist overrode global deny")
	}
	var entry security.Entry
	if err := json.Unmarshal(global.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if w := siteAdminRequest(s, token, "DELETE", fmt.Sprintf("/api/admin/security/entries/%d", entry.ID), "1", ""); w.Code != 404 {
		t.Fatal("global record deleted through site scope")
	}
	call("POST", "/api/admin/security/entries?scope=global", "1", `{"network":"203.0.113.25","kind":"allow","reason":"platform monitor"}`, 201)
	if w := public("alpha.example.test", "/ordinary", ip); w.Code == 403 {
		t.Fatal("global allow did not override deny")
	}
	// Simulate expiration in storage, then reload the cache as after a restart.
	if _, err := database.Exec(`UPDATE gocms_security_entry SET expires_at=? WHERE kind='allow' OR site_id=0`, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	s.security.Invalidate()
	if w := public("alpha.example.test", "/ordinary", ip); w.Code != 403 {
		t.Fatal("expired whitelist still applied")
	}
	if _, err := database.Exec(`UPDATE gocms_security_entry SET expires_at=?`, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	// Force refresh for the SQL edit; production expiration uses each rule's timestamp.
	s.security.Invalidate()
	if w := public("alpha.example.test", "/ordinary", ip); w.Code == 403 {
		t.Fatal("expired deny remained active")
	}
	// Settings and records survive reinitialization; site deletion removes only its own data.
	if _, err := New(database, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	call("POST", "/api/admin/security/entries", fmt.Sprint(other.ID), `{"network":"198.51.100.20","kind":"deny","reason":"other"}`, 201)
	if w := public("unconfigured.example.test", "/ordinary", "198.51.100.20"); w.Code != 403 {
		t.Fatal("unknown-host site selector bypassed the selected tenant's ban")
	}
	if w := public("alpha.example.test", "/ordinary", "198.51.100.20"); w.Code == 403 {
		t.Fatal("site selector overrode the configured host")
	}
	if err := db.DeleteSite(context.Background(), database, other.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM gocms_security_entry WHERE site_id=?`, other.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("deleted site data retained: %d %v", count, err)
	}
}

func TestSecurityGlobalPermanentDefaults(t *testing.T) {
	s, database, token, _ := newSiteAdminTestServer(t)
	ctx := context.Background()
	if _, err := database.Exec(`UPDATE gocms_site SET domain='alpha.example.test' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateSite(ctx, database, &db.Site{Name: "Another site", Code: "another", Domain: "beta.example.test"}); err != nil {
		t.Fatal(err)
	}
	w := siteAdminRequest(s, token, "GET", "/api/admin/security/policy", "1", "")
	var p security.Policy
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil || w.Code != 200 || p.BanScope != "global" || p.BanSeconds != 0 || p.Enabled {
		t.Fatalf("unexpected defaults: %+v %d %v", p, w.Code, err)
	}
	p.Enabled, p.Threshold = true, 2
	raw, _ := json.Marshal(p)
	if w := siteAdminRequest(s, token, "PUT", "/api/admin/security/policy", "1", string(raw)); w.Code != 200 {
		t.Fatalf("save global permanent policy: %d %s", w.Code, w.Body.String())
	}
	public := func(server *Server, host, path string) int {
		r := httptest.NewRequest("GET", "https://"+host+path, nil)
		r.RemoteAddr = "203.0.113.40:1234"
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, r)
		return w.Code
	}
	for i := 0; i < p.Threshold; i++ {
		if got := public(s, "alpha.example.test", "/server.key"); got != 403 {
			t.Fatalf("probe: %d", got)
		}
	}
	entries, total, err := security.ListEntries(ctx, database, 0, true, 1)
	if err != nil || total != 1 || entries[0].ExpiresAt != "" || entries[0].Source != "automatic" {
		t.Fatalf("permanent global ban: %+v %d %v", entries, total, err)
	}
	for _, host := range []string{"alpha.example.test", "beta.example.test"} {
		if got := public(s, host, "/ordinary"); got != 403 {
			t.Fatalf("global ban missing for %s: %d", host, got)
		}
	}
	events, total, err := security.ListEvents(ctx, database, 0, 1)
	if err != nil || total != 1 || events[0].Action != "ban" || events[0].Host != "alpha.example.test" {
		t.Fatalf("global ban event: %+v %d %v", events, total, err)
	}
	restarted, err := New(database, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := public(restarted, "beta.example.test", "/ordinary"); got != 403 {
		t.Fatalf("permanent ban lost on restart: %d", got)
	}
	if ok, err := security.RevokeEntry(ctx, database, 0, entries[0].ID); err != nil || !ok {
		t.Fatalf("revoke permanent global ban: %t %v", ok, err)
	}
	restarted.security.Invalidate()
	if got := public(restarted, "beta.example.test", "/ordinary"); got == 403 {
		t.Fatal("revoked permanent global ban still applied")
	}
	// Existing configurations retain their saved duration and original site scope.
	if err := security.SaveConfig(ctx, database, 1, map[string]any{"enabled": true, "window_seconds": 300, "threshold": 5, "ban_seconds": 3600, "paths": []string{"/server.key"}}); err != nil {
		t.Fatal(err)
	}
	legacy, err := security.LoadPolicy(ctx, database, 1)
	if err != nil || legacy.BanScope != "site" || legacy.BanSeconds != 3600 {
		t.Fatalf("legacy policy changed: %+v %v", legacy, err)
	}
}

func TestSecurityPermissionsAndPlatformBoundary(t *testing.T) {
	s, database, root, _ := newSiteAdminTestServer(t)
	if _, err := database.Exec(`INSERT INTO gocms_site(id,name,code,status) VALUES(2,'Second','second','active'); INSERT INTO gocms_admin_group(name,site_permissions) VALUES('security-editor','{"1":["security"],"2":["security","security.manage"]}'); INSERT INTO gocms_admin_user(username,group_id) SELECT 'security-editor',id FROM gocms_admin_group WHERE name='security-editor'`); err != nil {
		t.Fatal(err)
	}
	token, err := s.createSession("security-editor")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path, scope, body string
		want                      int
	}{
		{"GET", "/api/admin/security/entries", "1", "", 200},
		{"POST", "/api/admin/security/entries", "1", `{"network":"203.0.113.8","kind":"deny","reason":"test"}`, 403},
		{"POST", "/api/admin/security/entries", "2", `{"network":"203.0.113.8","kind":"deny","reason":"test"}`, 201},
		{"GET", "/api/admin/security/entries", "999", "", 403},
		{"GET", "/api/admin/security/platform", "1", "", 403},
		{"GET", "/api/admin/SECURITY/PLATFORM", "2", "", 403},
		{"GET", "/api/admin/security/nginx", "2", "", 403},
		{"POST", "/api/admin/security/nginx/install", "2", "{}", 403},
		{"POST", "/api/admin/security/entries?scope=global", "2", `{"network":"203.0.113.8","kind":"deny","reason":"test"}`, 403},
	} {
		w := siteAdminRequest(s, token, tc.method, tc.path, tc.scope, tc.body)
		if w.Code != tc.want {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
	key := apiKeyForTest(t, s, "security-editor")
	if w := apiKeyRequest(t, s, key.Key, "GET", "/api/admin/security/platform", ""); w.Code != 403 {
		t.Fatal("API key bypassed platform boundary")
	}
	p := security.DefaultPolicy()
	p.Enabled = true
	raw, _ := json.Marshal(p)
	if w := siteAdminRequest(s, token, "PUT", "/api/admin/security/policy", "2", string(raw)); w.Code != 403 {
		t.Fatal("site editor configured global automatic bans")
	}
	if w := apiKeyRequest(t, s, key.Key, "PUT", "/api/admin/security/policy", string(raw)); w.Code != 403 {
		t.Fatal("API key configured global automatic bans")
	}
	if w := siteAdminRequest(s, root, "PUT", "/api/admin/security/policy", "2", string(raw)); w.Code != 200 {
		t.Fatalf("super admin global policy: %d %s", w.Code, w.Body.String())
	}
	p.Enabled, p.BanScope = false, "site"
	raw, _ = json.Marshal(p)
	if w := siteAdminRequest(s, token, "PUT", "/api/admin/security/policy", "2", string(raw)); w.Code != 403 {
		t.Fatal("site editor disabled a global automatic ban policy")
	}
	if w := siteAdminRequest(s, root, "PUT", "/api/admin/security/policy", "2", string(raw)); w.Code != 200 {
		t.Fatalf("super admin site policy: %d %s", w.Code, w.Body.String())
	}
	if w := siteAdminRequest(s, token, "PUT", "/api/admin/security/policy", "2", string(raw)); w.Code != 200 {
		t.Fatalf("site editor site policy: %d %s", w.Code, w.Body.String())
	}
	if w := siteAdminRequest(s, "", "GET", "/api/admin/security/entries", "1", ""); w.Code != 401 {
		t.Fatal("anonymous security access")
	}
	for _, path := range []string{"/api/*", "/admin/*", "/.well-known/*", "/*", "/x;include"} {
		p := security.DefaultPolicy()
		p.Paths = []string{path}
		raw, _ := json.Marshal(p)
		if w := siteAdminRequest(s, root, "PUT", "/api/admin/security/policy", "1", string(raw)); w.Code != 400 {
			t.Fatalf("unsafe path accepted %s: %d %s", path, w.Code, w.Body.String())
		}
	}
	w := siteAdminRequest(s, root, "POST", "/api/admin/security/nginx/install", "1", "{}")
	archive, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if w.Code != 200 || err != nil || len(archive.File) != 3 {
		t.Fatalf("install bundle: %d %v", w.Code, err)
	}
	if w := siteAdminRequest(s, root, "POST", "/api/admin/security/entries", "1", `{"network":"203.0.113.1; include /etc/passwd","kind":"deny","reason":"test"}`); w.Code != 400 {
		t.Fatal("network injection accepted")
	}
	if w := siteAdminRequest(s, root, "POST", "/api/admin/security/entries", "1", `{"network":"0.0.0.0/0","kind":"deny","reason":"lock myself out"}`); w.Code != 400 {
		t.Fatal("operator self-ban allowed")
	}
	// Configuration is generated as data, not embedded executable statements.
	if strings.Contains(w.Body.String(), "sudo") {
		t.Fatal("unexpected command response")
	}
}

func TestSecuritySyncAcknowledgementRetryAndDisable(t *testing.T) {
	s, database, token, _ := newSiteAdminTestServer(t)
	dir, err := os.MkdirTemp("", "gocms-agent-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "agent.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var fail atomic.Bool
	fail.Store(true)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var snapshot nginxguard.Snapshot
		if err := json.NewDecoder(r.Body).Decode(&snapshot); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if fail.Load() {
			w.WriteHeader(502)
			_, _ = w.Write([]byte(`{"error":"nginx check failed"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(nginxguard.Result{Digest: nginxguard.Digest(snapshot), AppliedAt: time.Now().UTC().Format(time.RFC3339)})
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	p := security.DefaultPlatform()
	p.NginxEnabled = true
	p.SocketPath = socket
	call := func(method, path, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		w := siteAdminRequest(s, token, method, path, "1", body)
		if w.Code != want {
			t.Fatalf("%s %s %d %s", method, path, w.Code, w.Body.String())
		}
		return w
	}
	raw, _ := json.Marshal(p)
	call("PUT", "/api/admin/security/platform", string(raw), 200)
	call("POST", "/api/admin/security/nginx/sync", "{}", 502)
	if status := s.security.Status(context.Background()); status.State != "failed" || !strings.Contains(status.Error, "nginx check failed") {
		t.Fatalf("failure status %+v", status)
	}
	fail.Store(false)
	call("POST", "/api/admin/security/nginx/sync", "{}", 200)
	if status := s.security.Status(context.Background()); status.State != "applied" {
		t.Fatalf("success status %+v", status)
	}
	call("POST", "/api/admin/security/entries", `{"network":"203.0.113.5","kind":"deny","reason":"test sync"}`, 201)
	if status := s.security.Status(context.Background()); status.State != "pending" {
		t.Fatalf("pending status %+v", status)
	}
	call("POST", "/api/admin/security/nginx/sync", "{}", 200)
	p.NginxEnabled = false
	raw, _ = json.Marshal(p)
	call("PUT", "/api/admin/security/platform", string(raw), 200)
	if status := s.security.Status(context.Background()); status.State != "pending" {
		t.Fatalf("cleanup not marked pending %+v", status)
	}
	restarted, err := New(database, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if status := restarted.security.Status(context.Background()); status.State != "pending" {
		t.Fatalf("restart forgot outstanding cleanup %+v", status)
	}
	call("POST", "/api/admin/security/nginx/sync", "{}", 200)
	if status := s.security.Status(context.Background()); status.State != "disabled" {
		t.Fatalf("cleanup failed %+v", status)
	}
}
