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
	return nil
}
