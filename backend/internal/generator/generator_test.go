package generator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocms/internal/db"
)

func TestGenerateUsesConfiguredThemeDataAndRoutes(t *testing.T) {
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
		INSERT INTO "gocms_site_setting" ("key", "value") VALUES
			('site_name', '示例站点'), ('site_url', 'https://example.test/'),
			('site_copyright', 'Copyright')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO "gocms_category"
			("id", "name", "parent_id", "order_id", "list_page_size", "page_type", "route_id",
			 "list_path", "list_file_pattern", "list_template", "cover_template", "detail_path",
			 "detail_file_pattern", "detail_template")
		VALUES
			(1, '任意内容', 0, 1, 2, 'list', 1, 'sections', '{id}.html', 'lists/section.html', '', 'entries', '{id}.html', 'details/item.html'),
			(2, '另一个栏目', 0, 2, 14, 'list', 2, 'other', '{id}.html', 'lists/section.html', '', 'other-entries', 'item-{id}.html', 'details/item.html'),
			(3, '单页栏目', 0, 3, 14, 'cover', 3, 'landing', 'index.html', 'lists/section.html', 'covers/landing.html', 'entries', '{id}.html', 'details/item.html'),
			(4, '封面子栏目', 3, 1, 14, 'list', 4, 'landing-child', '{id}.html', 'lists/section.html', '', 'entries', '{id}.html', 'details/item.html')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO "gocms_content"
			("id", "category_id", "route_key", "title", "summary", "body", "sort_order", "visible")
		VALUES
			(10, 1, 'first', '第一条', '摘要一', '<p>正文一</p>', 1, 1),
			(11, 1, 'second', '第二条', '摘要二', '<p>正文二</p>', 2, 1),
			(12, 1, 'third', '第三条', '摘要三', '<p>正文三</p>', 3, 1),
			(20, 2, 'other', '其他内容', '其他摘要', '<p>其他正文</p>', 1, 1),
			(30, 1, 'hidden', '隐藏内容', '', '', 4, 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO "gocms_template_label" ("key", "name", "context", "content") VALUES
			('home-banner', '首页标题', 'home', 'banner={{.site_name}}'),
			('item-card', '内容卡片', 'list', '[{{.Title}}]')`); err != nil {
		t.Fatal(err)
	}

	templates := filepath.Join(t.TempDir(), "templates")
	writeTemplate(t, templates, "index.html", `home={{setting "site_name"}} {{label "HOME-BANNER" .}} nav={{range navigation .}}{{.Name}}={{.URL}};{{end}}`)
	writeTemplate(t, templates, "msg.html", `message`)
	writeTemplate(t, templates, "search.html", `search`)
	writeTemplate(t, templates, "lists/section.html", `list={{.category_name}} page={{.list_page}}/{{.content_count}} items={{range listItems .}}{{label "item-card" .}}{{.Title}}={{.URL}};{{end}}{{with listPagination .}}pages={{.Pages}} current={{.Page}}{{end}}`)
	writeTemplate(t, templates, "details/item.html", `detail={{.title}} category={{.category_name}} root={{.category_root_name}} body={{.body}} previous={{.previous_url}} next={{.next_url}} related={{range relatedItems . 2}}{{.Title}}={{.URL}};{{end}}`)
	writeTemplate(t, templates, "covers/landing.html", `cover={{.category_name}} children={{range listChildren .}}{{.Name}}={{.URL}};{{end}}`)

	root := t.TempDir()
	web := filepath.Join(root, "web")
	publisher := Publisher{DB: database, Web: web, Templates: templates, Data: filepath.Join(root, "data")}
	report, err := publisher.Generate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.Contents != 4 {
		t.Fatalf("visible content count = %d, want 4", report.Contents)
	}
	for _, relative := range []string{
		"index.html", "msg.html", "search.html", "sections/1.html", "sections/1-1.html", "sections/1-2.html",
		"sections/index.html", "entries/first.html", "entries/second.html", "entries/third.html",
		"other/2.html", "other/index.html", "other-entries/item-other.html", "landing/index.html", "Sitemap.xml",
	} {
		if _, err := os.Stat(filepath.Join(web, filepath.FromSlash(relative))); err != nil {
			t.Fatalf("missing generated page %s: %v", relative, err)
		}
	}
	if _, err := os.Stat(filepath.Join(web, "entries/hidden.html")); !os.IsNotExist(err) {
		t.Fatalf("hidden content was published: %v", err)
	}

	listBody := readGenerated(t, web, "sections/1-2.html")
	if !strings.Contains(listBody, "list=任意内容 page=2/3") || !strings.Contains(listBody, "[第三条]第三条=/entries/third.html") || strings.Contains(listBody, "隐藏内容") {
		t.Fatalf("unexpected configured list output: %s", listBody)
	}
	if homeBody := readGenerated(t, web, "index.html"); !strings.Contains(homeBody, "home=示例站点 banner=示例站点") {
		t.Fatalf("unexpected label output: %s", homeBody)
	}
	detailBody := readGenerated(t, web, "entries/second.html")
	if !strings.Contains(detailBody, "detail=第二条 category=任意内容 root=任意内容") || !strings.Contains(detailBody, "previous=/entries/first.html") || !strings.Contains(detailBody, "next=/entries/third.html") || !strings.Contains(detailBody, "related=第一条=/entries/first.html;第三条=/entries/third.html;") {
		t.Fatalf("unexpected configured detail output: %s", detailBody)
	}
	coverBody := readGenerated(t, web, "landing/index.html")
	if coverBody != "cover=单页栏目 children=封面子栏目=/landing-child/4.html;" {
		t.Fatalf("unexpected cover output: %s", coverBody)
	}

	before := readGenerated(t, web, "index.html")
	writeTemplate(t, templates, "index.html", `{{unknownFunction .}}`)
	if _, err := publisher.Generate(ctx); err == nil {
		t.Fatal("invalid theme template was accepted")
	}
	if after := readGenerated(t, web, "index.html"); after != before {
		t.Fatal("failed publication changed the live site")
	}
}

func TestContentURLUsesConfiguredCategoryRoute(t *testing.T) {
	c := &content{tables: map[string][]Row{
		"gocms_category": {{"id": "10", "detail_path": "entries", "detail_file_pattern": "item-{id}.html"}},
	}}
	if got := c.contentURL(Row{"category_id": "10", "route_key": "185"}); got != "/entries/item-185.html" {
		t.Fatalf("content URL = %q", got)
	}
}

func TestSharedListDirectoryKeepsFirstCategoryIndex(t *testing.T) {
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
		INSERT INTO "gocms_category"
			("id", "name", "parent_id", "order_id", "route_id", "list_path", "list_file_pattern", "list_template")
		VALUES
			(1, '第一个栏目', 0, 1, 1, 'catalog', '{id}.html', 'category.html'),
			(2, '第二个栏目', 0, 2, 2, 'catalog', '{id}.html', 'category.html')`); err != nil {
		t.Fatal(err)
	}

	templates := filepath.Join(t.TempDir(), "templates")
	writeTemplate(t, templates, "index.html", "home")
	writeTemplate(t, templates, "msg.html", "message")
	writeTemplate(t, templates, "search.html", "search")
	writeTemplate(t, templates, "category.html", "{{.category_name}}")

	root := t.TempDir()
	publisher := Publisher{DB: database, Web: filepath.Join(root, "web"), Templates: templates, Data: filepath.Join(root, "data")}
	if _, err := publisher.Generate(ctx); err != nil {
		t.Fatal(err)
	}
	if got := readGenerated(t, filepath.Join(root, "web"), "catalog/index.html"); got != "第一个栏目" {
		t.Fatalf("shared directory index = %q, want first configured category", got)
	}
}

