package db

import (
	"context"
	"database/sql"
	"fmt"
)

const adminUsersTable = "gocms_admin_user"

func EnsureAdminUsers(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS "`+adminUsersTable+`" (
			"id" INTEGER PRIMARY KEY AUTOINCREMENT,
			"username" TEXT NOT NULL UNIQUE,
			"password_hash" TEXT NOT NULL DEFAULT '',
			"flags" TEXT NOT NULL DEFAULT '',
			"last_login" TEXT,
			"last_login_ip" TEXT
		)`); err != nil {
		return fmt.Errorf("create admin users table: %w", err)
	}
	// Upgrade existing administrators once, preserving their previous full access.
	// New accounts default to no privileges and must be assigned explicitly.
	for _, column := range []struct{ name, definition string }{
		{"group_id", "INTEGER NOT NULL DEFAULT 0"},
		{"category_ids", "TEXT NOT NULL DEFAULT 'null'"},
		{"disabled", "INTEGER NOT NULL DEFAULT 0"},
		{"is_super", "INTEGER NOT NULL DEFAULT 0"},
	} {
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		var count int
		err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('gocms_admin_user') WHERE name = ?`, column.name).Scan(&count)
		if err == nil && count == 0 {
			_, err = tx.ExecContext(ctx, `ALTER TABLE "gocms_admin_user" ADD COLUMN "`+column.name+`" `+column.definition)
			if err == nil && column.name == "is_super" {
				_, err = tx.ExecContext(ctx, `UPDATE "gocms_admin_user" SET "is_super" = 1`)
			}
		}
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("upgrade admin users: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	if _, err := database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS "gocms_admin_group" (
		"id" INTEGER PRIMARY KEY AUTOINCREMENT,
		"name" TEXT NOT NULL UNIQUE,
		"permissions" TEXT NOT NULL DEFAULT '[]'
	)`); err != nil {
		return err
	}
	return nil
}
