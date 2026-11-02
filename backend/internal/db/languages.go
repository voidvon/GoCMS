package db

import (
	"context"
	"database/sql"
	"fmt"
)

const LanguageTable = "gocms_language"

const (
	LanguageSiteModeSubdir     = "subdir"
	LanguageSiteModeStandalone = "standalone"
)

type LanguageItem struct {
	ID         int64  `json:"id"`
	Code       string `json:"code"`
	Name       string `json:"name"`
	IsDefault  int    `json:"is_default"`
	IsFallback int    `json:"is_fallback"`
	IsEnabled  int    `json:"is_enabled"`
	SortOrder  int    `json:"sort_order"`
	PathPrefix string `json:"path_prefix"`
	SiteMode   string `json:"site_mode"`
	Domain     string `json:"domain"`
	OutputDir  string `json:"output_dir"`
	// IsAdminDefault marks the language the admin backend shows by default in
	// category and content lists. It is independent from IsDefault, which marks
	// the primary site language generated at the site root.
	IsAdminDefault int `json:"is_admin_default"`
}

// languageTableSchema scopes language codes to their owning site.
func languageTableSchema(table string) string {
	return `CREATE TABLE IF NOT EXISTS "` + table + `" (
		"id" INTEGER PRIMARY KEY AUTOINCREMENT,
		"site_id" INTEGER NOT NULL DEFAULT 1,
		"code" TEXT NOT NULL,
		"name" TEXT NOT NULL,
		"is_default" INTEGER NOT NULL DEFAULT 0,
		"is_fallback" INTEGER NOT NULL DEFAULT 0,
		"is_enabled" INTEGER NOT NULL DEFAULT 1,
		"sort_order" INTEGER NOT NULL DEFAULT 0,
		"path_prefix" TEXT NOT NULL DEFAULT '',
		"site_mode" TEXT NOT NULL DEFAULT 'subdir',
		"domain" TEXT NOT NULL DEFAULT '',
		"output_dir" TEXT NOT NULL DEFAULT '',
		"is_admin_default" INTEGER NOT NULL DEFAULT 0,
		UNIQUE ("site_id", "code")
	)`
}

// EnsureLanguages migrates shared settings into independent copies for existing
// sites. Subsequent runs never copy settings between sites.
func EnsureLanguages(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, languageTableSchema(LanguageTable)); err != nil {
		return fmt.Errorf("create language table: %w", err)
	}
	for name, definition := range map[string]string{
		"is_default":       "INTEGER NOT NULL DEFAULT 0",
		"is_fallback":      "INTEGER NOT NULL DEFAULT 0",
		"is_enabled":       "INTEGER NOT NULL DEFAULT 1",
		"sort_order":       "INTEGER NOT NULL DEFAULT 0",
		"path_prefix":      "TEXT NOT NULL DEFAULT ''",
		"site_mode":        "TEXT NOT NULL DEFAULT 'subdir'",
		"domain":           "TEXT NOT NULL DEFAULT ''",
		"output_dir":       "TEXT NOT NULL DEFAULT ''",
		"is_admin_default": "INTEGER NOT NULL DEFAULT 0",
	} {
		if err := ensureLanguageColumn(ctx, database, name, definition); err != nil {
			return err
		}
	}
	var hasSiteID int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('`+LanguageTable+`') WHERE name = 'site_id'`).Scan(&hasSiteID); err != nil {
		return err
	}
	siteIDs := []int64{1}
	ownerID := int64(1)
	if tableExists(ctx, database, SiteTable) {
		sites, err := ListSites(ctx, database)
		if err != nil {
			return err
		}
		if len(sites) > 0 {
			siteIDs = nil
			ownerID = sites[0].ID
			for _, site := range sites {
				siteIDs = append(siteIDs, site.ID)
			}
		}
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if hasSiteID == 0 {
		const migrated = "gocms_language_scoped"
		const columns = `"code", "name", "is_default", "is_fallback", "is_enabled", "sort_order", "path_prefix", "site_mode", "domain", "output_dir", "is_admin_default"`
		const copiedColumns = `"code", "name", "is_default", "is_fallback", "is_enabled", "sort_order", "path_prefix", "site_mode", '', '', "is_admin_default"`
		if _, err := tx.ExecContext(ctx, languageTableSchema(migrated)); err != nil {
			return err
		}
		// Keep the original IDs on the default site, then snapshot the shared
		// configuration for every other existing site with new independent IDs.
		// Legacy standalone hosts belonged to the default site. Keep those hosts
		// and explicit output directories there, so copies cannot claim the same
		// domain or overwrite the same generated directory.
		if _, err := tx.ExecContext(ctx, `INSERT INTO "`+migrated+`" ("id", "site_id", `+columns+`) SELECT "id", ?, `+columns+` FROM "`+LanguageTable+`"`, ownerID); err != nil {
			return fmt.Errorf("migrate languages: %w", err)
		}
		for _, siteID := range siteIDs {
			if siteID == ownerID {
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO "`+migrated+`" ("site_id", `+columns+`) SELECT ?, `+copiedColumns+` FROM "`+LanguageTable+`"`, siteID); err != nil {
				return fmt.Errorf("snapshot site languages: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, `DROP TABLE "`+LanguageTable+`"`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `ALTER TABLE "`+migrated+`" RENAME TO "`+LanguageTable+`"`); err != nil {
			return err
		}
	}
	for _, siteID := range siteIDs {
		if err := seedSiteLanguage(ctx, tx, siteID); err != nil {
			return err
		}
	}
	// Backfill independently; a configured admin language on another site must
	// not prevent this site's primary language from becoming its admin default.
	if _, err := tx.ExecContext(ctx, `UPDATE "`+LanguageTable+`" AS l SET "is_admin_default" = 1
		WHERE l."is_default" = 1 AND l."id" = (
			SELECT MIN(d."id") FROM "`+LanguageTable+`" AS d WHERE d."site_id" = l."site_id" AND d."is_default" = 1)
		AND NOT EXISTS (SELECT 1 FROM "`+LanguageTable+`" AS a WHERE a."site_id" = l."site_id" AND a."is_admin_default" = 1)`); err != nil {
		return fmt.Errorf("backfill admin default language: %w", err)
	}
	return tx.Commit()
}

func seedSiteLanguage(ctx context.Context, tx *sql.Tx, siteID int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO "`+LanguageTable+`"
		("site_id", "code", "name", "is_default", "is_fallback", "is_enabled", "sort_order", "is_admin_default")
		SELECT ?, 'zh-CN', '简体中文', 1, 1, 1, 10, 1
		WHERE NOT EXISTS (SELECT 1 FROM "`+LanguageTable+`" WHERE "site_id" = ?)`, siteID, siteID)
	return err
}

func ensureLanguageColumn(ctx context.Context, database *sql.DB, name, definition string) error {
	var exists int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('`+LanguageTable+`') WHERE name = ?`, name).Scan(&exists); err != nil {
		return fmt.Errorf("inspect language %s column: %w", name, err)
	}
	if exists == 0 {
		if _, err := database.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE "%s" ADD COLUMN "%s" %s`, LanguageTable, name, definition)); err != nil {
			return fmt.Errorf("add language column %s: %w", name, err)
		}
	}
	return nil
}
