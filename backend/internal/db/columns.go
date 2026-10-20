package db

import (
	"context"
	"database/sql"
	"fmt"
)

// tableHasColumn reports whether a table declares the given column.
func tableHasColumn(ctx context.Context, database *sql.DB, table, column string) (bool, error) {
	var count int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info('`+table+`') WHERE name = ?`, column).Scan(&count); err != nil {
		return false, fmt.Errorf("inspect %s.%s column: %w", table, column, err)
	}
	return count > 0, nil
}

// renameKeywordsToSEOTitle retires the obsolete keywords column in favor of
// seo_title. Values are kept as-is: imports from the previous CMS already
// stored the SEO title in keywords, and other imports simply lose a field that
// search engines no longer read.
func renameKeywordsToSEOTitle(ctx context.Context, database *sql.DB, table string) error {
	hasKeywords, err := tableHasColumn(ctx, database, table, "keywords")
	if err != nil || !hasKeywords {
		return err
	}
	hasSEOTitle, err := tableHasColumn(ctx, database, table, "seo_title")
	if err != nil || hasSEOTitle {
		// Both columns exist: keep seo_title and leave keywords for manual review.
		return err
	}
	if _, err := database.ExecContext(ctx,
		fmt.Sprintf(`ALTER TABLE "%s" RENAME COLUMN "keywords" TO "seo_title"`, table)); err != nil {
		return fmt.Errorf("rename %s.keywords: %w", table, err)
	}
	return nil
}

// ensureColumn adds a column when a table from an older release lacks it.
func ensureColumn(ctx context.Context, database *sql.DB, table, name, definition string) error {
	exists, err := tableHasColumn(ctx, database, table, name)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err := database.ExecContext(ctx,
		fmt.Sprintf(`ALTER TABLE "%s" ADD COLUMN "%s" %s`, table, name, definition)); err != nil {
		return fmt.Errorf("add %s.%s column: %w", table, name, err)
	}
	_, _ = database.ExecContext(ctx, fmt.Sprintf(`REINDEX "%s"`, table))
	return nil
}
