package site

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestTranslationFallbackStopsAtConfiguredLanguage(t *testing.T) {
	for _, primary := range []string{"en", "zh-CN"} {
		t.Run(primary, func(t *testing.T) {
			server, database, token := newCategoryTestServer(t)
			if _, err := database.Exec(`
				INSERT INTO gocms_language (code, name, is_enabled) VALUES ('en', 'English', 1), ('fr', 'French', 1);
				UPDATE gocms_language SET is_default = (code = ?), is_fallback = (code = 'en');
				INSERT INTO gocms_model_field (table_id, field_name, field_label, is_translatable)
				VALUES (2, 'downloads_html', 'Documents', 1);
				INSERT INTO gocms_category (id, name, seo_title, description, cover_content, link_url)
				VALUES (1, '旧名称', '旧SEO', '旧描述', '旧封面', '/old/');
				INSERT INTO gocms_category_translation (category_id, lang, name)
				VALUES (1, 'en', ''), (1, 'fr', 'French category'), (1, 'zh-CN', '中文栏目');
				INSERT INTO gocms_content (id, category_id, model_id, title, summary, body, seo_title, description, extra_data)
				VALUES (1, 1, 2, '旧标题', '旧摘要', '旧正文', '旧SEO', '旧描述', '{"downloads_html":"chinese.pdf","shared":"shared value"}');
				INSERT INTO gocms_content_translation (content_id, lang, title, extra_data, publish_status)
				VALUES (1, 'en', '', '{}', 'draft'), (1, 'fr', 'French title', '{"downloads_html":"  "}', 'published'),
				(1, 'zh-CN', '中文标题', '{"downloads_html":"chinese.pdf"}', 'published');`, primary); err != nil {
				t.Fatal(err)
			}

			read := func(lang string) Content {
				t.Helper()
				res := categoryRequest(t, server, token, http.MethodGet, "/api/admin/content/1?lang="+lang, "")
				if res.Code != http.StatusOK {
					t.Fatalf("content returned %d: %s", res.Code, res.Body.String())
				}
				var item Content
				if err := json.Unmarshal(res.Body.Bytes(), &item); err != nil {
					t.Fatal(err)
				}
				return item
			}
			categories := func(lang string) CategoryItem {
				t.Helper()
				res := categoryRequest(t, server, token, http.MethodGet, "/api/admin/categories?lang="+lang, "")
				var items []CategoryItem
				if res.Code != http.StatusOK || json.Unmarshal(res.Body.Bytes(), &items) != nil || len(items) != 1 {
					t.Fatalf("categories returned %d: %s", res.Code, res.Body.String())
				}
				return items[0]
			}

			for _, lang := range []string{"en", "fr", "es"} {
				item := read(lang)
				if item.Summary != "" || item.Content != "" || item.SEOTitle != "" || item.Description != "" || item.ExtraData["downloads_html"] != nil || item.ExtraData["shared"] != "shared value" {
					t.Fatalf("%s inherited base fields: %+v", lang, item)
				}
				if lang != "fr" && item.Title != "" {
					t.Fatalf("%s inherited base title: %q", lang, item.Title)
				}
				cat := categories(lang)
				if cat.SEOTitle != "" || cat.Description != "" || cat.CoverContent != "" || cat.LinkURL != "" {
					t.Fatalf("%s inherited base category fields: %+v", lang, cat)
				}
			}
			if item := read("en"); item.PublishStatus != "draft" || item.Translations["en"].ExtraData["downloads_html"] != nil {
				t.Fatalf("stored translation or status changed: %+v", item)
			}
			if item := read("zh-CN"); item.ExtraData["downloads_html"] != "chinese.pdf" {
				t.Fatalf("local documents lost: %+v", item)
			}

			if _, err := database.Exec(`
				UPDATE gocms_content_translation SET title='English title', summary='English summary', body='English body',
				seo_title='English SEO', description='English description', extra_data='{"downloads_html":"english.pdf"}' WHERE lang='en';
				UPDATE gocms_category_translation SET name='English category', seo_title='English SEO', description='English description',
				cover_content='English cover', link_url='/english/' WHERE lang='en';`); err != nil {
				t.Fatal(err)
			}
			for _, lang := range []string{"fr", "es"} {
				item := read(lang)
				if item.Summary != "English summary" || item.Content != "English body" || item.SEOTitle != "English SEO" || item.Description != "English description" || item.ExtraData["downloads_html"] != "english.pdf" {
					t.Fatalf("%s did not resolve English fields: %+v", lang, item)
				}
				cat := categories(lang)
				if cat.SEOTitle != "English SEO" || cat.Description != "English description" || cat.CoverContent != "English cover" || cat.LinkURL != "/english/" {
					t.Fatalf("%s did not resolve English category fields: %+v", lang, cat)
				}
			}
			if read("fr").Title != "French title" || categories("fr").Name != "French category" || read("zh-CN").ExtraData["downloads_html"] != "chinese.pdf" {
				t.Fatal("fallback replaced local values")
			}
			if _, err := database.Exec(`DELETE FROM gocms_content_translation WHERE lang='en'; DELETE FROM gocms_category_translation WHERE lang='en'`); err != nil {
				t.Fatal(err)
			}
			if item := read("en"); item.Title != "" || item.Content != "" || item.ExtraData["downloads_html"] != nil {
				t.Fatalf("missing English translation inherited the base record: %+v", item)
			}
			if cat := categories("en"); cat.Name != "" || cat.Description != "" || cat.LinkURL != "" {
				t.Fatalf("missing English category translation inherited the base record: %+v", cat)
			}
			if _, err := database.Exec(`UPDATE gocms_language SET is_fallback=(code='zh-CN')`); err != nil {
				t.Fatal(err)
			}
			if item := read("en"); item.Title != "中文标题" || item.ExtraData["downloads_html"] != "chinese.pdf" || item.FallbackLang != "zh-CN" {
				t.Fatalf("configured fallback change was ignored: %+v", item)
			}
		})
	}
}
