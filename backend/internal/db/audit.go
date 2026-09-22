package db

import (
	"context"
	"database/sql"
)

func EnsureAuditLog(ctx context.Context, database *sql.DB) error {
	_, err := database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS gocms_admin_operation (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id INTEGER NOT NULL DEFAULT 1,
		username TEXT NOT NULL,
		method TEXT NOT NULL,
		path TEXT NOT NULL,
		status INTEGER NOT NULL,
		ip TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS gocms_admin_login (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL,
		success INTEGER NOT NULL,
		ip TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_admin_operation_username ON gocms_admin_operation(username, id)`)
	if err != nil {
		return err
	}
	var hasSiteID int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('gocms_admin_operation') WHERE name = 'site_id'`).Scan(&hasSiteID); err == nil && hasSiteID == 0 {
		if _, err := database.ExecContext(ctx, `ALTER TABLE gocms_admin_operation ADD COLUMN site_id INTEGER NOT NULL DEFAULT 1`); err != nil {
			return err
		}
	}
	_, err = database.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_admin_operation_site ON gocms_admin_operation(site_id, id)`)
	return err
}
