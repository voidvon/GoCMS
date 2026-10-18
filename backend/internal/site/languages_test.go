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
	seedTestAdmin(t, server)
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

func TestStandaloneLanguageSiteAPI(t *testing.T) {
	server, _, token := newLanguageTestServer(t)

	// 1. Add standalone language ru
	ruPayload := `{"code":"ru","name":"Русский","is_default":false,"is_fallback":false,"is_enabled":true,"sort_order":40,"path_prefix":"ru","site_mode":"standalone","domain":"ru.example.com","output_dir":"sub_ru"}`
	res := adminRequest(t, server, token, http.MethodPost, "/api/admin/languages", ruPayload)
	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201 on create, got %d: %s", res.Code, res.Body.String())
	}

	// 2. Query languages and verify fields
	res = adminRequest(t, server, token, http.MethodGet, "/api/admin/languages", "")
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	var items []LanguageItem
	if err := json.Unmarshal(res.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	var ruItem *LanguageItem
	for i := range items {
		if items[i].Code == "ru" {
			ruItem = &items[i]
			break
		}
	}
	if ruItem == nil {
		t.Fatal("ru language not found")
	}
	if ruItem.SiteMode != db.LanguageSiteModeStandalone || ruItem.Domain != "ru.example.com" || ruItem.OutputDir != "sub_ru" {
		t.Fatalf("unexpected ru item fields: %+v", ruItem)
	}

	// 3. Update ru language
	updatePayload := fmt.Sprintf(`{"code":"ru","name":"Русский(Updated)","is_default":false,"is_fallback":false,"is_enabled":true,"sort_order":40,"path_prefix":"ru","site_mode":"standalone","domain":"new-ru.example.com","output_dir":"new_ru"}`,)
	res = adminRequest(t, server, token, http.MethodPut, fmt.Sprintf("/api/admin/languages/%d", ruItem.ID), updatePayload)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 on update, got %d: %s", res.Code, res.Body.String())
	}

	// 4. Verify updated fields
	res = adminRequest(t, server, token, http.MethodGet, "/api/admin/languages", "")
	_ = json.Unmarshal(res.Body.Bytes(), &items)
	for i := range items {
		if items[i].Code == "ru" {
			ruItem = &items[i]
			break
		}
	}
	if ruItem.Domain != "new-ru.example.com" || ruItem.OutputDir != "new_ru" {
		t.Fatalf("unexpected updated fields: %+v", ruItem)
	}
}

func TestLanguageAdminDisplayFlag(t *testing.T) {
	server, _, token := newLanguageTestServer(t)

	// 1. The seeded primary site language is also the initial admin display language.
	res := adminRequest(t, server, token, http.MethodGet, "/api/admin/languages", "")
	var items []LanguageItem
	if err := json.Unmarshal(res.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Code != "zh-CN" || items[0].IsAdminDefault != 1 {
		t.Fatalf("expected seeded admin display language zh-CN, got %+v", items)
	}

	// 2. A new secondary language is not the admin display language by default.
	res = adminRequest(t, server, token, http.MethodPost, "/api/admin/languages", `{"code":"en","name":"English","is_default":false,"is_fallback":false,"is_enabled":true,"sort_order":20,"path_prefix":"en"}`)
	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201 on create, got %d: %s", res.Code, res.Body.String())
	}
	var enID int64
	for i := range items {
		if items[i].Code == "en" {
			enID = items[i].ID
		}
	}
	res = adminRequest(t, server, token, http.MethodGet, "/api/admin/languages", "")
	_ = json.Unmarshal(res.Body.Bytes(), &items)
	for _, item := range items {
		if item.Code == "en" {
			enID = item.ID
			if item.IsAdminDefault != 0 {
				t.Fatalf("expected new language not to be the admin display language: %+v", item)
			}
		}
	}
	if enID == 0 {
		t.Fatal("expected created language id")
	}

	// 3. Marking another language keeps the primary site language untouched and
	//    clears the previous admin display language. A disabled admin display
	//    language is re-enabled.
	updatePayload := fmt.Sprintf(`{"code":"en","name":"English","is_default":false,"is_fallback":false,"is_enabled":false,"sort_order":20,"path_prefix":"en","is_admin_default":true}`)
	res = adminRequest(t, server, token, http.MethodPut, fmt.Sprintf("/api/admin/languages/%d", enID), updatePayload)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 on update, got %d: %s", res.Code, res.Body.String())
	}
	res = adminRequest(t, server, token, http.MethodGet, "/api/admin/languages", "")
	_ = json.Unmarshal(res.Body.Bytes(), &items)
	for _, item := range items {
		switch item.Code {
		case "en":
			if item.IsAdminDefault != 1 || item.IsEnabled != 1 {
				t.Fatalf("expected en to be the enabled admin display language: %+v", item)
			}
		case "zh-CN":
			if item.IsDefault != 1 {
				t.Fatalf("expected zh-CN to stay the primary site language: %+v", item)
			}
			if item.IsAdminDefault != 0 {
				t.Fatalf("expected zh-CN to no longer be the admin display language: %+v", item)
			}
		}
	}

	// 4. The admin display language cannot be deleted.
	res = adminRequest(t, server, token, http.MethodDelete, fmt.Sprintf("/api/admin/languages/%d", enID), "")
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when deleting the admin display language, got %d: %s", res.Code, res.Body.String())
	}

	// 5. Clearing the flag falls back to the primary site language.
	res = adminRequest(t, server, token, http.MethodPut, fmt.Sprintf("/api/admin/languages/%d", enID), `{"code":"en","name":"English","is_default":false,"is_fallback":false,"is_enabled":true,"sort_order":20,"path_prefix":"en"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 on update, got %d: %s", res.Code, res.Body.String())
	}
	res = adminRequest(t, server, token, http.MethodGet, "/api/admin/languages", "")
	_ = json.Unmarshal(res.Body.Bytes(), &items)
	var adminDefaultCodes []string
	for _, item := range items {
		if item.IsAdminDefault == 1 {
			adminDefaultCodes = append(adminDefaultCodes, item.Code)
		}
	}
	if len(adminDefaultCodes) != 1 || adminDefaultCodes[0] != "zh-CN" {
		t.Fatalf("expected zh-CN to take over the admin display language, got %v", adminDefaultCodes)
	}
}
