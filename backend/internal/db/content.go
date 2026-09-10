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
			"visible" INTEGER NOT NULL DEFAULT 1
		)`); err != nil {
		return fmt.Errorf("create content table: %w", err)
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_gocms_content_category ON "gocms_content" ("category_id", "sort_order", "id")`,
		`CREATE INDEX IF NOT EXISTS idx_gocms_content_visible ON "gocms_content" ("visible", "id")`,
		`CREATE INDEX IF NOT EXISTS idx_gocms_content_search ON "gocms_content" ("title", "code", "keywords")`,
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create content index: %w", err)
		}
	}
	return nil
}
