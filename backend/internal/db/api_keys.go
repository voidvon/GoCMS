package db

import (
	"context"
	"database/sql"
	"fmt"
)

const (
	ApiKeyTable      = "gocms_api_key"
	ApiKeyEventTable = "gocms_api_key_event"
)

func EnsureApiKeys(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS "`+ApiKeyTable+`" (
			"id" INTEGER PRIMARY KEY AUTOINCREMENT,
			"admin_id" INTEGER NOT NULL,
			"name" TEXT NOT NULL,
			"key_prefix" TEXT NOT NULL,
			"key_hash" TEXT NOT NULL UNIQUE,
			"expires_at" TEXT,
			"revoked_at" TEXT,
			"revoked_by_admin_id" INTEGER,
			"created_by_admin_id" INTEGER,
			"last_used_at" TEXT,
			"last_used_ip" TEXT NOT NULL DEFAULT '',
			"created_at" TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			"updated_at" TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY ("admin_id") REFERENCES "`+adminUsersTable+`"("id") ON DELETE CASCADE,
			FOREIGN KEY ("revoked_by_admin_id") REFERENCES "`+adminUsersTable+`"("id") ON DELETE SET NULL,
			FOREIGN KEY ("created_by_admin_id") REFERENCES "`+adminUsersTable+`"("id") ON DELETE SET NULL
		)`); err != nil {
		return fmt.Errorf("create api keys table: %w", err)
	}

	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS "`+ApiKeyEventTable+`" (
			"id" INTEGER PRIMARY KEY AUTOINCREMENT,
			"api_key_id" INTEGER,
			"actor_admin_id" INTEGER,
			"event_type" TEXT NOT NULL,
			"client_ip" TEXT NOT NULL DEFAULT '',
			"metadata_json" TEXT NOT NULL DEFAULT '{}',
			"created_at" TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY ("api_key_id") REFERENCES "`+ApiKeyTable+`"("id") ON DELETE SET NULL,
			FOREIGN KEY ("actor_admin_id") REFERENCES "`+adminUsersTable+`"("id") ON DELETE SET NULL
		)`); err != nil {
		return fmt.Errorf("create api key events table: %w", err)
	}

	indexes := []string{
		`CREATE INDEX IF NOT EXISTS "idx_gocms_api_key_admin_id" ON "` + ApiKeyTable + `" ("admin_id")`,
		`CREATE INDEX IF NOT EXISTS "idx_gocms_api_key_key_prefix" ON "` + ApiKeyTable + `" ("key_prefix")`,
		`CREATE INDEX IF NOT EXISTS "idx_gocms_api_key_revoked_at" ON "` + ApiKeyTable + `" ("revoked_at")`,
		`CREATE INDEX IF NOT EXISTS "idx_gocms_api_key_expires_at" ON "` + ApiKeyTable + `" ("expires_at")`,
		`CREATE INDEX IF NOT EXISTS "idx_gocms_api_key_event_key_id" ON "` + ApiKeyEventTable + `" ("api_key_id", "id" DESC)`,
		`CREATE INDEX IF NOT EXISTS "idx_gocms_api_key_event_created_at" ON "` + ApiKeyEventTable + `" ("created_at" DESC, "id" DESC)`,
	}
	for _, idx := range indexes {
		if _, err := database.ExecContext(ctx, idx); err != nil {
			return fmt.Errorf("create api key index: %w", err)
		}
	}

	return nil
}
