package site

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"gocms/internal/db"
)

func newLanguageTestServer(t *testing.T) (*Server, *sql.DB, string) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSchema(context.Background(), database); err != nil {
		database.Close()
		t.Fatal(err)
	}
	server, err := New(database, t.TempDir())
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	token, err := server.createSession("gocms")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return server, database, token
}

func adminRequest(t *testing.T, server *Server, token, method, path, payload string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(payload))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "gocms_admin", Value: token})
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

func TestLanguagesAPI(t *testing.T) {
	server, _, token := newLanguageTestServer(t)

	// 1. Get initial languages (should have zh-CN)
	res := adminRequest(t, server, token, http.MethodGet, "/api/admin/languages", "")
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	var items []LanguageItem
	if err := json.Unmarshal(res.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Code != "zh-CN" || items[0].IsDefault != 1 || items[0].IsFallback != 1 {
		t.Fatalf("unexpected initial languages: %+v", items)
	}

	// 2. Add en language
	newLangPayload := `{"code":"en","name":"English","is_default":false,"is_fallback":false,"is_enabled":true,"sort_order":10,"path_prefix":"en"}`
	res = adminRequest(t, server, token, http.MethodPost, "/api/admin/languages", newLangPayload)
	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201 on create, got %d: %s", res.Code, res.Body.String())
	}
	var createRes map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &createRes); err != nil {
		t.Fatal(err)
	}
	if createRes["ok"] != true {
		t.Fatalf("expected ok=true, got %+v", createRes)
	}

	// 3. List should have 2 languages
	res = adminRequest(t, server, token, http.MethodGet, "/api/admin/languages", "")
	if err := json.Unmarshal(res.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 languages, got %d", len(items))
	}
}

func TestMultiLanguageContentAndCategory(t *testing.T) {
	server, database, token := newLanguageTestServer(t)

	// Create en language
	res := adminRequest(t, server, token, http.MethodPost, "/api/admin/languages", `{"code":"en","name":"English","is_default":false,"is_fallback":false,"is_enabled":true,"sort_order":10,"path_prefix":"en"}`)
	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201 on lang create, got %d: %s", res.Code, res.Body.String())
	}

	// Create Category with translations
	catPayload := `{
		"name": "中文栏目",
		"dir": "news",
		"model_id": 1,
		"translations": {
			"zh-CN": {"name": "中文栏目", "seo_title": "中文SEO"},
			"en": {"name": "English News", "seo_title": "English SEO"}
		}
	}`
	res = adminRequest(t, server, token, http.MethodPost, "/api/admin/categories", catPayload)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 on cat create, got %d: %s", res.Code, res.Body.String())
	}
	var catID int64
	if err := database.QueryRow(`SELECT "id" FROM "gocms_category" WHERE "name" = '中文栏目'`).Scan(&catID); err != nil {
		t.Fatalf("query category id: %v", err)
	}

	// Query categories with lang=en
	res = adminRequest(t, server, token, http.MethodGet, "/api/admin/categories?lang=en", "")
	var catList []CategoryItem
	if err := json.Unmarshal(res.Body.Bytes(), &catList); err != nil {
		t.Fatalf("unmarshal cat list: %v", err)
	}
	if len(catList) != 1 || catList[0].Name != "English News" {
		t.Fatalf("expected English News, got %+v", catList)
	}

	// Query categories with untranslated lang=fr -> fallback to zh-CN
	res = adminRequest(t, server, token, http.MethodGet, "/api/admin/categories?lang=fr", "")
	json.Unmarshal(res.Body.Bytes(), &catList)
	if len(catList) != 1 || catList[0].Name != "中文栏目" || !catList[0].IsFallback {
		t.Fatalf("expected fallback 中文栏目, got %+v", catList)
	}

	// Create Content with translations
	contentPayload := fmt.Sprintf(`{
		"category_id": %d,
		"model_id": 1,
		"title": "默认文章标题",
		"translations": {
			"zh-CN": {"title": "默认文章标题", "content": "<p>内容</p>"},
			"en": {"title": "English Article", "content": "<p>English Content</p>"}
		}
	}`, catID)
	res = adminRequest(t, server, token, http.MethodPost, "/api/admin/content", contentPayload)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 on content create, got %d: %s", res.Code, res.Body.String())
	}

	var contentID int64
	if err := database.QueryRow(`SELECT "id" FROM "gocms_content" WHERE "title" = '默认文章标题'`).Scan(&contentID); err != nil {
		t.Fatalf("query content id: %v", err)
	}

	// Read content list with lang=en
	res = adminRequest(t, server, token, http.MethodGet, fmt.Sprintf("/api/admin/content?category_id=%d&lang=en", catID), "")
	var page ContentPage
	if err := json.Unmarshal(res.Body.Bytes(), &page); err != nil {
		t.Fatalf("unmarshal content page: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Title != "English Article" {
		t.Fatalf("expected English Article, got %+v", page.Items)
	}

	// Read single content item with lang=fr (fallback)
	res = adminRequest(t, server, token, http.MethodGet, fmt.Sprintf("/api/admin/content/%d?lang=fr", contentID), "")
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 on get content item, got %d: %s", res.Code, res.Body.String())
	}
	var singleContent Content
	if err := json.Unmarshal(res.Body.Bytes(), &singleContent); err != nil {
		t.Fatalf("unmarshal content item: %v", err)
	}
	if singleContent.Title != "默认文章标题" || !singleContent.IsFallback {
		t.Fatalf("expected fallback to 默认文章标题, got %+v", singleContent)
	}
}
