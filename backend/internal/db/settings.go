package db

import (
	"context"
	"database/sql"
	"fmt"
)

const siteSettingsTable = "gocms_site_setting"

// EnsureSiteSettings creates the generic key/value store used for site-wide
// settings. Themes decide which settings they display.
func EnsureSiteSettings(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS "`+siteSettingsTable+`" (
			"key" TEXT PRIMARY KEY,
			"value" TEXT NOT NULL DEFAULT '',
			"updated_at" TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		return fmt.Errorf("create site settings table: %w", err)
	}
	return nil
}

type settingsQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func LoadSiteSettings(ctx context.Context, query settingsQueryer) (map[string]string, error) {
	rows, err := query.QueryContext(ctx, `SELECT "key", "value" FROM "`+siteSettingsTable+`"`)
	if err != nil {
		return nil, fmt.Errorf("read site settings: %w", err)
	}
	defer rows.Close()
	settings := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("scan site setting: %w", err)
		}
		settings[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read site settings: %w", err)
	}
	return settings, nil
}
