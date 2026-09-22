package site

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"gocms/internal/db"
)

func newSiteAdminTestServer(t *testing.T) (*Server, *sql.DB, string, string) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSchema(context.Background(), database); err != nil {
		database.Close()
		t.Fatal(err)
	}
	siteDir := t.TempDir()
	server, err := New(database, siteDir)
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	seedTestAdmin(t, server)
	token, err := server.createSession("gocms")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return server, database, token, siteDir
}

func siteAdminRequest(server *Server, token, method, path, siteHeader, payload string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "gocms_admin", Value: token})
	}
	if siteHeader != "" {
		req.Header.Set("X-Site-Id", siteHeader)
	}
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	return res
}

func TestAdminSitesCRUD(t *testing.T) {
	server, database, token, _ := newSiteAdminTestServer(t)

	// 1. List sites - default site 1 exists
	res := siteAdminRequest(server, token, http.MethodGet, "/api/admin/sites", "", "")
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	var listResp []db.Site
	if err := json.Unmarshal(res.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp) != 1 || listResp[0].ID != 1 {
		t.Fatalf("expected 1 site (id=1), got: %+v", listResp)
	}

	// 2. Create site 2
	newSitePayload := `{"name":"科技资讯","code":"tech","domain":"tech.example.com","aliases":["www.tech.example.com"]}`
	res = siteAdminRequest(server, token, http.MethodPost, "/api/admin/sites", "", newSitePayload)
	if res.Code != http.StatusCreated {
		t.Fatalf("create site: expected 201, got %d: %s", res.Code, res.Body.String())
	}

	// 3. Verify site 2 in database
	site2, err := db.GetSiteByCode(context.Background(), database, "tech")
	if err != nil {
		t.Fatalf("failed to find site tech: %v", err)
	}
	if site2.Domain != "tech.example.com" || len(site2.Aliases) != 1 {
		t.Fatalf("unexpected site2 data: %+v", site2)
	}

	// 4. Update site 2
	updatePayload := fmt.Sprintf(`{"id":%d,"name":"科技头条","code":"tech","domain":"news.tech.com"}`, site2.ID)
	res = siteAdminRequest(server, token, http.MethodPut, fmt.Sprintf("/api/admin/sites/%d", site2.ID), "", updatePayload)
	if res.Code != http.StatusOK {
		t.Fatalf("update site: expected 200, got %d: %s", res.Code, res.Body.String())
	}

	updated, err := db.GetSiteByID(context.Background(), database, site2.ID)
	if err != nil || updated.Name != "科技头条" || updated.Domain != "news.tech.com" {
		t.Fatalf("unexpected updated site: %+v, err: %v", updated, err)
	}

	// 5. Delete default site 1 should be rejected
	res = siteAdminRequest(server, token, http.MethodDelete, "/api/admin/sites/1", "", "")
	if res.Code != http.StatusBadRequest {
		t.Fatalf("delete default site: expected 400, got %d", res.Code)
	}

	// 6. Delete site 2 should succeed
	res = siteAdminRequest(server, token, http.MethodDelete, fmt.Sprintf("/api/admin/sites/%d", site2.ID), "", "")
	if res.Code != http.StatusOK {
		t.Fatalf("delete site 2: expected 200, got %d: %s", res.Code, res.Body.String())
	}
}

