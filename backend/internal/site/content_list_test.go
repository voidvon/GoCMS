package site

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestAdminContentListOmitsHeavyFieldsAndKeepsDetail(t *testing.T) {
	server, database, token := newCategoryTestServer(t)
	category := createContentTestCategory(t, server, database, token, "任意栏目", 0)
	body := strings.Repeat("body text ", 8000)
	extra := `{"custom":"` + strings.Repeat("custom data ", 2000) + `"}`
	for id := 1; id <= 20; id++ {
		if _, err := database.Exec(`INSERT INTO gocms_content
			(id, category_id, route_key, title, code, summary, body, cover_image, published_at, source, seo_title, description, featured, visible, extra_data)
			VALUES (?, ?, ?, ?, 'CODE', 'summary', ?, '/cover.jpg', '2026-10-22 12:00:00', 'source', 'SEO', 'description', 1, 1, ?)`,
			id, category, fmt.Sprint(id), fmt.Sprintf("标题 %d", id), body, extra); err != nil {
			t.Fatal(err)
		}
		for _, lang := range []string{"zh-CN", "en", "fr"} {
			if _, err := database.Exec(`INSERT INTO gocms_content_translation
				(content_id, lang, title, summary, body, extra_data) VALUES (?, ?, ?, 'translated summary', ?, ?)`,
				id, lang, fmt.Sprintf("%s title %d", lang, id), body, extra); err != nil {
				t.Fatal(err)
			}
		}
	}
	response := categoryRequest(t, server, token, http.MethodGet, "/api/admin/content?lang=en&page_size=20", "")
	if response.Code != http.StatusOK {
		t.Fatalf("list returned %d: %s", response.Code, response.Body.String())
	}
	var page struct {
		Total int64                        `json:"total"`
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 20 || len(page.Items) != 20 {
		t.Fatalf("total = %d, items = %d", page.Total, len(page.Items))
	}
	fields := []string{"id", "category_id", "title", "code", "cover_image", "published_at", "featured", "visible"}
	for _, item := range page.Items {
		if len(item) != len(fields) {
			t.Fatalf("list returned unexpected fields: %v", item)
		}
		for _, field := range fields {
			if _, ok := item[field]; !ok {
				t.Errorf("missing list field %q", field)
			}
		}
	}
	if size := response.Body.Len(); size > 16*1024 {
		t.Fatalf("compact list is too large: %d bytes", size)
	}
	fullPage, err := server.queryContent(context.Background(), 1, "", 0, false, "en", 1, 20, false)
	if err != nil {
		t.Fatal(err)
	}
	fullJSON, err := json.Marshal(fullPage)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("20 items with large bodies and 3 translations: full response %d bytes; compact list %d bytes", len(fullJSON), response.Body.Len())

	response = categoryRequest(t, server, token, http.MethodGet, "/api/admin/content/1?lang=en", "")
	if response.Code != http.StatusOK {
		t.Fatalf("detail returned %d", response.Code)
	}
	var detail Content
	if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Content != body || detail.Summary != "translated summary" || detail.ExtraData["custom"] == nil || len(detail.Translations) != 3 || detail.Translations["fr"].Content != body {
		t.Fatal("detail lost body, summary, custom data or translations")
	}
}

func TestAdminContentListTitlesMatchDetail(t *testing.T) {
	server, database, token := newCategoryTestServer(t)
	insertContentTestRow(t, database, "Base title", 0, 1)
	if _, err := database.Exec(`INSERT INTO gocms_language (code, name, is_fallback) VALUES ('en', 'English', 0)`); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, lang, fallback string
		translations         map[string]string
		want                 string
	}{
		{"requested translation", "en", "zh-CN", map[string]string{"en": "English", "zh-CN": "中文"}, "English"},
		{"missing requested translation", "fr", "zh-CN", map[string]string{"zh-CN": "中文"}, "中文"},
		{"blank requested translation", "en", "zh-CN", map[string]string{"en": " \t\n", "zh-CN": "中文"}, "中文"},
		{"missing primary and fallback translation", "fr", "zh-CN", nil, "Base title"},
		{"missing primary before distinct fallback", "zh-CN", "en", map[string]string{"en": "English"}, "Base title"},
		{"blank primary allows distinct fallback", "zh-CN", "en", map[string]string{"zh-CN": " ", "en": "English"}, "English"},
		{"distinct fallback", "fr", "en", map[string]string{"en": "English"}, "English"},
		{"blank translations retain base", "fr", "en", map[string]string{"fr": " ", "en": ""}, "Base title"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := database.Exec(`UPDATE gocms_language SET is_fallback = (code = ?); DELETE FROM gocms_content_translation`, tc.fallback); err != nil {
				t.Fatal(err)
			}
			for lang, title := range tc.translations {
				if _, err := database.Exec(`INSERT INTO gocms_content_translation (content_id, lang, title) VALUES (1, ?, ?)`, lang, title); err != nil {
					t.Fatal(err)
				}
			}
			response := categoryRequest(t, server, token, http.MethodGet, "/api/admin/content?lang="+tc.lang, "")
			var page AdminContentPage
			if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			detail, err := server.readContent(context.Background(), 1, tc.lang, false)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != 1 || page.Items[0].Title != tc.want || page.Items[0].Title != detail.Title {
				t.Fatalf("list = %+v; detail title = %q; want %q", page.Items, detail.Title, tc.want)
			}
		})
	}
}

