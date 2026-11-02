package site

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocms/internal/db"
	"gocms/internal/generator"
)

func TestAdminPublishIndexesGroupsScopePermissionsAndPublicLinks(t *testing.T) {
	server, database, token := newLanguageTestServer(t)
	ctx := context.Background()
	for _, site := range []db.Site{
		{Name: "任意甲站", Code: "alpha", Domain: "alpha.example.test"},
		{Name: "任意乙站", Code: "beta", Domain: "beta.example.test"},
	} {
		if _, err := db.CreateSite(ctx, database, &site); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec(`INSERT INTO gocms_language (site_id, code, name, path_prefix, site_mode, domain, output_dir) VALUES
		(2, 'en', 'English', 'international', 'subdir', '', ''),
		(2, 'fr', 'Français', '', 'standalone', 'fr.alpha.example.test', 'alpha-french'),
		(3, 'fr', 'Other French', '', 'standalone', 'fr.beta.example.test', 'beta-french');
		INSERT INTO gocms_admin_group (name, permissions, site_ids) VALUES ('publisher', '["publish"]', '[2]'), ('reader', '["content"]', '[2]');
		INSERT INTO gocms_admin_user (username, group_id) SELECT name, id FROM gocms_admin_group WHERE name IN ('publisher', 'reader')`); err != nil {
		t.Fatal(err)
	}
	var frenchID, foreignID int64
	if err := database.QueryRow(`SELECT id FROM gocms_language WHERE site_id = 2 AND code = 'fr'`).Scan(&frenchID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT id FROM gocms_language WHERE site_id = 3 AND code = 'fr'`).Scan(&foreignID); err != nil {
		t.Fatal(err)
	}
	server.ConfigurePublishing("", t.TempDir(), "", "", "")
	for _, name := range []string{"2/index.html", "2/international/article.html", "alpha-french/index.html", "alpha-french/entry.html", "beta-french/index.html"} {
		path := filepath.Join(server.siteRoot, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("<title>Published page</title>"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	publisherToken, err := server.createSession("publisher")
	if err != nil {
		t.Fatal(err)
	}
	readerToken, err := server.createSession("reader")
	if err != nil {
		t.Fatal(err)
	}
	const endpoint = "/api/admin/publish/indexes"
	for _, test := range []struct {
		token, site string
		status      int
	}{
		{"", "2", http.StatusUnauthorized}, {readerToken, "2", http.StatusForbidden},
		{publisherToken, "3", http.StatusForbidden}, {publisherToken, "2", http.StatusOK},
	} {
		res := siteAdminRequest(server, test.token, http.MethodGet, endpoint, test.site, "")
		if res.Code != test.status {
			t.Fatalf("GET site %s: %d want %d: %s", test.site, res.Code, test.status, res.Body.String())
		}
	}
	res := siteAdminRequest(server, token, http.MethodGet, endpoint, "2", "")
	var result struct {
		Targets []generator.IndexTarget `json:"targets"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Targets) != 2 || len(result.Targets[0].Languages) != 2 || strings.Contains(res.Body.String(), server.siteRoot) || strings.Contains(res.Body.String(), "beta.example.test") {
		t.Fatalf("incorrect target metadata or private path leak: %s", res.Body.String())
	}
	for _, test := range []struct {
		body   string
		status int
	}{
		{`{}`, http.StatusBadRequest}, {`{"target":"main","format":"bad"}`, http.StatusBadRequest},
		{fmt.Sprintf(`{"target":"language:%d","format":"all"}`, foreignID), http.StatusNotFound},
		{fmt.Sprintf(`{"target":"language:%d","format":"xml"}`, frenchID), http.StatusOK},
		{`{"target":"all","format":"all"}`, http.StatusOK},
	} {
		res := siteAdminRequest(server, publisherToken, http.MethodPost, endpoint, "2", test.body)
		if res.Code != test.status {
			t.Fatalf("POST %s: got %d want %d: %s", test.body, res.Code, test.status, res.Body.String())
		}
	}
	for _, test := range []struct{ host, wanted, forbidden string }{
		{"alpha.example.test", "international/article.html", "entry.html"},
		{"fr.alpha.example.test", "entry.html", "international/article.html"},
	} {
		for _, filename := range []string{"sitemap.html", "sitemap.xml", "Sitemap.xml", "llms.txt"} {
			req := httptest.NewRequest(http.MethodGet, "http://"+test.host+"/"+filename, nil)
			res := httptest.NewRecorder()
			server.Handler().ServeHTTP(res, req)
			if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), test.wanted) || strings.Contains(res.Body.String(), test.forbidden) {
				t.Fatalf("public %s/%s: %d %s", test.host, filename, res.Code, res.Body.String())
			}
		}
	}
	if _, err := os.Stat(filepath.Join(server.siteRoot, "beta-french", "llms.txt")); !os.IsNotExist(err) {
		t.Fatal("generation crossed the current site boundary")
	}
	if _, err := database.Exec(`UPDATE gocms_language SET domain = '' WHERE id = ?`, frenchID); err != nil {
		t.Fatal(err)
	}
	res = siteAdminRequest(server, publisherToken, http.MethodPost, endpoint, "2", fmt.Sprintf(`{"target":"language:%d","format":"all"}`, frenchID))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("missing domain accepted: %d %s", res.Code, res.Body.String())
	}
}
