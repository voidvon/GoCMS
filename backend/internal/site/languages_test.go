package site

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"gocms/internal/db"
)

func TestLanguagesAPIIsolatesSitesAndRollsBackFailedChanges(t *testing.T) {
	server, database, token := newLanguageTestServer(t)
	ctx := context.Background()
	for _, code := range []string{"alpha", "beta"} {
		if _, err := db.CreateSite(ctx, database, &db.Site{Name: code, Code: code}); err != nil {
			t.Fatal(err)
		}
	}
	list := func(siteID string) []LanguageItem {
		t.Helper()
		res := siteAdminRequest(server, token, http.MethodGet, "/api/admin/languages", siteID, "")
		var items []LanguageItem
		if res.Code != http.StatusOK {
			t.Fatalf("list site %s: %d %s", siteID, res.Code, res.Body.String())
		}
		if err := json.Unmarshal(res.Body.Bytes(), &items); err != nil {
			t.Fatal(err)
		}
		return items
	}
	initial2, initial3 := list("2"), list("3")
	if len(initial2) != 1 || len(initial3) != 1 || initial2[0].ID == initial3[0].ID {
		t.Fatalf("initial languages share records: %+v %+v", initial2, initial3)
	}
	create := func(siteID, payload string) int64 {
		t.Helper()
		res := siteAdminRequest(server, token, http.MethodPost, "/api/admin/languages", siteID, payload)
		if res.Code != http.StatusCreated {
			t.Fatalf("create site %s: %d %s", siteID, res.Code, res.Body.String())
		}
		var created struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &created); err != nil {
			t.Fatal(err)
		}
		return created.ID
	}
	en2 := create("2", `{"code":"en","name":"English alpha","is_default":true,"is_fallback":true,"is_admin_default":true,"is_enabled":true}`)
	if got := list("3"); !reflect.DeepEqual(initial3, got) {
		t.Fatalf("site 2 creation changed site 3: %+v", got)
	}
	en3 := create("3", `{"code":"en","name":"English beta","is_enabled":true,"path_prefix":"english-beta"}`)
	if en2 == en3 {
		t.Fatal("same language code shares an ID across sites")
	}
	before2, before3 := list("2"), list("3")
	defaultsPayload := `{"code":"en","name":"must not apply","is_default":true,"is_fallback":true,"is_admin_default":true,"is_enabled":true}`
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		res := siteAdminRequest(server, token, method, fmt.Sprintf("/api/admin/languages/%d", en2), "3", defaultsPayload)
		if res.Code != http.StatusNotFound {
			t.Fatalf("cross-site %s returned %d: %s", method, res.Code, res.Body.String())
		}
	}
	for _, change := range []struct{ method, path, payload string }{
		{http.MethodPost, "/api/admin/languages", defaultsPayload},
		{http.MethodPut, fmt.Sprintf("/api/admin/languages/%d", initial3[0].ID), defaultsPayload},
	} {
		res := siteAdminRequest(server, token, change.method, change.path, "3", change.payload)
		if res.Code != http.StatusBadRequest {
			t.Fatalf("duplicate code returned %d: %s", res.Code, res.Body.String())
		}
	}
	if !reflect.DeepEqual(before2, list("2")) || !reflect.DeepEqual(before3, list("3")) {
		t.Fatal("failed changes mutated language rows or default flags")
	}
	default2, fallback2 := server.getDefaultAndFallbackLang(ctx, 2)
	default3, fallback3 := server.getDefaultAndFallbackLang(ctx, 3)
	if default2 != "en" || fallback2 != "en" || default3 != "zh-CN" || fallback3 != "zh-CN" {
		t.Fatalf("defaults shared across sites: %s/%s %s/%s", default2, fallback2, default3, fallback3)
	}
	res := siteAdminRequest(server, token, http.MethodPut, fmt.Sprintf("/api/admin/languages/%d", en3), "3", `{"code":"en","name":"English beta updated","is_admin_default":true,"is_enabled":true}`)
	if res.Code != http.StatusOK || !reflect.DeepEqual(before2, list("2")) {
		t.Fatalf("updating site 3 affected site 2: %d %s", res.Code, res.Body.String())
	}
	// Return the admin default to Chinese, then delete only site 3's English.
	res = siteAdminRequest(server, token, http.MethodPut, fmt.Sprintf("/api/admin/languages/%d", initial3[0].ID), "3", `{"code":"zh-CN","name":"简体中文","is_default":true,"is_fallback":true,"is_admin_default":true,"is_enabled":true}`)
	if res.Code != http.StatusOK {
		t.Fatalf("reset site 3: %d %s", res.Code, res.Body.String())
	}
	res = siteAdminRequest(server, token, http.MethodDelete, fmt.Sprintf("/api/admin/languages/%d", en3), "3", "")
	if res.Code != http.StatusOK || len(list("3")) != 1 || !reflect.DeepEqual(before2, list("2")) {
		t.Fatalf("delete was not isolated: %d %s", res.Code, res.Body.String())
	}
	res = siteAdminRequest(server, token, http.MethodGet, "/api/admin/languages", "99999", "")
	if res.Code != http.StatusForbidden {
		t.Fatalf("unknown site accepted: %d", res.Code)
	}
	if _, err := database.Exec(`INSERT INTO gocms_admin_group (name, permissions, site_ids) VALUES ('language editor', '["languages"]', '[3]');
		INSERT INTO gocms_admin_user (username, group_id) SELECT 'language-editor', id FROM gocms_admin_group WHERE name = 'language editor'`); err != nil {
		t.Fatal(err)
	}
	editorToken, err := server.createSession("language-editor")
	if err != nil {
		t.Fatal(err)
	}
	for _, siteID := range []string{"2", "3"} {
		want := http.StatusForbidden
		if siteID == "3" {
			want = http.StatusOK
		}
		res := siteAdminRequest(server, editorToken, http.MethodGet, "/api/admin/languages", siteID, "")
		if res.Code != want {
			t.Fatalf("restricted editor reading site %s: got %d want %d", siteID, res.Code, want)
		}
	}
}