func TestCoverCategoryDoesNotPaginate(t *testing.T) {
	c := &content{tables: map[string][]Row{
		"gocms_category": {
			{"id": "3", "name": "任意封面", "parent_id": "0", "route_id": "3", "page_type": "cover", "list_path": "landing", "list_file_pattern": "index.html", "cover_template": "cover.html"},
		},
		"gocms_content": {},
	}}
	if got := c.categoryListURL(c.cat(3), 1); got != "/landing/" {
		t.Fatalf("cover URL = %q", got)
	}
	if got := c.categoryListPagePath(c.cat(3), 2); got != "landing/index.html" {
		t.Fatalf("cover page path = %q", got)
	}
}

func TestNormalizeLinksLeavesMissingThemeImageUntouched(t *testing.T) {
	root := t.TempDir()
	assets := filepath.Join(root, "assets")
	theme := filepath.Join(root, "theme")
	if err := os.MkdirAll(filepath.Join(theme, "images"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(theme, "images", "content-placeholder.jpg"), []byte("placeholder"), 0644); err != nil {
		t.Fatal(err)
	}

	const page = `<img src="/images/missing.jpg">`
	c := &content{pages: map[string][]byte{"index.html": []byte(page)}}
	if err := c.normalizeLinks(assets, theme); err != nil {
		t.Fatal(err)
	}
	if got := string(c.pages["index.html"]); got != page {
		t.Fatalf("missing image was rewritten: %q", got)
	}
}

func writeTemplate(t *testing.T, root, relative, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func readGenerated(t *testing.T, root, relative string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
