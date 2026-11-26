package db

import (
	"context"
	"database/sql"
)

func EnsureAdminSettings(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS gocms_admin_setting (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		admin_name TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		return err
	}
	legacy, err := tableHasColumn(ctx, database, SiteTable, "admin_name")
	if err != nil {
		return err
	}
	if legacy {
		// Use one existing name when upgrading an installation that stored names per site.
		if _, err := database.ExecContext(ctx, `INSERT INTO gocms_admin_setting (id, admin_name)
			SELECT 1, TRIM(admin_name) FROM gocms_site WHERE TRIM(admin_name) <> ''
			ORDER BY is_default DESC, id LIMIT 1 ON CONFLICT(id) DO NOTHING`); err != nil {
			return err
		}
	}
	_, err = database.ExecContext(ctx, `INSERT INTO gocms_admin_setting (id) VALUES (1) ON CONFLICT(id) DO NOTHING`)
	return err
}

func LoadAdminName(ctx context.Context, database *sql.DB) (string, error) {
	var name string
	err := database.QueryRowContext(ctx, `SELECT admin_name FROM gocms_admin_setting WHERE id = 1`).Scan(&name)
	return name, err
}

func SaveAdminName(ctx context.Context, database *sql.DB, name string) error {
	_, err := database.ExecContext(ctx, `INSERT INTO gocms_admin_setting (id, admin_name) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET admin_name = excluded.admin_name`, name)
	return err
}