func TestLanguageDefaultsApplyToOwningSiteContentAndCategories(t *testing.T) {
	server, database, token := newLanguageTestServer(t)
	ctx := context.Background()
	if _, err := db.CreateSite(ctx, database, &db.Site{Name: "任意独立站点", Code: "independent"}); err != nil {
		t.Fatal(err)
	}
	res := siteAdminRequest(server, token, http.MethodPost, "/api/admin/languages", "2", `{"code":"fr","name":"Français","is_default":true,"is_fallback":true,"is_admin_default":true,"is_enabled":true}`)
	if res.Code != http.StatusCreated {
		t.Fatalf("create language: %d %s", res.Code, res.Body.String())
	}
	res = siteAdminRequest(server, token, http.MethodPost, "/api/admin/categories", "2", `{"translations":{"fr":{"name":"Rubrique"}},"page_type":"list","list_path":"arbitrary-sections","detail_path":"arbitrary-entries"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("category failed to use site's primary language: %d %s", res.Code, res.Body.String())
	}
	var category struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &category); err != nil {
		t.Fatal(err)
	}
	res = siteAdminRequest(server, token, http.MethodPost, "/api/admin/content", "2", fmt.Sprintf(`{"category_id":%d,"visible":1,"translations":{"fr":{"title":"Titre français"},"zh-CN":{"title":"中文译文"}}}`, category.ID))
	if res.Code != http.StatusOK {
		t.Fatalf("content failed to use site's primary language: %d %s", res.Code, res.Body.String())
	}
	var contentID int64
	if err := database.QueryRow(`SELECT id FROM gocms_content WHERE site_id = 2`).Scan(&contentID); err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"", "missing-translation"} {
		page, err := server.queryAdminContent(ctx, 2, "", 0, false, lang, 1, 10)
		if err != nil || len(page.Items) != 1 || page.Items[0].Title != "Titre français" {
			t.Fatalf("admin content lang %q: %+v %v", lang, page, err)
		}
		public, err := server.queryContent(ctx, 2, "", 0, false, lang, 1, 10, true)
		if err != nil || len(public.Items) != 1 || public.Items[0].Title != "Titre français" {
			t.Fatalf("public content lang %q: %+v %v", lang, public, err)
		}
		detail, err := server.readContent(ctx, contentID, lang, false)
		if err != nil || detail.Title != "Titre français" {
			t.Fatalf("detail lang %q: %+v %v", lang, detail, err)
		}
	}
	res = siteAdminRequest(server, token, http.MethodGet, "/api/admin/categories", "2", "")
	var categories []CategoryItem
	if err := json.Unmarshal(res.Body.Bytes(), &categories); err != nil || len(categories) != 1 || categories[0].Name != "Rubrique" {
		t.Fatalf("category defaults: %s %v", res.Body.String(), err)
	}
}

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
	updatePayload := fmt.Sprintf(`{"code":"ru","name":"Русский(Updated)","is_default":false,"is_fallback":false,"is_enabled":true,"sort_order":40,"path_prefix":"ru","site_mode":"standalone","domain":"new-ru.example.com","output_dir":"new_ru"}`)
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
