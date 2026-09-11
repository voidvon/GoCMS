package db

import (
	"context"
	"database/sql"
	"fmt"
)

const LanguageTable = "gocms_language"

type LanguageItem struct {
	ID         int64  `json:"id"`
	Code       string `json:"code"`
	Name       string `json:"name"`
	IsDefault  int    `json:"is_default"`
	IsFallback int    `json:"is_fallback"`
	IsEnabled  int    `json:"is_enabled"`
	SortOrder  int    `json:"sort_order"`
	PathPrefix string `json:"path_prefix"`
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
			"path_prefix" TEXT NOT NULL DEFAULT ''
		)`); err != nil {
		return fmt.Errorf("create language table: %w", err)
	}

	for name, definition := range map[string]string{
		"is_default":  "INTEGER NOT NULL DEFAULT 0",
		"is_fallback": "INTEGER NOT NULL DEFAULT 0",
		"is_enabled":  "INTEGER NOT NULL DEFAULT 1",
		"sort_order":  "INTEGER NOT NULL DEFAULT 0",
		"path_prefix": "TEXT NOT NULL DEFAULT ''",
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
			INSERT INTO "`+LanguageTable+`" ("code", "name", "is_default", "is_fallback", "is_enabled", "sort_order", "path_prefix")
			VALUES ('zh-CN', '简体中文', 1, 1, 1, 10, '')`); err != nil {
			return fmt.Errorf("seed default language: %w", err)
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
