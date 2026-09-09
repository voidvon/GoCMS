package db

import (
	"context"
	"database/sql"
	"fmt"
)

const unifiedMessageTable = "gocms_message"

// EnsureMessages creates the runtime message model. Historical message rows
// are imported once by MigrateLegacyMessages and are not read by the server.
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

// MigrateLegacyMessages is used only by the one-time Access import.
func MigrateLegacyMessages(ctx context.Context, database *sql.DB) error {
	if err := EnsureMessages(ctx, database); err != nil {
		return err
	}
	_, err := database.ExecContext(ctx, `
		INSERT OR IGNORE INTO "gocms_message"
		("id", "title", "name", "phone", "mobile", "fax", "email", "address", "content", "created_at", "state", "content_id")
		SELECT "id", COALESCE("Title", ''), COALESCE("linkren", ''), COALESCE("phone", ''),
		       COALESCE("mobile", ''), COALESCE("fax", ''), COALESCE("email", ''), COALESCE("address", ''),
		       COALESCE("content", ''), COALESCE("date", ''), COALESCE("state", 0), COALESCE("prodid", 0)
		FROM "benming_ch_Msg"`)
	if err != nil {
		return fmt.Errorf("migrate legacy messages: %w", err)
	}
	return nil
}
