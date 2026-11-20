package generator

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"gocms/internal/db"
)

func TestGenerateUsesOnlyConfiguredTranslationFallback(t *testing.T) {
	for _, primary := range []string{"en", "zh-CN"} {
		t.Run(primary, func(t *testing.T) {
			database, err := db.Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			ctx := context.Background()
			if err := db.CreateSchema(ctx, database); err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec(`
				INSERT INTO gocms_language (code, name, is_enabled, path_prefix)
				VALUES ('en', 'English', 1, 'en'), ('fr', 'French', 1, 'fr');
				UPDATE gocms_language SET is_default = (code = ?), is_fallback = (code = 'en');
				INSERT INTO gocms_model_field (table_id, field_name, field_label, is_translatable)
				VALUES (2, 'downloads_html', 'Documents', 1);
				INSERT INTO gocms_site_setting (key, value) VALUES ('site_url', 'https://example.test/');
				INSERT INTO gocms_category (id, name, list_path, list_template, detail_path, detail_template, seo_title, description, cover_content)
				VALUES (1, '旧栏目', 'topics', 'custom/list.html', 'entries', 'custom/detail.html', '旧SEO', '旧描述', '旧封面');
				INSERT INTO gocms_category_translation (category_id, lang, name, description)
				VALUES (1, 'en', 'English category', 'English description'), (1, 'fr', 'French category', '');
				INSERT INTO gocms_content (id, category_id, route_key, model_id, title, summary, body, seo_title, description, visible, extra_data)
				VALUES (1, 1, 'empty.html', 2, '旧标题', '旧摘要', '旧正文', '旧SEO', '旧描述', 1, '{"downloads_html":"chinese.pdf","shared":"shared"}'),
				(2, 1, 'english.html', 2, '旧标题', '', '', '', '', 1, '{"downloads_html":"chinese.pdf"}'),
				(3, 1, 'shared.html', 1, 'Shared title', '', '', '', '', 1, '{"downloads_html":"shared.pdf"}');
				INSERT INTO gocms_content_translation (content_id, lang, title, extra_data)
				VALUES (1, 'en', 'English title', '{}'), (1, 'fr', 'French title', '{"downloads_html":" "}'),
				(1, 'zh-CN', '中文标题', '{"downloads_html":"chinese.pdf"}'),
				(2, 'en', 'English documents', '{"downloads_html":"english.pdf"}'),
				(2, 'zh-CN', '中文资料', '{"downloads_html":"chinese.pdf"}');`, primary); err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			templates := filepath.Join(root, "templates")
			writeTemplate(t, templates, "index.html", `home`)
			writeTemplate(t, templates, "msg.html", `message`)
			writeTemplate(t, templates, "search.html", `search`)
			writeTemplate(t, templates, "custom/list.html", `name={{.category_name}};seo={{.seo_title}};description={{.description}};body={{.body}}`)
			writeTemplate(t, templates, "custom/detail.html", `title={{.title}};summary={{.summary}};body={{.body}};seo={{.seo_title}};description={{.description}};docs={{if .downloads_html}}{{.downloads_html}}{{end}};shared={{if .shared}}{{.shared}}{{end}}`)
			web := filepath.Join(root, "web")
			publisher := Publisher{DB: database, Web: web, Templates: templates, Data: filepath.Join(root, "data")}
			if _, err := publisher.Generate(ctx); err != nil {
				t.Fatal(err)
			}
			enPrefix, zhPrefix := "en/", ""
			if primary == "en" {
				enPrefix, zhPrefix = "", "zh-cn/"
			}
			for _, prefix := range []string{enPrefix, "fr/"} {
				body := readGenerated(t, web, prefix+"entries/empty.html")
				if strings.Contains(body, "旧") || strings.Contains(body, "chinese.pdf") || !strings.Contains(body, ";summary=;body=;seo=;description=;docs=;shared=shared") {
					t.Fatalf("%s retained base fields: %s", prefix, body)
				}
				if body := readGenerated(t, web, prefix+"entries/english.html"); !strings.Contains(body, "docs=english.pdf") || strings.Contains(body, "chinese.pdf") {
					t.Fatalf("%s lost English documents: %s", prefix, body)
				}
				if body := readGenerated(t, web, prefix+"entries/shared.html"); !strings.Contains(body, "docs=shared.pdf") {
					t.Fatalf("field from another model was treated as translatable: %s", body)
				}
			}
			if body := readGenerated(t, web, zhPrefix+"entries/empty.html"); !strings.Contains(body, "docs=chinese.pdf") {
				t.Fatalf("local documents lost: %s", body)
			}
			if body := readGenerated(t, web, "fr/topics/1.html"); body != "name=French category;seo=;description=English description;body=" {
				t.Fatalf("category did not fall back per field: %s", body)
			}
			if _, err := database.Exec(`DELETE FROM gocms_content_translation WHERE content_id=1 AND lang='en'; DELETE FROM gocms_category_translation WHERE lang='en'`); err != nil {
				t.Fatal(err)
			}
			if _, err := publisher.GenerateContent(ctx, 1); err != nil {
				t.Fatal(err)
			}
			if body := readGenerated(t, web, enPrefix+"entries/empty.html"); body != "title=;summary=;body=;seo=;description=;docs=;shared=shared" {
				t.Fatalf("missing English translation inherited the base record: %s", body)
			}
			if body := readGenerated(t, web, "fr/topics/1.html"); body != "name=French category;seo=;description=;body=" {
				t.Fatalf("missing fallback category fields inherited the base record: %s", body)
			}
			if _, err := database.Exec(`UPDATE gocms_language SET is_fallback=(code='zh-CN')`); err != nil {
				t.Fatal(err)
			}
			if _, err := publisher.GenerateContent(ctx, 1); err != nil {
				t.Fatal(err)
			}
			if body := readGenerated(t, web, enPrefix+"entries/empty.html"); !strings.Contains(body, "title=中文标题;") || !strings.Contains(body, "docs=chinese.pdf") {
				t.Fatalf("configured fallback change was ignored: %s", body)
			}
		})
	}
}
