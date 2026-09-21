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

func EnsureSiteUsers(ctx context.Context, database *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS gocms_user_event(id INTEGER PRIMARY KEY AUTOINCREMENT,user_id INTEGER,action TEXT NOT NULL,created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE INDEX IF NOT EXISTS idx_gocms_user_event_rate ON gocms_user_event(action,created_at)`,
		`CREATE TABLE IF NOT EXISTS gocms_user (id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL UNIQUE, email TEXT NOT NULL DEFAULT '', password_hash TEXT NOT NULL, display_name TEXT NOT NULL DEFAULT '', avatar_url TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'active', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, last_login_at TEXT, last_login_ip TEXT)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_gocms_user_email ON gocms_user(email) WHERE email <> ''`,
		`CREATE TABLE IF NOT EXISTS gocms_user_session (token_hash TEXT PRIMARY KEY, user_id INTEGER NOT NULL, expires_at INTEGER NOT NULL, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, ip TEXT NOT NULL DEFAULT '', user_agent TEXT NOT NULL DEFAULT '', FOREIGN KEY(user_id) REFERENCES gocms_user(id) ON DELETE CASCADE)`,
		`CREATE INDEX IF NOT EXISTS idx_gocms_user_session_expiry ON gocms_user_session(expires_at)`,
		`CREATE TABLE IF NOT EXISTS gocms_user_login (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER, identifier TEXT NOT NULL, success INTEGER NOT NULL DEFAULT 0, ip TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS gocms_user_group (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, slug TEXT NOT NULL UNIQUE, description TEXT NOT NULL DEFAULT '', sort_order INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'active')`,
		`CREATE TABLE IF NOT EXISTS gocms_user_group_member (user_id INTEGER NOT NULL, group_id INTEGER NOT NULL, started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, expires_at TEXT, status TEXT NOT NULL DEFAULT 'active', PRIMARY KEY(user_id, group_id), FOREIGN KEY(user_id) REFERENCES gocms_user(id) ON DELETE CASCADE, FOREIGN KEY(group_id) REFERENCES gocms_user_group(id) ON DELETE CASCADE)`,
		`CREATE INDEX IF NOT EXISTS idx_gocms_user_group_member_expiry ON gocms_user_group_member(user_id, expires_at)`,
	}
	for _, statement := range statements {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure site users: %w", err)
		}
	}
	var exists int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('gocms_user') WHERE name='max_sessions'").Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		if _, err := database.ExecContext(ctx, "ALTER TABLE gocms_user ADD COLUMN max_sessions INTEGER NOT NULL DEFAULT 2 CHECK(max_sessions BETWEEN 1 AND 100)"); err != nil {
			return err
		}
	}
	if _, err := database.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_user_session_owner ON gocms_user_session(user_id,expires_at)"); err != nil {
		return err
	}

	return nil
}
