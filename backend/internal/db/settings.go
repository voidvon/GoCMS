package db

import (
	"context"
	"database/sql"
	"fmt"
)

const siteSettingsTable = "gocms_site_setting"

// EnsureSiteSettings creates and migrates the key/value store used for site-wide
// settings, scoped by site_id.
func EnsureSiteSettings(ctx context.Context, database *sql.DB) error {
	if !tableExists(ctx, database, siteSettingsTable) {
		if _, err := database.ExecContext(ctx, `
			CREATE TABLE IF NOT EXISTS "`+siteSettingsTable+`" (
				"site_id" INTEGER NOT NULL DEFAULT 1,
				"key" TEXT NOT NULL,
				"value" TEXT NOT NULL DEFAULT '',
				"updated_at" TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				PRIMARY KEY ("site_id", "key")
			)`); err != nil {
			return fmt.Errorf("create site settings table: %w", err)
		}
	} else {
		var hasSiteID int
		if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('`+siteSettingsTable+`') WHERE name = 'site_id'`).Scan(&hasSiteID); err != nil {
			return fmt.Errorf("inspect site settings table: %w", err)
		}
		if hasSiteID == 0 {
			tx, err := database.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()

			if _, err := tx.ExecContext(ctx, `
				CREATE TABLE "gocms_site_setting_v2" (
					"site_id" INTEGER NOT NULL DEFAULT 1,
					"key" TEXT NOT NULL,
					"value" TEXT NOT NULL DEFAULT '',
					"updated_at" TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
					PRIMARY KEY ("site_id", "key")
				)`); err != nil {
				return fmt.Errorf("create migration settings table: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT OR REPLACE INTO "gocms_site_setting_v2" ("site_id", "key", "value", "updated_at")
				SELECT 1, "key", "value", "updated_at" FROM "`+siteSettingsTable+`"`); err != nil {
				return fmt.Errorf("migrate existing settings: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `DROP TABLE "`+siteSettingsTable+`"`); err != nil {
				return fmt.Errorf("drop legacy settings table: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `ALTER TABLE "gocms_site_setting_v2" RENAME TO "`+siteSettingsTable+`"`); err != nil {
				return fmt.Errorf("rename settings table: %w", err)
			}
			if err := tx.Commit(); err != nil {
				return err
			}
		}
	}

	if _, err := database.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_gocms_site_setting_site ON "`+siteSettingsTable+`" ("site_id")`); err != nil {
		return fmt.Errorf("create site setting index: %w", err)
	}

	return nil
}

type settingsQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func LoadSiteSettings(ctx context.Context, query settingsQueryer) (map[string]string, error) {
	return LoadSiteSettingsForSite(ctx, query, 1)
}

func LoadSiteSettingsForSite(ctx context.Context, query settingsQueryer, siteID int64) (map[string]string, error) {
	if siteID <= 0 {
		siteID = 1
	}
	rows, err := query.QueryContext(ctx, `SELECT "key", "value" FROM "`+siteSettingsTable+`" WHERE "site_id" = ?`, siteID)
	if err != nil {
		return nil, fmt.Errorf("read site settings for site %d: %w", siteID, err)
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

func SaveSiteSettings(ctx context.Context, database *sql.DB, settings map[string]string) error {
	return SaveSiteSettingsForSite(ctx, database, 1, settings)
}

func SaveSiteSettingsForSite(ctx context.Context, database *sql.DB, siteID int64, settings map[string]string) error {
	if siteID <= 0 {
		siteID = 1
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO "`+siteSettingsTable+`" ("site_id", "key", "value", "updated_at")
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT("site_id", "key") DO UPDATE SET "value" = excluded.value, "updated_at" = CURRENT_TIMESTAMP`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for k, v := range settings {
		if _, err := stmt.ExecContext(ctx, siteID, k, v); err != nil {
			return fmt.Errorf("save site setting %s: %w", k, err)
		}
	}

	return tx.Commit()
}