func TestMultiSiteCategoryAndContentIsolation(t *testing.T) {
	server, database, token, _ := newSiteAdminTestServer(t)

	// Create site 2
	_, err := db.CreateSite(context.Background(), database, &db.Site{
		Name:   "分站A",
		Code:   "subsite",
		Domain: "sub.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create Category on Site 1 (default)
	cat1Payload := `{"name":"站点1栏目","page_type":"list","list_path":"site1_list","detail_path":"site1_detail"}`
	res := siteAdminRequest(server, token, http.MethodPost, "/api/admin/categories", "1", cat1Payload)
	if res.Code != http.StatusOK {
		t.Fatalf("create cat site 1: expected 200, got %d: %s", res.Code, res.Body.String())
	}

	// Create Category on Site 2
	cat2Payload := `{"name":"站点2栏目","page_type":"list","list_path":"site2_list","detail_path":"site2_detail"}`
	res = siteAdminRequest(server, token, http.MethodPost, "/api/admin/categories", "2", cat2Payload)
	if res.Code != http.StatusOK {
		t.Fatalf("create cat site 2: expected 200, got %d: %s", res.Code, res.Body.String())
	}

	// Query categories for Site 1
	res = siteAdminRequest(server, token, http.MethodGet, "/api/admin/categories", "1", "")
	if res.Code != http.StatusOK {
		t.Fatalf("get cat site 1: expected 200, got %d", res.Code)
	}
	var cats1 []CategoryItem
	_ = json.Unmarshal(res.Body.Bytes(), &cats1)
	if len(cats1) != 1 || cats1[0].Name != "站点1栏目" {
		t.Fatalf("expected only site 1 category, got: %+v", cats1)
	}

	// Query categories for Site 2
	res = siteAdminRequest(server, token, http.MethodGet, "/api/admin/categories", "2", "")
	if res.Code != http.StatusOK {
		t.Fatalf("get cat site 2: expected 200, got %d", res.Code)
	}
	var cats2 []CategoryItem
	_ = json.Unmarshal(res.Body.Bytes(), &cats2)
	if len(cats2) != 1 || cats2[0].Name != "站点2栏目" {
		t.Fatalf("expected only site 2 category, got: %+v", cats2)
	}

	cat1ID := cats1[0].ID
	cat2ID := cats2[0].ID

	// Create Content on Site 1
	res = siteAdminRequest(server, token, http.MethodPost, "/api/admin/content", "1", fmt.Sprintf(`{"category_id":%d,"title":"站点1文章"}`, cat1ID))
	if res.Code != http.StatusOK {
		t.Fatalf("create content site 1: expected 200, got %d: %s", res.Code, res.Body.String())
	}

	// Create Content on Site 2
	res = siteAdminRequest(server, token, http.MethodPost, "/api/admin/content", "2", fmt.Sprintf(`{"category_id":%d,"title":"站点2文章"}`, cat2ID))
	if res.Code != http.StatusOK {
		t.Fatalf("create content site 2: expected 200, got %d: %s", res.Code, res.Body.String())
	}

	// List content for Site 1
	res = siteAdminRequest(server, token, http.MethodGet, "/api/admin/content", "1", "")
	var content1 ContentPage
	_ = json.Unmarshal(res.Body.Bytes(), &content1)
	if content1.Total != 1 || content1.Items[0].Title != "站点1文章" {
		t.Fatalf("expected only site 1 content, got: %+v", content1)
	}

	// List content for Site 2
	res = siteAdminRequest(server, token, http.MethodGet, "/api/admin/content", "2", "")
	var content2 ContentPage
	_ = json.Unmarshal(res.Body.Bytes(), &content2)
	if content2.Total != 1 || content2.Items[0].Title != "站点2文章" {
		t.Fatalf("expected only site 2 content, got: %+v", content2)
	}
}

func TestMultiSiteHostRouting(t *testing.T) {
	server, database, _, siteDir := newSiteAdminTestServer(t)

	// Create Site 2 with domain "blog.example.com"
	_, err := db.CreateSite(context.Background(), database, &db.Site{
		Name:   "博客站点",
		Code:   "blog",
		Domain: "blog.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create static files for Site 1 in independent directory (web/1/index.html)
	site1Dir := filepath.Join(siteDir, "1")
	if err := os.MkdirAll(site1Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site1Dir, "index.html"), []byte("Hello Site 1 Independent"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create static files for Site 2 (web/2/index.html)
	site2Dir := filepath.Join(siteDir, "2")
	if err := os.MkdirAll(site2Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site2Dir, "index.html"), []byte("Hello Site 2 Blog"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Request from default host -> Site 1 from independent directory
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.Host = "localhost:8080"
	rec1 := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK || rec1.Body.String() != "Hello Site 1 Independent" {
		t.Fatalf("expected Site 1 Independent, got %d: %s", rec1.Code, rec1.Body.String())
	}

	// Request with Host "blog.example.com" -> Site 2
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Host = "blog.example.com"
	rec2 := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK || rec2.Body.String() != "Hello Site 2 Blog" {
		t.Fatalf("expected Site 2, got %d: %s", rec2.Code, rec2.Body.String())
	}

	// Request behind reverse proxy with X-Forwarded-Host -> Site 2
	req3 := httptest.NewRequest(http.MethodGet, "/", nil)
	req3.Host = "127.0.0.1:8080"
	req3.Header.Set("X-Forwarded-Host", "blog.example.com")
	rec3 := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK || rec3.Body.String() != "Hello Site 2 Blog" {
		t.Fatalf("expected Site 2 via X-Forwarded-Host, got %d: %s", rec3.Code, rec3.Body.String())
	}
}
