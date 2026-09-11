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
