package db

import (
	"context"
	"database/sql"
	"fmt"
)

const unifiedMessageTable = "gocms_message"

// EnsureMessages creates the message model used by the public form and admin.
func EnsureMessages(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS "`+unifiedMessageTable+`" (
			"id" INTEGER PRIMARY KEY AUTOINCREMENT,
			"title" TEXT NOT NULL DEFAULT '',
			"name" TEXT NOT NULL DEFAULT '',
			"phone" TEXT NOT NULL DEFAULT '',
			"mobile" TEXT NOT NULL DEFAULT '',
			"fax" TEXT NOT NULL DEFAULT '',
			"email" TEXT NOT NULL DEFAULT '',
			"address" TEXT NOT NULL DEFAULT '',
			"content" TEXT NOT NULL DEFAULT '',
			"created_at" TEXT NOT NULL DEFAULT '',
			"state" INTEGER NOT NULL DEFAULT 0,
			"content_id" INTEGER NOT NULL DEFAULT 0
		)`); err != nil {
		return fmt.Errorf("create unified message table: %w", err)
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_gocms_message_state ON "gocms_message" ("state", "id")`,
		`CREATE INDEX IF NOT EXISTS idx_gocms_message_content ON "gocms_message" ("content_id", "id")`,
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create unified message index: %w", err)
		}
	}
	return nil
}
