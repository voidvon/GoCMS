package db

import (
	"context"
	"database/sql"
	"fmt"
)

const unifiedCategoryTable = "gocms_category"

// EnsureUnifiedCategories creates the category model used by the CMS. A
// category owns its route and template bindings, which keeps presentation
// decisions in database configuration and themes.
func EnsureUnifiedCategories(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS "`+unifiedCategoryTable+`" (
			"id" INTEGER PRIMARY KEY AUTOINCREMENT,
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
			"model_id" INTEGER NOT NULL DEFAULT 1
		)`); err != nil {
		return fmt.Errorf("create category table: %w", err)
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_gocms_category_parent ON "gocms_category" ("parent_id", "order_id", "id")`,
		`CREATE INDEX IF NOT EXISTS idx_gocms_category_model ON "gocms_category" ("model_id", "id")`,
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create category index: %w", err)
		}
	}
	for name, definition := range map[string]string{
		"list_page_size": "INTEGER NOT NULL DEFAULT 14",
		"page_type":      "TEXT NOT NULL DEFAULT 'list'",
		"cover_template": "TEXT NOT NULL DEFAULT ''",
		"keywords":       "TEXT NOT NULL DEFAULT ''",
		"description":    "TEXT NOT NULL DEFAULT ''",
		"cover_content":  "TEXT NOT NULL DEFAULT ''",
		"model_id":       "INTEGER NOT NULL DEFAULT 1",
	} {
		if err := ensureCategoryColumn(ctx, database, name, definition); err != nil {
			return err
		}
	}
	if _, err := database.ExecContext(ctx, `
		UPDATE "gocms_category"
		SET "page_type" = 'list'
		WHERE TRIM(COALESCE("page_type", '')) = ''`); err != nil {
		return fmt.Errorf("initialize category page types: %w", err)
	}
	return nil
}

func ensureCategoryColumn(ctx context.Context, database *sql.DB, name, definition string) error {
	var exists int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('gocms_category') WHERE name = ?`, name).Scan(&exists); err != nil {
		return fmt.Errorf("inspect category %s column: %w", name, err)
	}
	if exists == 0 {
		if _, err := database.ExecContext(ctx, `ALTER TABLE "gocms_category" ADD COLUMN "`+name+`" `+definition); err != nil {
			return fmt.Errorf("add category %s column: %w", name, err)
		}
	}
	return nil
}
