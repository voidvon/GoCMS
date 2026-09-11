package db

import (
	"context"
	"database/sql"
	"fmt"
)

const unifiedContentTable = "gocms_content"

func EnsureContent(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS "`+unifiedContentTable+`" (
			"id" INTEGER PRIMARY KEY AUTOINCREMENT,
			"category_id" INTEGER NOT NULL DEFAULT 0,
			"route_key" TEXT NOT NULL DEFAULT '',
			"title" TEXT NOT NULL DEFAULT '',
			"code" TEXT NOT NULL DEFAULT '',
			"summary" TEXT NOT NULL DEFAULT '',
			"body" TEXT NOT NULL DEFAULT '',
			"cover_image" TEXT NOT NULL DEFAULT '',
			"published_at" TEXT NOT NULL DEFAULT '',
			"source" TEXT NOT NULL DEFAULT '',
			"keywords" TEXT NOT NULL DEFAULT '',
			"description" TEXT NOT NULL DEFAULT '',
			"sort_order" INTEGER NOT NULL DEFAULT 0,
			"featured" INTEGER NOT NULL DEFAULT 0,
			"visible" INTEGER NOT NULL DEFAULT 1,
			"model_id" INTEGER NOT NULL DEFAULT 1,
			"extra_data" TEXT NOT NULL DEFAULT '{}'
		)`); err != nil {
		return fmt.Errorf("create content table: %w", err)
	}
	for name, definition := range map[string]string{
		"model_id":   "INTEGER NOT NULL DEFAULT 1",
		"extra_data": "TEXT NOT NULL DEFAULT '{}'",
	} {
		if err := ensureContentColumn(ctx, database, name, definition); err != nil {
			return err
		}
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_gocms_content_category ON "gocms_content" ("category_id", "sort_order", "id")`,
		`CREATE INDEX IF NOT EXISTS idx_gocms_content_model ON "gocms_content" ("model_id", "id")`,
		`CREATE INDEX IF NOT EXISTS idx_gocms_content_visible ON "gocms_content" ("visible", "id")`,
		`CREATE INDEX IF NOT EXISTS idx_gocms_content_search ON "gocms_content" ("title", "code", "keywords")`,
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create content index: %w", err)
		}
	}
	if err := MigrateCategoryAndContentModels(ctx, database); err != nil {
		return err
	}
	return nil
}

func ensureContentColumn(ctx context.Context, database *sql.DB, name, definition string) error {
	var exists int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('`+unifiedContentTable+`') WHERE name = ?`, name).Scan(&exists); err != nil {
		return fmt.Errorf("inspect content %s column: %w", name, err)
	}
	if exists == 0 {
		if _, err := database.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE "%s" ADD COLUMN "%s" %s`, unifiedContentTable, name, definition)); err != nil {
			return fmt.Errorf("add content column %s: %w", name, err)
		}
		_, _ = database.ExecContext(ctx, fmt.Sprintf(`REINDEX "%s"`, unifiedContentTable))
	}
	return nil
}
