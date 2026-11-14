package db

import (
	"context"
	"database/sql"
)

// EnsureRedirects keeps administrator-managed public URL redirects outside themes.
func EnsureRedirects(ctx context.Context, database *sql.DB) error {
	_, err := database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS gocms_redirect (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id INTEGER NOT NULL REFERENCES gocms_site(id) ON DELETE CASCADE,
		source_path TEXT NOT NULL,
		target_url TEXT NOT NULL,
		status_code INTEGER NOT NULL DEFAULT 301 CHECK(status_code IN (301,302)),
		enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)),
		preserve_query INTEGER NOT NULL DEFAULT 1 CHECK(preserve_query IN (0,1)),
		note TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(site_id, source_path)
	)`)
	return err
}
