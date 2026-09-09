package site

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bilvie/internal/db"
)

func TestAdminThemeFiles(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	server, err := New(database, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	themeRoot := t.TempDir()
	templateRoot := t.TempDir()
	for path, content := range map[string]string{
		filepath.Join(themeRoot, "css", "site.css"):     "body { color: red; }",
		filepath.Join(themeRoot, "skin", "nested.css"):  ".nested { display: block; }",
		filepath.Join(themeRoot, "secret.txt"):          "private",
		filepath.Join(templateRoot, "index.html"):       "<!doctype html><html></html>",
		filepath.Join(templateRoot, "z-entry.html"):     "<main>entry</main>",
		filepath.Join(templateRoot, "not-template.txt"): "private",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	server.themeRoot = themeRoot
	server.templateRoot = templateRoot
	token, err := server.createSession("bilvie")
	if err != nil {
		t.Fatal(err)
	}

	request := func(rawURL string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, rawURL, nil)
		req.AddCookie(&http.Cookie{Name: "bilvie_admin", Value: token})
		server.Handler().ServeHTTP(response, req)
		return response
	}

	listResponse := request("/api/admin/theme")
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listResponse.Code, listResponse.Body.String())
	}
	var listed ThemeFilesResponse
	if err := json.Unmarshal(listResponse.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Name != filepath.Base(themeRoot) || len(listed.CSSFiles) != 2 || len(listed.TemplateFiles) != 2 || len(listed.TemplateGroups) != 2 {
		t.Fatalf("unexpected theme file list: %+v", listed)
	}
	if listed.CSSFiles[0].Path != "css/site.css" || listed.TemplateFiles[0].Path != "index.html" {
		t.Fatalf("theme files are not sorted: %+v", listed)
	}
	if listed.TemplateGroups[0].Key != "home" || listed.TemplateGroups[0].Files[0].Path != "index.html" || listed.TemplateGroups[1].Key != "other" || listed.TemplateGroups[1].Files[0].Path != "z-entry.html" {
		t.Fatalf("unexpected home template group: %+v", listed.TemplateGroups[0])
	}
	if len(listed.TemplateGroups[0].Assignments) != 1 || listed.TemplateGroups[0].Assignments[0].TemplatePath != "index.html" {
		t.Fatalf("unexpected home assignment: %+v", listed.TemplateGroups[0])
	}

	cssPath := url.Values{"kind": {"css"}, "path": {"css/site.css"}}.Encode()
	cssResponse := request("/api/admin/theme?" + cssPath)
	if cssResponse.Code != http.StatusOK || cssResponse.Body.String() == "" {
		t.Fatalf("css response = %d %s", cssResponse.Code, cssResponse.Body.String())
	}
	var css ThemeFileContent
	if err := json.Unmarshal(cssResponse.Body.Bytes(), &css); err != nil {
		t.Fatal(err)
	}
	if css.Kind != "css" || css.Path != "css/site.css" || css.Content != "body { color: red; }" {
		t.Fatalf("unexpected css content: %+v", css)
	}

	updateRequest := httptest.NewRequest(http.MethodPut, "/api/admin/theme/assignments/home_index", strings.NewReader(`{"template_path":"index.html"}`))
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRequest.AddCookie(&http.Cookie{Name: "bilvie_admin", Value: token})
	updateResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(updateResponse, updateRequest)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("assignment update status = %d, body = %s", updateResponse.Code, updateResponse.Body.String())
	}
	categoryUpdate := httptest.NewRequest(http.MethodPut, "/api/admin/theme/assignments/category_detail", strings.NewReader(`{"template_path":"z-entry.html"}`))
	categoryUpdate.Header.Set("Content-Type", "application/json")
	categoryUpdate.AddCookie(&http.Cookie{Name: "bilvie_admin", Value: token})
	categoryResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(categoryResponse, categoryUpdate)
	if categoryResponse.Code != http.StatusBadRequest {
		t.Fatalf("category assignment update status = %d, body = %s", categoryResponse.Code, categoryResponse.Body.String())
	}

	htmlPath := url.Values{"kind": {"template"}, "path": {"z-entry.html"}}.Encode()
	htmlResponse := request("/api/admin/theme?" + htmlPath)
	if htmlResponse.Code != http.StatusOK {
		t.Fatalf("html status = %d, body = %s", htmlResponse.Code, htmlResponse.Body.String())
	}
	var html ThemeFileContent
	if err := json.Unmarshal(htmlResponse.Body.Bytes(), &html); err != nil {
		t.Fatal(err)
	}
	if html.Kind != "template" || html.Content != "<main>entry</main>" {
		t.Fatalf("unexpected html content: %+v", html)
	}

	traversalPath := url.Values{"kind": {"css"}, "path": {"../secret.txt"}}.Encode()
	if response := request("/api/admin/theme?" + traversalPath); response.Code != http.StatusBadRequest {
		t.Fatalf("path traversal status = %d, body = %s", response.Code, response.Body.String())
	}
}
