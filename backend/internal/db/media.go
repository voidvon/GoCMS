package db

import (
	"context"
	"database/sql"
	"fmt"
)

const mediaTable = "gocms_media"

func EnsureMedia(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS "`+mediaTable+`" (
			"id" INTEGER PRIMARY KEY AUTOINCREMENT,
			"kind" TEXT NOT NULL DEFAULT 'image',
			"storage_path" TEXT NOT NULL UNIQUE,
			"public_path" TEXT NOT NULL UNIQUE,
			"original_name" TEXT NOT NULL DEFAULT '',
			"mime_type" TEXT NOT NULL DEFAULT '',
			"size_bytes" INTEGER NOT NULL DEFAULT 0,
			"width" INTEGER NOT NULL DEFAULT 0,
			"height" INTEGER NOT NULL DEFAULT 0,
			"sha256" TEXT NOT NULL DEFAULT '',
			"status" TEXT NOT NULL DEFAULT 'active',
			"uploaded_by" TEXT NOT NULL DEFAULT '',
			"draft_token" TEXT NOT NULL DEFAULT '',
			"created_at" TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		return fmt.Errorf("create media table: %w", err)
	}
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS "gocms_media_ref" (
			"id" INTEGER PRIMARY KEY AUTOINCREMENT,
			"media_id" INTEGER NOT NULL,
			"content_id" INTEGER NOT NULL,
			"field_name" TEXT NOT NULL,
			"created_at" TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY ("media_id") REFERENCES "gocms_media" ("id") ON DELETE CASCADE,
			FOREIGN KEY ("content_id") REFERENCES "gocms_content" ("id") ON DELETE CASCADE,
			UNIQUE ("media_id", "content_id", "field_name")
		)`); err != nil {
		return fmt.Errorf("create media reference table: %w", err)
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_gocms_media_kind_status ON "gocms_media" ("kind", "status", "id")`,
		`CREATE INDEX IF NOT EXISTS idx_gocms_media_created ON "gocms_media" ("created_at", "id")`,
		`CREATE INDEX IF NOT EXISTS idx_gocms_media_ref_content ON "gocms_media_ref" ("content_id", "field_name")`,
		`CREATE INDEX IF NOT EXISTS idx_gocms_media_ref_media ON "gocms_media_ref" ("media_id", "content_id")`,
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create media index: %w", err)
		}
	}
	return nil
}
