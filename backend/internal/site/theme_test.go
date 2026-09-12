package site

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocms/internal/db"
	"gocms/internal/templateconfig"
	themepkg "gocms/internal/theme"
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
	seedTestAdmin(t, server)
	token, err := server.createSession("gocms")
	if err != nil {
		t.Fatal(err)
	}

	request := func(rawURL string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, rawURL, nil)
		req.AddCookie(&http.Cookie{Name: "gocms_admin", Value: token})
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
	if listed.TemplateGroups[0].Key != "home" || listed.TemplateGroups[0].Files[0].Path != "index.html" || listed.TemplateGroups[1].Key != "public" || listed.TemplateGroups[1].Files[0].Path != "z-entry.html" {
		t.Fatalf("unexpected home template group: %+v", listed.TemplateGroups[0])
	}
	if len(listed.TemplateGroups[0].Assignments) != 0 {
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

	updateRequest := httptest.NewRequest(http.MethodPut, "/api/admin/theme/assignments/search", strings.NewReader(`{"template_path":"z-entry.html"}`))
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRequest.AddCookie(&http.Cookie{Name: "gocms_admin", Value: token})
	updateResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(updateResponse, updateRequest)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("assignment update status = %d, body = %s", updateResponse.Code, updateResponse.Body.String())
	}

	homeIndexUpdate := httptest.NewRequest(http.MethodPut, "/api/admin/theme/assignments/home_index", strings.NewReader(`{"template_path":"index.html"}`))
	homeIndexUpdate.Header.Set("Content-Type", "application/json")
	homeIndexUpdate.AddCookie(&http.Cookie{Name: "gocms_admin", Value: token})
	homeIndexResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(homeIndexResponse, homeIndexUpdate)
	if homeIndexResponse.Code != http.StatusBadRequest {
		t.Fatalf("home_index assignment update status = %d, body = %s", homeIndexResponse.Code, homeIndexResponse.Body.String())
	}

	categoryUpdate := httptest.NewRequest(http.MethodPut, "/api/admin/theme/assignments/category_detail", strings.NewReader(`{"template_path":"z-entry.html"}`))
	categoryUpdate.Header.Set("Content-Type", "application/json")
	categoryUpdate.AddCookie(&http.Cookie{Name: "gocms_admin", Value: token})
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

