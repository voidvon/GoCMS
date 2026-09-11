package legacyimport

import (
	"context"
	"strings"
	"testing"

	"gocms/internal/db"
)

func TestCustomLabelSettings(t *testing.T) {
	settings := customLabelSettings([]sourceRow{
		{"lname": "#BM_linkind#", "lcontent": `<a href="https://example.test">Example</a>`},
		{"lname": "#BM_indexfoot#", "lcontent": `Copyright &#169;2004-2015 <a href="#HOPE_WebUrl#">Example</a>`},
	})
	if got := settings["site_footer_links"]; got != `<a href="https://example.test">Example</a>` {
		t.Fatalf("footer links = %q", got)
	}
	if got := settings["site_copyright_years"]; got != "2004-2015" {
		t.Fatalf("copyright years = %q", got)
	}
}

func TestImportTemplateLabels(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	if err := db.CreateSchema(ctx, database); err != nil {
		t.Fatal(err)
	}

	labels := []sourceRow{
		{"id": "50", "lname": "#BM_top#", "ldes": "头部文件", "lcontent": `#BM_child# #HOPE_Webname# #HOPE_TITLE# #categories_plain()#`, "lkind": "11"},
		{"id": "51", "lname": "#BM_child#", "ldes": "子片段", "lcontent": `<span>#HOPE_WebMsn#</span>`, "lkind": "11"},
	}
	categories := []sourceRow{{"id": "11", "kindname": "站点片段"}}
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := importTemplateLabels(ctx, transaction, categories, labels); err != nil {
		_ = transaction.Rollback()
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}

	var categoryID int64
	if err := database.QueryRow(`SELECT "id" FROM "gocms_template_label_category" WHERE "name" = '站点片段'`).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}
	var key, name, content string
	if err := database.QueryRow(`SELECT "key", "name", "content" FROM "gocms_template_label" WHERE "key" = 'bm-top'`).Scan(&key, &name, &content); err != nil {
		t.Fatal(err)
	}
	if key != "bm-top" || name != "头部文件" || !strings.Contains(content, `{{label "bm-child" .}}`) ||
		!strings.Contains(content, `{{setting "site_name"}}`) || !strings.Contains(content, `{{.title}}`) ||
		!strings.Contains(content, `{{range catalogCategories .}}`) {
		t.Fatalf("unexpected migrated label: key=%q name=%q content=%q", key, name, content)
	}
	var storedCategoryID int64
	if err := database.QueryRow(`SELECT "category_id" FROM "gocms_template_label" WHERE "key" = 'bm-top'`).Scan(&storedCategoryID); err != nil {
		t.Fatal(err)
	}
	if storedCategoryID != categoryID {
		t.Fatalf("label category id = %d, want %d", storedCategoryID, categoryID)
	}

	if _, err := database.Exec(`UPDATE "gocms_template_label" SET "content" = 'edited' WHERE "key" = 'bm-top'`); err != nil {
		t.Fatal(err)
	}
	transaction, err = database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := importTemplateLabels(ctx, transaction, categories, labels); err != nil {
		_ = transaction.Rollback()
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM "gocms_template_label"`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("migrated label count = %d, want 2", count)
	}
	if err := database.QueryRow(`SELECT "content" FROM "gocms_template_label" WHERE "key" = 'bm-top'`).Scan(&content); err != nil {
		t.Fatal(err)
	}
	if content != "edited" {
		t.Fatalf("existing label was overwritten: %q", content)
	}
}

func TestLegacyLabelKey(t *testing.T) {
	for _, item := range []struct {
		raw, want string
	}{
		{"#BM_top#", "bm-top"},
		{"#Legacy Header#", "legacy-header"},
		{"#123#", "label-123"},
	} {
		if got := legacyLabelKey(item.raw); got != item.want {
			t.Errorf("legacyLabelKey(%q) = %q, want %q", item.raw, got, item.want)
		}
	}
}
