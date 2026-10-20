package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
)

// TestSEOMigrationRenamesRetiredKeywords 模拟旧版本数据库升级：keywords 列与
// 模型字段应改名为 seo_title，并保留原有取值。
func TestSEOMigrationRenamesRetiredKeywords(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := CreateSchema(ctx, database); err != nil {
		t.Fatal(err)
	}

	if _, err := database.ExecContext(ctx, `
		INSERT INTO "gocms_category" ("id", "site_id", "name", "seo_title") VALUES (7, 1, '示例栏目', '栏目 SEO 标题');
		INSERT INTO "gocms_category_translation" ("category_id", "lang", "name", "seo_title") VALUES (7, 'zh-CN', '示例栏目', '栏目 SEO 标题');
		INSERT INTO "gocms_content" ("id", "site_id", "category_id", "title", "seo_title") VALUES (11, 1, 7, '示例内容', '内容 SEO 标题');
		INSERT INTO "gocms_content_translation" ("content_id", "lang", "title", "seo_title") VALUES (11, 'zh-CN', '示例内容', '内容 SEO 标题');
	`); err != nil {
		t.Fatal(err)
	}

	// 回退为新版本之前的表结构：seo_title 列改回 keywords，模型字段同理。
	for _, statement := range []string{
		`ALTER TABLE "gocms_content" RENAME COLUMN "seo_title" TO "keywords"`,
		`ALTER TABLE "gocms_content_translation" RENAME COLUMN "seo_title" TO "keywords"`,
		`ALTER TABLE "gocms_category" RENAME COLUMN "seo_title" TO "keywords"`,
		`ALTER TABLE "gocms_category_translation" RENAME COLUMN "seo_title" TO "keywords"`,
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.ExecContext(ctx, `
		UPDATE "gocms_model_field" SET "field_name" = 'keywords', "field_label" = '关键词'
		WHERE "field_name" = 'seo_title';
		UPDATE "gocms_model" SET "entry_fields" = '[{"field":"title","label":"信息标题"},{"field":"keywords","label":"关键词"}]'
		WHERE "id" = 1;
	`); err != nil {
		t.Fatal(err)
	}

	if err := CreateSchema(ctx, database); err != nil {
		t.Fatalf("upgrade failed: %v", err)
	}

	for table, want := range map[string]string{
		"gocms_content":              "内容 SEO 标题",
		"gocms_content_translation":  "内容 SEO 标题",
		"gocms_category":             "栏目 SEO 标题",
		"gocms_category_translation": "栏目 SEO 标题",
	} {
		if HasColumn(t, database, table, "keywords") {
			t.Fatalf("%s still exposes keywords column", table)
		}
		var got string
		if err := database.QueryRowContext(ctx, `SELECT "seo_title" FROM "`+table+`" LIMIT 1`).Scan(&got); err != nil {
			t.Fatalf("read %s.seo_title: %v", table, err)
		}
		if got != want {
			t.Fatalf("%s.seo_title = %q, want %q", table, got, want)
		}
	}

	var keywordFields int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM "gocms_model_field" WHERE "field_name" = 'keywords'`).Scan(&keywordFields); err != nil {
		t.Fatal(err)
	}
	if keywordFields != 0 {
		t.Fatalf("keywords model field survived: %d", keywordFields)
	}
	var fieldLabel string
	if err := database.QueryRowContext(ctx, `SELECT "field_label" FROM "gocms_model_field" WHERE "field_name" = 'seo_title' ORDER BY "table_id" LIMIT 1`).Scan(&fieldLabel); err != nil {
		t.Fatal(err)
	}
	if fieldLabel != "SEO 标题" {
		t.Fatalf("seo_title field label = %q", fieldLabel)
	}

	var rawEntryFields string
	if err := database.QueryRowContext(ctx, `SELECT "entry_fields" FROM "gocms_model" WHERE "id" = 1`).Scan(&rawEntryFields); err != nil {
		t.Fatal(err)
	}
	var entries []map[string]string
	if err := json.Unmarshal([]byte(rawEntryFields), &entries); err != nil {
		t.Fatal(err)
	}
	var fields []string
	for _, entry := range entries {
		fields = append(fields, entry["field"])
		if entry["field"] == "seo_title" && entry["label"] != "SEO 标题" {
			t.Fatalf("entry field label = %q", entry["label"])
		}
	}
	if !strings.Contains(strings.Join(fields, ","), "seo_title") {
		t.Fatalf("entry fields not migrated: %s", rawEntryFields)
	}
}

// HasColumn reports whether table declares column, for assertions in tests.
func HasColumn(t *testing.T, database *sql.DB, table, column string) bool {
	t.Helper()
	has, err := tableHasColumn(context.Background(), database, table, column)
	if err != nil {
		t.Fatal(err)
	}
	return has
}
