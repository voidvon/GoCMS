package site

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocms/internal/apikey"
	"gocms/internal/db"
)

func TestRedirectSchemaUpgrade(t *testing.T) {
	_, database, _, root := newSiteAdminTestServer(t)
	if _, err := database.Exec(`DROP TABLE gocms_redirect`); err != nil {
		t.Fatal(err)
	}
	server, err := New(database, root)
	if err != nil {
		t.Fatal(err)
	}
	rule := newRedirectRule()
	rule.SourcePath, rule.TargetURL = "/legacy", "/current"
	if err := server.saveRedirects(context.Background(), 1, []redirectRule{rule}); err != nil {
		t.Fatal(err)
	}
	if _, err := New(database, root); err != nil {
		t.Fatal(err)
	}
	items, err := listRedirects(context.Background(), database, 1)
	if err != nil || len(items) != 1 || items[0].TargetURL != "/current" {
		t.Fatalf("upgrade/restart lost rules: %+v %v", items, err)
	}
}

func TestConfiguredRedirectRoutingAndLifecycle(t *testing.T) {
	s, database, token, root := newSiteAdminTestServer(t)
	call := func(method, endpoint, site, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		res := siteAdminRequest(s, token, method, endpoint, site, body)
		if res.Code != want {
			t.Fatalf("%s %s site %s: %d %s", method, endpoint, site, res.Code, res.Body.String())
		}
		return res
	}
	otherSite, err := db.CreateSite(context.Background(), database, &db.Site{Name: "Other", Code: "other", Domain: "other.example.test", Aliases: []string{"alias.example.test"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO gocms_language (site_id, code, name, is_enabled, site_mode, domain) VALUES (?, 'fr', 'French', 1, 'standalone', 'fr.example.test')`, otherSite.ID); err != nil {
		t.Fatal(err)
	}
	// A real published file is superseded immediately, without generation.
	if err := os.WriteFile(filepath.Join(root, "old.html"), []byte("old page"), 0600); err != nil {
		t.Fatal(err)
	}
	call("POST", "/api/admin/redirects", "1", `{"source_path":"/old.html/","target_url":"/new?campaign=target#section"}`, 200)
	call("POST", "/api/admin/redirects", fmt.Sprint(otherSite.ID), `{"source_path":"/old.html","target_url":"https://external.example.test/new","status_code":302,"preserve_query":false}`, 200)
	public := func(method, host, path string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		req.Host = host
		req.Header.Set("X-Site-Id", "1") // Cannot override configured host ownership.
		res := httptest.NewRecorder()
		s.Handler().ServeHTTP(res, req)
		return res
	}
	for _, method := range []string{"GET", "HEAD"} {
		res := public(method, "localhost", "/old.html/?campaign=source&utm_source=search&tag=a&tag=b")
		if res.Code != 301 || res.Header().Get("Location") != "/new?campaign=target&tag=a&tag=b&utm_source=search#section" || res.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("default site redirect: %d %v", res.Code, res.Header())
		}
		if method == "HEAD" && res.Body.Len() != 0 {
			t.Fatal("HEAD redirect has a body")
		}
	}
	for _, host := range []string{"other.example.test", "alias.example.test", "fr.example.test"} {
		res := public("GET", host, "/old.html?site_id=1&ignored=1")
		if res.Code != 302 || res.Header().Get("Location") != "https://external.example.test/new" {
			t.Fatalf("site isolation %s: %d %v", host, res.Code, res.Header())
		}
	}
	for _, path := range []string{"", "/child"} {
		if res := public("GET", "localhost", "/old.html"+path+"/child"); res.Header().Get("Location") == "/new" || res.Code == 301 {
			t.Fatalf("redirect unexpectedly matches descendants: %d %v", res.Code, res.Header())
		}
	}
	if res := public("POST", "localhost", "/old.html"); res.Header().Get("Location") != "" {
		t.Fatal("POST was redirected")
	}
	var listed struct {
		Items []redirectRule `json:"items"`
	}
	if err := json.Unmarshal(call("GET", "/api/admin/redirects", "1", "", 200).Body.Bytes(), &listed); err != nil || len(listed.Items) != 1 {
		t.Fatalf("list: %+v %v", listed, err)
	}
	id := listed.Items[0].ID
	call("PUT", fmt.Sprintf("/api/admin/redirects/%d", id), fmt.Sprint(otherSite.ID), `{"source_path":"/old.html","target_url":"/hijack"}`, 404)
	call("DELETE", fmt.Sprintf("/api/admin/redirects/%d", id), fmt.Sprint(otherSite.ID), "", 404)
	call("PUT", fmt.Sprintf("/api/admin/redirects/%d", id), "1", `{"source_path":"/old.html","target_url":"/new","enabled":false}`, 200)
	if res := public("GET", "localhost", "/old.html"); res.Code != 200 || res.Body.String() != "old page" {
		t.Fatalf("disable did not restore file: %d %s", res.Code, res.Body.String())
	}
	call("DELETE", fmt.Sprintf("/api/admin/redirects/%d", id), "1", "", 200)
	// Explicit site deletion cleans up its configuration as well.
	if err := db.DeleteSite(context.Background(), database, otherSite.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM gocms_redirect`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("redirect cleanup: %d %v", count, err)
	}
}

func TestRedirectValidationAndAtomicImport(t *testing.T) {
	s, database, token, _ := newSiteAdminTestServer(t)
	call := func(endpoint, body string, want int) {
		t.Helper()
		res := siteAdminRequest(s, token, "POST", endpoint, "1", body)
		if res.Code != want {
			t.Fatalf("%s %s: %d %s", endpoint, body, res.Code, res.Body.String())
		}
	}
	for _, input := range []string{
		`{"source_path":"//other/path","target_url":"/new"}`,
		`{"source_path":"/old?query=1","target_url":"/new"}`,
		`{"source_path":"/old#anchor","target_url":"/new"}`,
		`{"source_path":"/x/../API/admin","target_url":"/new"}`,
		`{"source_path":"/shop/cart","target_url":"/new"}`,
		`{"source_path":"/assets/7/themes/demo/templates/index.html","target_url":"/new"}`,
		`{"source_path":"/old%0d%0aLocation:evil","target_url":"/new"}`,
		`{"source_path":"/old","target_url":"javascript:alert(1)"}`,
		`{"source_path":"/old","target_url":"//other/path"}`,
		`{"source_path":"/old","target_url":"https://user:pass@example.test/"}`,
		`{"source_path":"/old","target_url":"/%5cevil"}`,
		`{"source_path":"/old","target_url":"/new","status_code":310}`,
	} {
		call("/api/admin/redirects", input, 400)
	}
	call("/api/admin/redirects", `{"source_path":"/a/","target_url":"/b"}`, 200)
	call("/api/admin/redirects", `{"source_path":"/a","target_url":"/different"}`, 409)
	call("/api/admin/redirects", `{"source_path":"/b","target_url":"/a/?q=1"}`, 409)
	call("/api/admin/redirects", `{"source_path":"/self","target_url":"/self/"}`, 409)
	call("/api/admin/redirects/import", `{"items":[{"source_path":"/fresh","target_url":"/new"},{"source_path":"/a/","target_url":"/other"}]}`, 409)
	call("/api/admin/redirects/import", `{"items":[{"source_path":"/x","target_url":"/y"},{"source_path":"/y","target_url":"/x"}]}`, 409)
	call("/api/admin/redirects/import", `{"items":[null]}`, 400)
	call("/api/admin/redirects/import", `{"items":[{"source_path":"/fresh","target_url":"/new"},{"source_path":"/outside","target_url":"https://unconfigured.example.test/outside"}]}`, 201)
	items, err := listRedirects(context.Background(), database, 1)
	if err != nil || len(items) != 3 {
		t.Fatalf("atomic import: %+v %v", items, err)
	}
	for _, rule := range items {
		if !rule.Enabled || !rule.PreserveQuery || rule.StatusCode != 301 {
			t.Fatalf("import defaults missing: %+v", rule)
		}
	}
	// A disabled edge is permitted until activation would close the cycle.
	call("/api/admin/redirects", `{"source_path":"/b","target_url":"/a","enabled":false}`, 200)
	var id int64
	if err := database.QueryRow(`SELECT id FROM gocms_redirect WHERE source_path='/b'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	res := siteAdminRequest(s, token, "PUT", fmt.Sprintf("/api/admin/redirects/%d", id), "1", `{"source_path":"/b","target_url":"/a","enabled":true}`)
	if res.Code != 409 {
		t.Fatalf("cycle activation: %d %s", res.Code, res.Body.String())
	}
}

func TestRedirectCrossSiteCycles(t *testing.T) {
	s, database, token, _ := newSiteAdminTestServer(t)
	if _, err := database.Exec(`UPDATE gocms_site SET domain='primary.example.test', aliases='["alias.example.test"]' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	otherSite, err := db.CreateSite(context.Background(), database, &db.Site{Name: "Other", Code: "other", Domain: "other.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	res := siteAdminRequest(s, token, "POST", "/api/admin/redirects", "1", `{"source_path":"/a","target_url":"https://other.example.test/b"}`)
	if res.Code != 200 {
		t.Fatal(res.Body.String())
	}
	res = siteAdminRequest(s, token, "POST", "/api/admin/redirects", fmt.Sprint(otherSite.ID), `{"source_path":"/b","target_url":"https://alias.example.test/a/"}`)
	if res.Code != 409 {
		t.Fatalf("cross-site cycle accepted: %d %s", res.Code, res.Body.String())
	}
	res = siteAdminRequest(s, token, "POST", "/api/admin/redirects", "1", `{"source_path":"/self","target_url":"https://primary.example.test/self"}`)
	if res.Code != 409 {
		t.Fatalf("absolute self cycle accepted: %d %s", res.Code, res.Body.String())
	}
}

func TestRedirectPermissionsAndAPIKey(t *testing.T) {
	s, database, root, _ := newSiteAdminTestServer(t)
	if _, err := database.Exec(`INSERT INTO gocms_site (id,name,code,status) VALUES (2,'Second','second','active');
		INSERT INTO gocms_admin_group (name, permissions, site_permissions) VALUES ('redirect-editor','[]','{"1":["redirects"],"2":["redirects","redirects.manage"]}');
		INSERT INTO gocms_admin_user (username,group_id) SELECT 'redirect-editor',id FROM gocms_admin_group WHERE name='redirect-editor'`); err != nil {
		t.Fatal(err)
	}
	token, err := s.createSession("redirect-editor")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		token, method, endpoint, site, body string
		want                                int
	}{
		{"", "GET", "/api/admin/redirects", "1", "", 401},
		{token, "GET", "/api/admin/redirects", "1", "", 200},
		{token, "POST", "/api/admin/redirects", "1", `{"source_path":"/a","target_url":"/b"}`, 403},
		{token, "POST", "/api/admin/redirects/import", "1", `{"items":[]}`, 403},
		{token, "DELETE", "/api/admin/redirects/1", "1", "", 403},
		{token, "POST", "/api/admin/redirects", "2", `{"source_path":"/a","target_url":"/b"}`, 200},
		{token, "GET", "/api/admin/redirects", "999", "", 403},
	} {
		res := siteAdminRequest(s, tc.token, tc.method, tc.endpoint, tc.site, tc.body)
		if res.Code != tc.want {
			t.Fatalf("permission %s site %s: %d %s", tc.method, tc.site, res.Code, res.Body.String())
		}
	}
	var adminID int64
	if err := database.QueryRow(`SELECT id FROM gocms_admin_user WHERE username='redirect-editor'`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	key, err := apikey.Create(context.Background(), database, adminID, adminID, apikey.CreateInput{Name: "redirect-import"}, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		site string
		want int
	}{{"1", 403}, {"2", 201}} {
		req := httptest.NewRequest("POST", "/api/admin/redirects/import", strings.NewReader(`{"items":[{"source_path":"/old","target_url":"/new"}]}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", key.Key)
		req.Header.Set("X-Site-Id", tc.site)
		res := httptest.NewRecorder()
		s.Handler().ServeHTTP(res, req)
		if res.Code != tc.want {
			t.Fatalf("API key permission site %s: %d %s", tc.site, res.Code, res.Body.String())
		}
	}
	res := siteAdminRequest(s, root, "GET", "/api/admin/logs", "2", "")
	if res.Code != 200 || !strings.Contains(res.Body.String(), "/api/admin/redirects/import") {
		t.Fatalf("redirect mutation missing from audit: %d %s", res.Code, res.Body.String())
	}
}
