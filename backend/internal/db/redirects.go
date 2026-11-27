package db

import (
	"context"
	"database/sql"
)

// EnsureRedirects keeps administrator-managed public URL redirects outside themes.
func EnsureRedirects(ctx context.Context, database *sql.DB) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, redirectTableSchema("gocms_redirect")); err != nil {
		return err
	}
	var hasMatchType bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pragma_table_info('gocms_redirect') WHERE name = 'match_type')`).Scan(&hasMatchType); err != nil {
		return err
	}
	if !hasMatchType {
		if _, err := tx.ExecContext(ctx, redirectTableSchema("gocms_redirect_new")); err != nil {
			return err
		}
		// Preserve IDs and the sequence even when the last-issued ID was deleted.
		const columns = `id, site_id, source_path, target_url, status_code, enabled, preserve_query, note, created_at, updated_at`
		if _, err := tx.ExecContext(ctx, `INSERT INTO sqlite_sequence (name, seq)
			SELECT 'gocms_redirect_new', seq FROM sqlite_sequence WHERE name = 'gocms_redirect';
			INSERT INTO gocms_redirect_new (`+columns+`) SELECT `+columns+` FROM gocms_redirect;
			DROP TABLE gocms_redirect;
			ALTER TABLE gocms_redirect_new RENAME TO gocms_redirect`); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func redirectTableSchema(table string) string {
	return `CREATE TABLE IF NOT EXISTS ` + table + ` (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id INTEGER NOT NULL REFERENCES gocms_site(id) ON DELETE CASCADE,
		match_type TEXT NOT NULL DEFAULT 'exact' CHECK(match_type IN ('exact','regex')),
		source_path TEXT NOT NULL,
		target_url TEXT NOT NULL,
		status_code INTEGER NOT NULL DEFAULT 301 CHECK(status_code IN (301,302)),
		enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)),
		preserve_query INTEGER NOT NULL DEFAULT 1 CHECK(preserve_query IN (0,1)),
		note TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(site_id, match_type, source_path)
	)`
}
