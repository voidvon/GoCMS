package db

import (
	"context"
	"database/sql"
	"fmt"
)

const (
	CategoryTable            = "gocms_category"
	CategoryTranslationTable = "gocms_category_translation"
	unifiedCategoryTable     = CategoryTable
)

// EnsureUnifiedCategories creates the category model used by the CMS. A
// category owns its route and template bindings, which keeps presentation
// decisions in database configuration and themes.
func EnsureUnifiedCategories(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS "`+unifiedCategoryTable+`" (
			"id" INTEGER PRIMARY KEY AUTOINCREMENT,
			"site_id" INTEGER NOT NULL DEFAULT 1,
			"name" TEXT NOT NULL,
			"parent_id" INTEGER NOT NULL DEFAULT 0,
			"order_id" INTEGER NOT NULL DEFAULT 0,
			"list_page_size" INTEGER NOT NULL DEFAULT 14,
			"page_type" TEXT NOT NULL DEFAULT 'list',
			"route_id" INTEGER NOT NULL DEFAULT 0,
			"list_path" TEXT NOT NULL DEFAULT 'category',
			"list_file_pattern" TEXT NOT NULL DEFAULT '{id}.html',
			"list_template" TEXT NOT NULL DEFAULT 'category_list.html',
			"cover_template" TEXT NOT NULL DEFAULT '',
			"detail_path" TEXT NOT NULL DEFAULT 'content',
			"detail_file_pattern" TEXT NOT NULL DEFAULT '{id}.html',
			"detail_template" TEXT NOT NULL DEFAULT 'content_detail.html',
			"keywords" TEXT NOT NULL DEFAULT '',
			"description" TEXT NOT NULL DEFAULT '',
			"cover_content" TEXT NOT NULL DEFAULT '',
			"model_id" INTEGER NOT NULL DEFAULT 1,
			"link_url" TEXT NOT NULL DEFAULT '',
			"nav_position" TEXT NOT NULL DEFAULT 'main'
		)`); err != nil {
		return fmt.Errorf("create category table: %w", err)
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_gocms_category_site ON "gocms_category" ("site_id", "parent_id", "order_id", "id")`,
		`CREATE INDEX IF NOT EXISTS idx_gocms_category_parent ON "gocms_category" ("parent_id", "order_id", "id")`,
		`CREATE INDEX IF NOT EXISTS idx_gocms_category_model ON "gocms_category" ("model_id", "id")`,
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create category index: %w", err)
		}
	}
	for name, definition := range map[string]string{
		"site_id":        "INTEGER NOT NULL DEFAULT 1",
		"list_page_size": "INTEGER NOT NULL DEFAULT 14",
		"page_type":      "TEXT NOT NULL DEFAULT 'list'",
		"cover_template": "TEXT NOT NULL DEFAULT ''",
		"keywords":       "TEXT NOT NULL DEFAULT ''",
		"description":    "TEXT NOT NULL DEFAULT ''",
		"cover_content":  "TEXT NOT NULL DEFAULT ''",
		"model_id":       "INTEGER NOT NULL DEFAULT 1",
		"link_url":       "TEXT NOT NULL DEFAULT ''",
		"nav_position":   "TEXT NOT NULL DEFAULT 'main'",
	} {
		if err := ensureTableColumn(ctx, database, unifiedCategoryTable, name, definition); err != nil {
			return err
		}
	}
	_, _ = database.ExecContext(ctx, `
		UPDATE "gocms_category"
		SET "site_id" = 1
		WHERE "site_id" <= 0 OR "site_id" IS NULL`)
	if _, err := database.ExecContext(ctx, `
		UPDATE "gocms_category"
		SET "page_type" = 'list'
		WHERE TRIM(COALESCE("page_type", '')) = ''`); err != nil {
		return fmt.Errorf("initialize category page types: %w", err)
	}
	if _, err := database.ExecContext(ctx, `
		UPDATE "gocms_category"
		SET "nav_position" = 'main'
		WHERE TRIM(COALESCE("nav_position", '')) = ''`); err != nil {
		return fmt.Errorf("initialize category nav positions: %w", err)
	}

	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS "`+CategoryTranslationTable+`" (
			"id" INTEGER PRIMARY KEY AUTOINCREMENT,
			"category_id" INTEGER NOT NULL,
			"lang" TEXT NOT NULL,
			"name" TEXT NOT NULL DEFAULT '',
			"keywords" TEXT NOT NULL DEFAULT '',
			"description" TEXT NOT NULL DEFAULT '',
			"cover_content" TEXT NOT NULL DEFAULT '',
			"link_url" TEXT NOT NULL DEFAULT '',
			UNIQUE("category_id", "lang")
		)`); err != nil {
		return fmt.Errorf("create category translation table: %w", err)
	}
	if _, err := database.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_gocms_category_trans ON "`+CategoryTranslationTable+`" ("category_id", "lang")`); err != nil {
		return fmt.Errorf("create category translation index: %w", err)
	}

	for name, definition := range map[string]string{
		"link_url": "TEXT NOT NULL DEFAULT ''",
	} {
		if err := ensureTableColumn(ctx, database, CategoryTranslationTable, name, definition); err != nil {
			return err
		}
	}

	_, _ = database.ExecContext(ctx, `
		INSERT OR IGNORE INTO "`+CategoryTranslationTable+`" ("category_id", "lang", "name", "keywords", "description", "cover_content", "link_url")
		SELECT "id", 'zh-CN', "name", "keywords", "description", "cover_content", "link_url"
		FROM "`+unifiedCategoryTable+`"
	`)

	return nil
}

func ensureTableColumn(ctx context.Context, database *sql.DB, tableName, name, definition string) error {
	var exists int
	if err := database.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name = ?`, tableName), name).Scan(&exists); err != nil {
		return fmt.Errorf("inspect %s %s column: %w", tableName, name, err)
	}
	if exists == 0 {
		if _, err := database.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE "%s" ADD COLUMN "%s" %s`, tableName, name, definition)); err != nil {
			return fmt.Errorf("add %s %s column: %w", tableName, name, err)
		}
	}
	return nil
}
