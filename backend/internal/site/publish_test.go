package site

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocms/internal/db"
	"gocms/internal/generator"
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
	if _, err := database.Exec(`UPDATE gocms_site SET domain = 'example.com' WHERE id = 1`); err != nil {
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
		INSERT INTO gocms_content (id, category_id, route_key, title, visible, updated_at) VALUES (301, 71, 'entry.html', '指定条目', 1, '2024-03-02T12:34:00Z');
		INSERT INTO gocms_content (id, site_id, category_id, route_key, title, visible) VALUES (302, 2, 71, 'other.html', '其他站点', 1);
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
	var updatedAt string
	if err := database.QueryRow(`SELECT updated_at FROM gocms_content WHERE id = 301`).Scan(&updatedAt); err != nil || updatedAt != "2024-03-02T12:34:00Z" {
		t.Fatalf("generating HTML changed the update time to %q: %v", updatedAt, err)
	}
	web := server.publication.publisher.TargetWeb()
	body, err := os.ReadFile(filepath.Join(web, "content", "entry.html"))
	if err != nil || !strings.Contains(string(body), "detail 指定条目") {
		t.Fatalf("detail: %s %v", body, err)
	}
	for _, detail := range []struct {
		filename, address string
	}{
		{"entry", "/content/entry"},
		{"entry.html", "/content/entry.html"},
		{"archive/entry/index.html", "/content/archive/entry/"},
	} {
		change := categoryRequest(t, server, token, http.MethodPut, "/api/admin/content/301",
			fmt.Sprintf(`{"title":"指定条目","category_id":71,"visible":1,"route_key":%q}`, detail.filename))
		if change.Code != http.StatusOK {
			t.Fatalf("save %s: %d %s", detail.filename, change.Code, change.Body.String())
		}
		publish := categoryRequest(t, server, token, http.MethodPost, "/api/admin/publish/content", `{"id":301}`)
		if publish.Code != http.StatusOK {
			t.Fatalf("publish %s: %d %s", detail.filename, publish.Code, publish.Body.String())
		}
		page := categoryRequest(t, server, token, http.MethodGet, detail.address, "")
		if page.Code != http.StatusOK || !strings.HasPrefix(page.Header().Get("Content-Type"), "text/html") ||
			!strings.Contains(page.Body.String(), "detail 指定条目") {
			t.Fatalf("GET %s: %d %s %s", detail.address, page.Code, page.Header().Get("Content-Type"), page.Body.String())
		}
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

func TestQueuedPublicationProgressAndReviewPermission(t *testing.T) {
	s, database, root := newCategoryTestServer(t)
	s.ConfigurePublishing(t.TempDir(), t.TempDir(), "", "", "")
	if _, err := database.Exec(`INSERT INTO gocms_site (id, name, code) VALUES (2, '第二站点', 'two');
		INSERT INTO gocms_admin_group (name, permissions) VALUES ('reviewers', '["content","content.review"]');
		INSERT INTO gocms_admin_user (username, group_id, site_ids) SELECT 'reviewer', id, '[2]' FROM gocms_admin_group WHERE name = 'reviewers'`); err != nil {
		t.Fatal(err)
	}
	token, err := s.createSession("reviewer")
	if err != nil {
		t.Fatal(err)
	}
	const path = "/api/admin/publish?site_id=2"
	pub := s.publicationForSite(context.Background(), 2)
	pub.mu.Lock()
	current := pub.beginRunLocked(51)
	pub.mu.Unlock()
	current.OnProgress(generator.Progress{Stage: "rendering", Completed: 4})
	s.invalidateSitePublication(2)
	next := s.publicationForSite(context.Background(), 2)
	queued, started := s.startPublishPub(next, 0, true)
	if started || !queued.Queued || queued.Progress == nil || queued.Progress.Completed != 4 {
		t.Fatalf("lost running or queued progress: %+v", queued)
	}
	response := categoryRequest(t, s, token, http.MethodGet, path, "")
	var snapshot generator.Report
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &snapshot) != nil || !snapshot.Queued || snapshot.ContentID != 0 || snapshot.Progress == nil || snapshot.Progress.Completed != 4 {
		t.Fatalf("reviewer status: %d %s", response.Code, response.Body.String())
	}
	for _, request := range []struct{ method, path string }{
		{http.MethodPost, path}, {http.MethodGet, "/api/admin/publish?site_id=1"}, {http.MethodGet, "/api/admin/publish/indexes?site_id=2"},
	} {
		if res := categoryRequest(t, s, token, request.method, request.path, ""); res.Code != http.StatusForbidden {
			t.Fatalf("%s %s: %d", request.method, request.path, res.Code)
		}
	}
	if report := s.publicationReport(s.publication); report.State != "idle" {
		t.Fatalf("progress leaked to another site: %+v", report)
	}
	// The queued build has no templates. It must replace the preceding successful
	// run with its own terminal failure, even after config cache invalidation.
	s.finishPublish(pub, generator.Report{State: "success", Finished: time.Now()}, nil)
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		report := s.publicationReport(next)
		if report.State == "failed" && !report.Queued && report.Error != "" {
			res := categoryRequest(t, s, root, http.MethodGet, path, "")
			if res.Code != 200 || !strings.Contains(res.Body.String(), "index.html") {
				t.Fatalf("publisher failure detail: %s", res.Body.String())
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("queued publication did not finish")
}

func TestSaveAndPublishRefreshesOnlySavedContent(t *testing.T) {
	s, database, token := newCategoryTestServer(t)
	templates := t.TempDir()
	for name, body := range map[string]string{
		"index.html": "Home", "category_list.html": "List", "content_detail.html": "{{.title}}",
	} {
		if err := os.WriteFile(filepath.Join(templates, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	s.ConfigurePublishing(templates, t.TempDir(), "", "", "")
	if _, err := database.Exec(`INSERT INTO gocms_category (id, name, list_path, list_page_size, list_template, detail_template) VALUES
		(71,'编辑栏目','articles',1,'category_list.html','content_detail.html'),
		(72,'无关栏目','other',1,'missing.html','missing.html');
		INSERT INTO gocms_content (id,category_id,route_key,title,visible) VALUES
		(101,71,'first.html','First',1),(102,71,'second.html','Second',1),(103,72,'other.html','Other',1)`); err != nil {
		t.Fatal(err)
	}
	web := s.publication.publisher.TargetWeb()
	if err := os.MkdirAll(web, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web, "sitemap.xml"), []byte("original sitemap"), 0644); err != nil {
		t.Fatal(err)
	}

	save := func(method, path, title string) generator.Report {
		t.Helper()
		res := categoryRequest(t, s, token, method, path+"?publish=1",
			fmt.Sprintf(`{"title":%q,"category_id":71,"visible":1}`, title))
		var result struct{ Publication generator.Report }
		if res.Code != http.StatusOK || json.Unmarshal(res.Body.Bytes(), &result) != nil || result.Publication.State != "running" {
			t.Fatalf("save: %d %s", res.Code, res.Body.String())
		}
		return result.Publication
	}
	wait := func(id int64) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
			r := s.publicationReport(s.publication)
			if r.State != "running" {
				if r.State != "success" || r.ContentID != id || r.Contents != 1 || r.Queued {
					t.Fatalf("unexpected completion: %+v", r)
				}
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("single publication did not finish")
	}

	created := save(http.MethodPost, "/api/admin/content", "Created")
	if created.ContentID <= 103 {
		t.Fatalf("missing inserted ID: %+v", created)
	}
	wait(created.ContentID)
	updated := save(http.MethodPut, "/api/admin/content/101", "Edited")
	if updated.ContentID != 101 {
		t.Fatalf("update chose wrong content: %+v", updated)
	}
	wait(101)

	// Hold a run open while independent HTTP requests commit new content snapshots.
	pub := s.publication
	pub.mu.Lock()
	pub.beginRunLocked(101)
	pub.mu.Unlock()
	for _, change := range []struct{ path, title string }{
		{"/api/admin/content/101", "First queued"}, {"/api/admin/content/102", "Second queued"}, {"/api/admin/content/101", "Latest first"},
	} {
		if report := save(http.MethodPut, change.path, change.title); !report.Queued {
			t.Fatal("save did not queue")
		}
	}
	pub.mu.Lock()
	n := len(pub.pending)
	pub.mu.Unlock()
	if n != 2 {
		t.Fatalf("wanted two distinct pending items, got %d", n)
	}
	s.finishPublish(pub, generator.Report{State: "success", ContentID: 101, Finished: time.Now()}, nil)
	wait(102)
	for name, want := range map[string]string{
		"content/first.html": "Latest first", "content/second.html": "Second queued", "sitemap.xml": "original sitemap",
	} {
		got, err := os.ReadFile(filepath.Join(web, name))
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v, want %q", name, got, err, want)
		}
	}
}

func TestPendingFullPublishCoversContentSaves(t *testing.T) {
	p := &publication{publicationState: &publicationState{report: generator.Report{State: "running"}}}
	next := &publication{publicationState: p.publicationState}
	p.enqueueLocked(14)
	p.enqueueLocked(28)
	p.enqueueLocked(0)
	next.enqueueLocked(42)
	if len(p.pending) != 1 || p.pending[0].contentID != 0 || p.pending[0].target != next {
		t.Fatalf("expected one full publish using current site config, got %+v", p.pending)
	}
}
