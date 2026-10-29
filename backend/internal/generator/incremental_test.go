package generator

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocms/internal/db"
)

func incrementalFixture(t *testing.T) Publisher {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.CreateSchema(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO gocms_category
		(id, name, parent_id, order_id, list_page_size, page_type, route_id, list_path, list_file_pattern, list_template, cover_template, detail_path, detail_file_pattern, detail_template)
		VALUES
		(71, '任意总览', 0, 1, 2, 'cover', 710, 'portal', 'index.html', '', 'custom/cover.html', 'entries', '{id}/index.html', 'custom/detail.html'),
		(82, '任意父级', 71, 2, 2, 'list', 820, 'catalog', 'index.html', 'custom/list.html', '', 'entries', '{id}/index.html', 'custom/detail.html'),
		(93, '任意叶子', 82, 3, 2, 'list', 930, 'feed', 'p-{id}.html', 'custom/list.html', '', 'entries', '{id}/index.html', 'custom/detail.html'),
		(104, '无关栏目', 0, 0, 2, 'list', 1040, 'feed', 'p-{id}.html', 'custom/other.html', '', 'other', '{id}.html', 'custom/other.html');
		INSERT INTO gocms_content (id, category_id, route_key, title, visible, sort_order) VALUES
		(301, 93, 'selected', '原始标题', 1, 1),
		(302, 93, 'second', '同栏目第二条', 1, 2),
		(303, 93, 'third', '同栏目第三条', 1, 3),
		(304, 93, 'fourth', '同栏目第四条', 1, 4),
		(305, 93, 'fifth', '同栏目第五条', 1, 5),
		(401, 104, 'unrelated', '无关内容', 1, 6)`); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	p := Publisher{DB: database, Templates: filepath.Join(root, "templates"), Web: filepath.Join(root, "web"), Data: filepath.Join(root, "data")}
	writeTemplate(t, p.Templates, "index.html", `home {{range contentItems 0 50 true false "sort"}}{{.Title}};{{end}}`)
	writeTemplate(t, p.Templates, "msg.html", `message`)
	writeTemplate(t, p.Templates, "search.html", `search`)
	writeTemplate(t, p.Templates, "custom/cover.html", `cover={{.category_name}} {{range contentItems 0 50 true false "sort"}}{{.Title}};{{end}}`)
	writeTemplate(t, p.Templates, "custom/list.html", `list={{.category_name}} {{range listItems .}}{{.Title}};{{end}} {{with listPagination .}}{{.Page}}/{{.Pages}}{{end}}`)
	writeTemplate(t, p.Templates, "custom/detail.html", `detail={{.title}} previous={{.previous_url}} next={{.next_url}}`)
	writeTemplate(t, p.Templates, "custom/other.html", `unrelated={{.title}}{{.category_name}}`)
	return p
}

func TestGenerateContentUpdatesAncestorsAndAllPaginationOnly(t *testing.T) {
	p := incrementalFixture(t)
	ctx := context.Background()
	if _, err := p.Generate(ctx); err != nil {
		t.Fatal(err)
	}
	untouched := []string{"entries/second/index.html", "other/unrelated.html", "feed/p-1040-1.html", "feed/index.html", "search.html", "msg.html"}
	before := map[string]os.FileInfo{}
	for _, page := range untouched {
		var err error
		before[page], err = os.Stat(filepath.Join(p.Web, page))
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.DB.Exec(`UPDATE gocms_content SET title = '更新后标题' WHERE id = 301`); err != nil {
		t.Fatal(err)
	}
	// An unrelated template would fail if a full publication were performed.
	writeTemplate(t, p.Templates, "custom/other.html", `{{include "missing.html" .}}`)
	report, err := p.GenerateContent(ctx, 301)
	if err != nil {
		t.Fatal(err)
	}
	if report.State != "success" || report.Contents != 1 || report.Files != 10 || report.ContentID != 301 {
		t.Fatalf("unexpected report: %+v", report)
	}
	for _, page := range []string{"index.html", "portal/index.html", "catalog/index.html", "feed/p-930.html", "feed/p-930-1.html", "entries/selected/index.html"} {
		if body := readGenerated(t, p.Web, page); !strings.Contains(body, "更新后标题") {
			t.Errorf("%s did not refresh: %s", page, body)
		}
	}
	for _, page := range []string{"catalog/index-2.html", "catalog/index-3.html", "feed/p-930-2.html", "feed/p-930-3.html"} {
		if body := readGenerated(t, p.Web, page); !strings.Contains(body, "/3") {
			t.Errorf("pagination not generated for %s: %s", page, body)
		}
	}
	for _, page := range untouched {
		after, err := os.Stat(filepath.Join(p.Web, page))
		if err != nil || !os.SameFile(before[page], after) {
			t.Errorf("unrelated page was replaced: %s (%v)", page, err)
		}
	}
	if _, err := p.GenerateContent(ctx, 99999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing entry: %v", err)
	}
}

func TestGenerateContentRemovesOldRoutesAndExcessPages(t *testing.T) {
	p := incrementalFixture(t)
	ctx := context.Background()
	if _, err := p.Generate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := p.DB.Exec(`UPDATE gocms_content SET visible = 0 WHERE id = 305`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.GenerateContent(ctx, 305); err != nil {
		t.Fatal(err)
	}
	for _, page := range []string{"entries/fifth/index.html", "feed/p-930-3.html", "catalog/index-3.html"} {
		if _, err := os.Stat(filepath.Join(p.Web, page)); !os.IsNotExist(err) {
			t.Errorf("obsolete page retained: %s (%v)", page, err)
		}
	}
	if _, err := p.DB.Exec(`UPDATE gocms_content SET route_key = 'renamed', category_id = 104 WHERE id = 301`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.GenerateContent(ctx, 301); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.Web, "entries/selected/index.html")); !os.IsNotExist(err) {
		t.Fatalf("old detail retained: %v", err)
	}
	if got := readGenerated(t, p.Web, "other/renamed.html"); !strings.Contains(got, "原始标题") {
		t.Fatal(got)
	}
	if got := readGenerated(t, p.Web, "feed/p-930-1.html"); strings.Contains(got, "原始标题") {
		t.Fatalf("old category still lists moved entry: %s", got)
	}
}

func TestGenerateContentLanguagesAndSiteIsolation(t *testing.T) {
	p := incrementalFixture(t)
	ctx := context.Background()
	p.SiteID, p.OutputDir = 1, "custom-site"
	if _, err := p.DB.Exec(`INSERT INTO gocms_language (code, name, is_enabled, site_mode, path_prefix, output_dir) VALUES
		('en', 'English', 1, 'subdir', 'english', ''), ('ru', 'Russian', 1, 'standalone', '', 'independent');
		INSERT INTO gocms_content_translation (content_id, lang, title) VALUES (301, 'en', 'English before'), (301, 'ru', 'Russian before');
		INSERT INTO gocms_content (id, site_id, category_id, route_key, title, visible) VALUES (901, 2, 93, 'other-site', '其他站点', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Generate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := p.DB.Exec(`UPDATE gocms_content SET title = '中文更新' WHERE id = 301;
		UPDATE gocms_content_translation SET title = 'English after' WHERE content_id = 301 AND lang = 'en';
		UPDATE gocms_content_translation SET title = 'Russian after' WHERE content_id = 301 AND lang = 'ru'`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.GenerateContent(ctx, 301); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct{ root, prefix, title string }{
		{p.TargetWeb(), "", "中文更新"},
		{p.TargetWeb(), "english/", "English after"},
		{p.LanguageTargetWeb(LanguageInfo{SiteMode: "standalone", OutputDir: "independent", Code: "ru"}), "", "Russian after"},
	} {
		for _, page := range []string{"index.html", "entries/selected/index.html", "feed/p-930-1.html", "catalog/index.html", "portal/index.html"} {
			if body := readGenerated(t, check.root, check.prefix+page); !strings.Contains(body, check.title) || strings.Contains(body, "其他站点") {
				t.Errorf("%s/%s: %s", check.root, page, body)
			}
		}
	}
	if _, err := p.GenerateContent(ctx, 901); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-site content: %v", err)
	}
}

func TestGenerateContentFailuresPreserveLivePages(t *testing.T) {
	p := incrementalFixture(t)
	ctx := context.Background()
	if _, err := p.Generate(ctx); err != nil {
		t.Fatal(err)
	}
	before := readGenerated(t, p.Web, "index.html")
	if _, err := p.DB.Exec(`UPDATE gocms_content SET title = '不可提前发布' WHERE id = 301`); err != nil {
		t.Fatal(err)
	}
	writeTemplate(t, p.Templates, "custom/list.html", `{{include "missing.html" .}}`)
	if report, err := p.GenerateContent(ctx, 301); err == nil || report.State != "failed" {
		t.Fatalf("expected render failure: %+v %v", report, err)
	}
	if got := readGenerated(t, p.Web, "index.html"); got != before {
		t.Fatal("render failure changed live home")
	}
	writeTemplate(t, p.Templates, "custom/list.html", `list={{.category_name}}`)
	if err := os.MkdirAll(p.Web+".previous", 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := p.GenerateContent(ctx, 301); err == nil {
		t.Fatal("backup collision was accepted")
	}
	if got := readGenerated(t, p.Web, "index.html"); got != before {
		t.Fatal("staging failure changed live home through hard link")
	}
	lock, err := os.OpenFile(filepath.Join(p.Data, p.lockPath()), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	unlock, err := acquirePublishLock(lock)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err := p.GenerateContent(ctx, 301); !errors.Is(err, ErrPublishBusy) {
		t.Fatalf("publish lock not shared: %v", err)
	}
}

func TestGenerateContentStandaloneFailurePreservesAllLanguages(t *testing.T) {
	p := incrementalFixture(t)
	p.SiteID = 1
	ctx := context.Background()
	if _, err := p.DB.Exec(`INSERT INTO gocms_language (code, name, is_enabled, site_mode, output_dir)
		VALUES ('ru', 'Russian', 1, 'standalone', 'independent')`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Generate(ctx); err != nil {
		t.Fatal(err)
	}
	standalone := p.LanguageTargetWeb(LanguageInfo{SiteMode: "standalone", OutputDir: "independent", Code: "ru"})
	before := readGenerated(t, p.TargetWeb(), "index.html")
	if _, err := p.DB.Exec(`UPDATE gocms_content SET title = '不可提前发布' WHERE id = 301`); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(standalone+".previous", 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := p.GenerateContent(ctx, 301); err == nil {
		t.Fatal("standalone staging error was accepted")
	}
	for _, web := range []string{p.TargetWeb(), standalone} {
		if got := readGenerated(t, web, "index.html"); got != before {
			t.Fatalf("failed publication changed %s", web)
		}
	}
}

func TestPageUpdateRollsBackDirectoriesWhenHistoryCannotBeSaved(t *testing.T) {
	root := t.TempDir()
	var updates []*stagedPages
	for _, name := range []string{"main", "standalone"} {
		web := filepath.Join(root, name)
		writeTemplate(t, web, "index.html", "before")
		update, err := stagePageUpdate(context.Background(), web, map[string][]byte{"index.html": []byte("after")}, false, true, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer update.discard()
		updates = append(updates, update)
	}
	failure := errors.New("history storage failed")
	if err := installPageUpdates(updates, func() error { return failure }); !errors.Is(err, failure) {
		t.Fatalf("expected history failure: %v", err)
	}
	for _, update := range updates {
		if got := readGenerated(t, update.web, "index.html"); got != "before" {
			t.Fatalf("history failure changed %s", update.web)
		}
		if _, err := os.Stat(update.backup); !os.IsNotExist(err) {
			t.Fatalf("backup not restored: %v", err)
		}
	}
}

func TestGenerateContentLinkAncestorAndCustomHome(t *testing.T) {
	p := incrementalFixture(t)
	ctx := context.Background()
	p.HomeTemplate = "custom/home.html"
	writeTemplate(t, p.Templates, p.HomeTemplate, `custom-home {{range contentItems 0 50 true false "sort"}}{{.Title}};{{end}}`)
	if _, err := p.DB.Exec(`UPDATE gocms_category SET page_type = 'link', link_url = 'https://example.test/' WHERE id = 82`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Generate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := p.DB.Exec(`UPDATE gocms_content SET title = '穿过链接父级' WHERE id = 301`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.GenerateContent(ctx, 301); err != nil {
		t.Fatal(err)
	}
	if got := readGenerated(t, p.Web, "portal/index.html"); !strings.Contains(got, "穿过链接父级") {
		t.Fatalf("root above link ancestor did not refresh: %s", got)
	}
	if got := readGenerated(t, p.Web, "index.html"); !strings.HasPrefix(got, "custom-home") || !strings.Contains(got, "穿过链接父级") {
		t.Fatalf("custom home template ignored: %s", got)
	}
	if _, err := os.Stat(filepath.Join(p.Web, "catalog/index.html")); !os.IsNotExist(err) {
		t.Fatalf("link ancestor generated a static page: %v", err)
	}
}
