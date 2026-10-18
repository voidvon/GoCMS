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

// EnsureLanguages creates and migrates the language table.
func EnsureLanguages(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS "`+LanguageTable+`" (
			"id" INTEGER PRIMARY KEY AUTOINCREMENT,
			"code" TEXT NOT NULL UNIQUE,
			"name" TEXT NOT NULL,
			"is_default" INTEGER NOT NULL DEFAULT 0,
			"is_fallback" INTEGER NOT NULL DEFAULT 0,
			"is_enabled" INTEGER NOT NULL DEFAULT 1,
			"sort_order" INTEGER NOT NULL DEFAULT 0,
			"path_prefix" TEXT NOT NULL DEFAULT '',
			"site_mode" TEXT NOT NULL DEFAULT 'subdir',
			"domain" TEXT NOT NULL DEFAULT '',
			"output_dir" TEXT NOT NULL DEFAULT '',
			"is_admin_default" INTEGER NOT NULL DEFAULT 0
		)`); err != nil {
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

	// Ensure at least one default and fallback language
	var count int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM "`+LanguageTable+`"`).Scan(&count); err != nil {
		return fmt.Errorf("check languages count: %w", err)
	}
	if count == 0 {
		if _, err := database.ExecContext(ctx, `
			INSERT INTO "`+LanguageTable+`" ("code", "name", "is_default", "is_fallback", "is_enabled", "sort_order", "path_prefix", "is_admin_default")
			VALUES ('zh-CN', '简体中文', 1, 1, 1, 10, '', 1)`); err != nil {
			return fmt.Errorf("seed default language: %w", err)
		}
	}

	// Older installs merged the primary site language with the language the
	// backend displays. Backfill the admin display language from the primary
	// site language so existing behaviour is preserved until it is changed.
	var adminDefaultCount int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM "`+LanguageTable+`" WHERE "is_admin_default" = 1`).Scan(&adminDefaultCount); err != nil {
		return fmt.Errorf("check admin default language: %w", err)
	}
	if adminDefaultCount == 0 {
		if _, err := database.ExecContext(ctx, `
			UPDATE "`+LanguageTable+`" SET "is_admin_default" = 1
			WHERE "is_default" = 1 AND "id" = (SELECT MIN("id") FROM "`+LanguageTable+`" WHERE "is_default" = 1)`); err != nil {
			return fmt.Errorf("backfill admin default language: %w", err)
		}
	}

	return nil
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
