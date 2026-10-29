package site

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"gocms/internal/db"
)

func TestAdminSitemapGeneration(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.CreateSchema(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO "gocms_site_setting" ("key", "value") VALUES ('site_url', 'https://example.com/')`); err != nil {
		t.Fatal(err)
	}
	server, err := New(database, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	web := server.siteRoot
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("home"), 0644); err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	server.ConfigurePublishing("", data, "", "", "")
	seedTestAdmin(t, server)
	token, err := server.createSession("gocms")
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/admin/publish/sitemap", bytes.NewBufferString(`{"format":"xml"}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "gocms_admin", Value: token})
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("sitemap status = %d: %s", response.Code, response.Body.String())
	}
	var result struct {
		OK     bool   `json:"ok"`
		Format string `json:"format"`
		Path   string `json:"path"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Format != "xml" || result.Path != "/Sitemap.xml" {
		t.Fatalf("unexpected sitemap response: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(web, "Sitemap.xml")); err != nil {
		t.Fatal(err)
	}
}

func TestAdminLLMSGeneration(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.CreateSchema(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	server, err := New(database, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server.ConfigurePublishing("", t.TempDir(), "", "", "")
	if err := os.WriteFile(filepath.Join(server.siteRoot, "index.html"), []byte("<title>Public site</title>"), 0644); err != nil {
		t.Fatal(err)
	}
	seedTestAdmin(t, server)
	token, err := server.createSession("gocms")
	if err != nil {
		t.Fatal(err)
	}
	for _, authenticated := range []bool{false, true} {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/publish/llms", nil)
		if authenticated {
			req.AddCookie(&http.Cookie{Name: "gocms_admin", Value: token})
		}
		res := httptest.NewRecorder()
		server.Handler().ServeHTTP(res, req)
		if !authenticated {
			if res.Code != http.StatusUnauthorized {
				t.Fatalf("unauthenticated: %d", res.Code)
			}
			continue
		}
		if res.Code != http.StatusOK {
			t.Fatalf("generate: %d %s", res.Code, res.Body.String())
		}
	}
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/llms.txt", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("public guide: %d", res.Code)
	}
}

func TestAdminPublishContent(t *testing.T) {
	server, database, token := newCategoryTestServer(t)
	templates := t.TempDir()
	for name, body := range map[string]string{
		"index.html": "home", "category_list.html": "list {{.category_name}}", "content_detail.html": "detail {{.title}}",
	} {
		if err := os.WriteFile(filepath.Join(templates, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	server.ConfigurePublishing(templates, t.TempDir(), "", "", "")
	if _, err := database.Exec(`INSERT INTO gocms_category (id, name, list_path, list_file_pattern, list_template, detail_template) VALUES
		(71, '任意栏目', 'custom', 'index.html', 'category_list.html', 'content_detail.html');
		INSERT INTO gocms_content (id, category_id, route_key, title, visible) VALUES (301, 71, 'entry', '指定条目', 1);
		INSERT INTO gocms_content (id, site_id, category_id, route_key, title, visible) VALUES (302, 2, 71, 'other', '其他站点', 1);
		INSERT INTO gocms_admin_group (name, permissions) VALUES ('editor', '["content","content.edit"]'), ('publisher', '["content","publish"]');
		INSERT INTO gocms_admin_user (username, group_id) SELECT 'editor', id FROM gocms_admin_group WHERE name = 'editor';
		INSERT INTO gocms_admin_user (username, group_id, category_ids) SELECT 'publisher', id, '[99]' FROM gocms_admin_group WHERE name = 'publisher'`); err != nil {
		t.Fatal(err)
	}
	editorToken, err := server.createSession("editor")
	if err != nil {
		t.Fatal(err)
	}
	scopedToken, err := server.createSession("publisher")
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		name, token, method, body string
		status                    int
	}{
		{"authentication", "", http.MethodPost, `{"id":301}`, http.StatusUnauthorized},
		{"permission", editorToken, http.MethodPost, `{"id":301}`, http.StatusForbidden},
		{"category scope", scopedToken, http.MethodPost, `{"id":301}`, http.StatusNotFound},
		{"method", token, http.MethodGet, "", http.StatusMethodNotAllowed},
		{"invalid ID", token, http.MethodPost, `{"id":0}`, http.StatusBadRequest},
		{"invalid body", token, http.MethodPost, `{`, http.StatusBadRequest},
		{"missing content", token, http.MethodPost, `{"id":999}`, http.StatusNotFound},
		{"site scope", token, http.MethodPost, `{"id":302}`, http.StatusNotFound},
		{"success", token, http.MethodPost, `{"id":301}`, http.StatusOK},
	} {
		t.Run(check.name, func(t *testing.T) {
			res := categoryRequest(t, server, check.token, check.method, "/api/admin/publish/content", check.body)
			if res.Code != check.status {
				t.Fatalf("status %d, want %d: %s", res.Code, check.status, res.Body.String())
			}
			if check.status == http.StatusOK {
				var report struct {
					State    string `json:"state"`
					Contents int    `json:"contents"`
				}
				if err := json.Unmarshal(res.Body.Bytes(), &report); err != nil || report.State != "success" || report.Contents != 1 {
					t.Fatalf("report: %+v %v", report, err)
				}
			}
		})
	}
	web := server.publication.publisher.TargetWeb()
	body, err := os.ReadFile(filepath.Join(web, "content", "entry.html"))
	if err != nil || string(body) != "detail 指定条目" {
		t.Fatalf("detail: %s %v", body, err)
	}
	server.publication.mu.Lock()
	server.publication.report.State = "running"
	server.publication.mu.Unlock()
	res := categoryRequest(t, server, token, http.MethodPost, "/api/admin/publish/content", `{"id":301}`)
	if res.Code != http.StatusConflict {
		t.Fatalf("busy status: %d %s", res.Code, res.Body.String())
	}
	server.publication.mu.Lock()
	server.publication.report.State = "success"
	server.publication.mu.Unlock()
	if err := os.WriteFile(filepath.Join(templates, "content_detail.html"), []byte(`{{include "missing.html" .}}`), 0644); err != nil {
		t.Fatal(err)
	}
	res = categoryRequest(t, server, token, http.MethodPost, "/api/admin/publish/content", `{"id":301}`)
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("failure status: %d %s", res.Code, res.Body.String())
	}
	if report := server.publicationReport(server.publication); report.State != "failed" || report.Error == "" {
		t.Fatalf("failure report: %+v", report)
	}
}