func TestAdminContentListSearchAndPagination(t *testing.T) {
	server, database, token := newCategoryTestServer(t)
	for id := 1; id <= 3; id++ {
		insertContentTestRow(t, database, fmt.Sprintf("Base %d", id), 0, int64(id))
	}
	if _, err := database.Exec(`
		UPDATE gocms_content SET code = 'CODE-ONLY' WHERE id = 1;
		UPDATE gocms_content SET body = 'base-body-marker' WHERE id = 2;
		INSERT INTO gocms_content_translation (content_id, lang, title, summary, body) VALUES
		(1, 'en', 'Needle prefix', '', ''),
		(2, 'en', 'Needle', 'summary-marker', ''),
		(3, 'en', 'Other', '', 'Needle translation-body-marker'),
		(3, 'fr', 'foreign-title-marker', '', '');`); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, query string
		page, size  int
		wantTotal   int64
		wantIDs     []int64
	}{
		{"first ranked page", "Needle", 1, 2, 3, []int64{2, 1}},
		{"second ranked page", "Needle", 2, 2, 3, []int64{3}},
		{"base body", "base-body-marker", 1, 20, 1, []int64{2}},
		{"translation body", "translation-body-marker", 1, 20, 1, []int64{3}},
		{"translation summary", "summary-marker", 1, 20, 1, []int64{2}},
		{"code", "CODE-ONLY", 1, 20, 1, []int64{1}},
		{"other language excluded", "foreign-title-marker", 1, 20, 0, []int64{}},
		{"empty query", "", 1, 2, 3, []int64{1, 2}},
		{"past last page", "", 3, 2, 3, []int64{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := fmt.Sprintf("/api/admin/content?lang=en&q=%s&page=%d&page_size=%d", url.QueryEscape(tc.query), tc.page, tc.size)
			response := categoryRequest(t, server, token, http.MethodGet, path, "")
			if response.Code != http.StatusOK {
				t.Fatalf("list returned %d: %s", response.Code, response.Body.String())
			}
			var page AdminContentPage
			if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			ids := make([]int64, 0, len(page.Items))
			for _, item := range page.Items {
				ids = append(ids, item.ID)
			}
			if !reflect.DeepEqual(ids, tc.wantIDs) || page.Total != tc.wantTotal || page.Page != tc.page || page.PageSize != tc.size || page.Query != tc.query {
				t.Fatalf("page = %+v, ids = %v; want total %d, ids %v", page, ids, tc.wantTotal, tc.wantIDs)
			}
		})
	}
}