func TestAdminTemplateCustomFiles(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	server, err := New(database, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server.themeRoot = t.TempDir()
	seedTestAdmin(t, server)
	token, err := server.createSession("gocms")
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, rawURL string, body io.Reader, contentType string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		req := httptest.NewRequest(method, rawURL, body)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		req.AddCookie(&http.Cookie{Name: "gocms_admin", Value: token})
		server.Handler().ServeHTTP(response, req)
		return response
	}

	cssResponse := request(http.MethodPut, "/api/admin/templates/files", strings.NewReader(`{"kind":"css","path":"site.css","content":"body { color: red; }"}`), "application/json")
	if cssResponse.Code != http.StatusOK {
		t.Fatalf("save css status = %d: %s", cssResponse.Code, cssResponse.Body.String())
	}
	var saved struct {
		File ThemeFile `json:"file"`
	}
	if err := json.Unmarshal(cssResponse.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.File.Path != "css/site.css" {
		t.Fatalf("saved css path = %q", saved.File.Path)
	}

	jsResponse := request(http.MethodPut, "/api/admin/templates/files", strings.NewReader(`{"kind":"js","path":"js/site.js","content":"console.log('ok')"}`), "application/json")
	if jsResponse.Code != http.StatusOK {
		t.Fatalf("save js status = %d: %s", jsResponse.Code, jsResponse.Body.String())
	}

	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	multipartWriter := multipart.NewWriter(&body)
	if err := multipartWriter.WriteField("kind", "image"); err != nil {
		t.Fatal(err)
	}
	part, err := multipartWriter.CreateFormFile("file", "cover.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(imageData.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := multipartWriter.Close(); err != nil {
		t.Fatal(err)
	}
	imageResponse := request(http.MethodPost, "/api/admin/templates/files", &body, multipartWriter.FormDataContentType())
	if imageResponse.Code != http.StatusCreated {
		t.Fatalf("upload image status = %d: %s", imageResponse.Code, imageResponse.Body.String())
	}
	if !strings.Contains(imageResponse.Body.String(), "images/cover.png") {
		t.Fatalf("uploaded image response = %s", imageResponse.Body.String())
	}

	listResponse := request(http.MethodGet, "/api/admin/templates", nil, "")
	if listResponse.Code != http.StatusOK {
		t.Fatalf("custom file list status = %d: %s", listResponse.Code, listResponse.Body.String())
	}
	var listed ThemeFilesResponse
	if err := json.Unmarshal(listResponse.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.JSFiles) != 1 || len(listed.ImageFiles) != 1 || len(listed.CustomFiles.CSS) != 1 {
		t.Fatalf("custom file list = %+v", listed)
	}

	imagePath := url.Values{"kind": {"image"}, "path": {"images/cover.png"}}.Encode()
	imageMetadata := request(http.MethodGet, "/api/admin/templates?"+imagePath, nil, "")
	if imageMetadata.Code != http.StatusOK || !strings.Contains(imageMetadata.Body.String(), `"content":""`) {
		t.Fatalf("image metadata = %d: %s", imageMetadata.Code, imageMetadata.Body.String())
	}
	publicImage := request(http.MethodGet, "/images/cover.png", nil, "")
	if publicImage.Code != http.StatusOK || publicImage.Body.Len() == 0 {
		t.Fatalf("public image = %d, %d bytes", publicImage.Code, publicImage.Body.Len())
	}

	traversal := request(http.MethodPut, "/api/admin/templates/files", strings.NewReader(`{"kind":"css","path":"../outside.css","content":"nope"}`), "application/json")
	if traversal.Code != http.StatusBadRequest {
		t.Fatalf("custom file traversal status = %d: %s", traversal.Code, traversal.Body.String())
	}
	deletePath := url.Values{"kind": {"image"}, "path": {"images/cover.png"}}.Encode()
	if response := request(http.MethodDelete, "/api/admin/templates/files?"+deletePath, nil, ""); response.Code != http.StatusOK {
		t.Fatalf("delete image status = %d: %s", response.Code, response.Body.String())
	}
}

func TestThemeCustomImageValidation(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	if err := validateThemeImage(data.Bytes(), ".jpg"); err == nil {
		t.Fatal("png content accepted as jpg")
	}
	if err := validateThemeImage(data.Bytes(), ".png"); err != nil {
		t.Fatalf("png content rejected: %v", err)
	}
}

func TestAdminThemeCatalogActions(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	server, err := New(database, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	themesRoot := filepath.Join(t.TempDir(), "themes")
	first := writeTestTheme(t, themesRoot, "first", "First")
	second := writeTestTheme(t, themesRoot, "second", "Second")
	dataRoot := filepath.Join(t.TempDir(), "data")
	server.ConfigureThemeCatalog(themesRoot, dataRoot, first)
	seedTestAdmin(t, server)
	token, err := server.createSession("gocms")
	if err != nil {
		t.Fatal(err)
	}

	request := func(method, rawURL string, body io.Reader, contentType string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		req := httptest.NewRequest(method, rawURL, body)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		req.AddCookie(&http.Cookie{Name: "gocms_admin", Value: token})
		server.Handler().ServeHTTP(response, req)
		return response
	}

	listResponse := request(http.MethodGet, "/api/admin/theme", nil, "")
	if listResponse.Code != http.StatusOK {
		t.Fatalf("theme list status = %d: %s", listResponse.Code, listResponse.Body.String())
	}
	var listed ThemeFilesResponse
	if err := json.Unmarshal(listResponse.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.ActiveTheme != "first" || len(listed.Themes) != 2 || !listed.Themes[0].Active {
		t.Fatalf("unexpected theme catalog: %+v", listed)
	}

	activation := bytes.NewBufferString(`{"id":"second"}`)
	activationResponse := request(http.MethodPost, "/api/admin/theme/activate", activation, "application/json")
	if activationResponse.Code != http.StatusOK {
		t.Fatalf("theme activation status = %d: %s", activationResponse.Code, activationResponse.Body.String())
	}
	if active, err := themepkg.LoadActive(dataRoot); err != nil || active != "second" {
		t.Fatalf("active theme = %q, err = %v", active, err)
	}
	assetsRoot, templatesRoot := server.themePaths()
	if assetsRoot != second.AssetsRoot || templatesRoot != second.TemplatesRoot {
		t.Fatalf("active roots = %q, %q", assetsRoot, templatesRoot)
	}

	exportResponse := request(http.MethodGet, "/api/admin/theme/export?id=second", nil, "")
	if exportResponse.Code != http.StatusOK || exportResponse.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("theme export = %d %q", exportResponse.Code, exportResponse.Header().Get("Content-Type"))
	}

	thirdRoot := filepath.Join(t.TempDir(), "third")
	third := writeTestTheme(t, filepath.Dir(thirdRoot), filepath.Base(thirdRoot), "Third")
	var archive bytes.Buffer
	if err := themepkg.WriteArchive(&archive, third); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	multipartWriter := multipart.NewWriter(&body)
	part, err := multipartWriter.CreateFormFile("theme", "third.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(archive.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := multipartWriter.Close(); err != nil {
		t.Fatal(err)
	}
	importResponse := request(http.MethodPost, "/api/admin/theme/import", &body, multipartWriter.FormDataContentType())
	if importResponse.Code != http.StatusCreated {
		t.Fatalf("theme import status = %d: %s", importResponse.Code, importResponse.Body.String())
	}
	listResponse = request(http.MethodGet, "/api/admin/theme", nil, "")
	if err := json.Unmarshal(listResponse.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Themes) != 3 || listed.ActiveTheme != "second" {
		t.Fatalf("catalog after import = %+v", listed)
	}
}

func TestThemeValidationRequiresCoverTemplate(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.CreateSchema(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO "gocms_category"
		("id", "name", "parent_id", "route_id", "list_path", "list_file_pattern", "list_template", "page_type", "cover_template", "detail_path", "detail_file_pattern", "detail_template")
		VALUES (1, '封面栏目', 0, 1, 'landing', 'page.html', 'category_list.html', 'cover', 'cover.html', 'content', '{id}.html', 'content_detail.html')`); err != nil {
		t.Fatal(err)
	}

	themesRoot := filepath.Join(t.TempDir(), "themes")
	definition := writeTestTheme(t, themesRoot, "cover-check", "Cover Check")
	for _, assignment := range templateconfig.Defaults() {
		path := filepath.Join(definition.TemplatesRoot, assignment.TemplatePath)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("<main>"+assignment.Key+"</main>"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(definition.TemplatesRoot, "content_detail.html"), []byte("<main>detail</main>"), 0644); err != nil {
		t.Fatal(err)
	}
	server, err := New(database, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := server.validateThemeTemplates(definition); err == nil || !strings.Contains(err.Error(), "cover.html") {
		t.Fatalf("missing cover template was accepted: %v", err)
	}
	coverPath := filepath.Join(definition.TemplatesRoot, "cover.html")
	if err := os.WriteFile(coverPath, []byte("<main>cover</main>"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := server.validateThemeTemplates(definition); err != nil {
		t.Fatalf("cover template validation failed: %v", err)
	}
}

func writeTestTheme(t *testing.T, themesRoot, id, name string) themepkg.Definition {
	t.Helper()
	root := filepath.Join(themesRoot, id)
	for path, content := range map[string]string{
		filepath.Join(root, "theme.json"):                `{"id":"` + id + `","name":"` + name + `"}`,
		filepath.Join(root, "templates", "index.html"):   "<main>" + id + "</main>",
		filepath.Join(root, "assets", "css", "site.css"): "body { color: red; }",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	definition, err := themepkg.Find(themesRoot, id)
	if err != nil {
		t.Fatal(err)
	}
	return definition
}
