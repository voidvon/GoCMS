package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
)

var legacyImageToken = regexp.MustCompile(`(?i)(?:https?://[^"'\s<>\)]+|//[^"'\s<>\)]+|(?:[a-z]:)?[\\/]*(?:uploadfile|produppic)[^"'\s<>\)]+)`)

func normalizeImageText(value string) string {
	return legacyImageToken.ReplaceAllStringFunc(value, normalizeImageToken)
}

func normalizeImageToken(value string) string {
	normalized := strings.ReplaceAll(value, `\`, "/")
	u, err := url.Parse(normalized)
	if err != nil || u.Path == "" {
		return value
	}
	if u.Host != "" && !strings.EqualFold(u.Hostname(), "www.bilvie.com") {
		return value
	}
	trimmed := strings.TrimPrefix(u.Path, "/")
	lower := strings.ToLower(trimmed)
	if !strings.HasPrefix(lower, "uploadfile/") && !strings.HasPrefix(lower, "produppic/") {
		return value
	}
	filename := path.Base(trimmed)
	if filename == "." || filename == "/" || filename == "" {
		return value
	}
	u.Scheme, u.Host, u.Opaque = "", "", ""
	u.User = nil
	u.RawPath = ""
	u.Path = "/images/" + filename
	return u.String()
}

// NormalizeImagePaths migrates legacy upload URLs in every TEXT column to the
// single public image namespace. It is safe to run repeatedly.
func NormalizeImagePaths(ctx context.Context, database *sql.DB) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	tables, err := tx.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return err
	}
	var tableNames []string
	for tables.Next() {
		var name string
		if err := tables.Scan(&name); err != nil {
			tables.Close()
			return err
		}
		tableNames = append(tableNames, name)
	}
	if err := tables.Err(); err != nil {
		tables.Close()
		return err
	}
	if err := tables.Close(); err != nil {
		return err
	}

	for _, table := range tableNames {
		columns, err := tx.QueryContext(ctx, `PRAGMA table_info(`+quoteIdentifier(table)+`)`)
		if err != nil {
			return fmt.Errorf("inspect table %s: %w", table, err)
		}
		var textColumns []string
		for columns.Next() {
			var cid, notNull, primaryKey int
			var name, columnType string
			var defaultValue sql.NullString
			if err := columns.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
				columns.Close()
				return fmt.Errorf("inspect table %s: %w", table, err)
			}
			if strings.EqualFold(columnType, "TEXT") {
				textColumns = append(textColumns, name)
			}
		}
		if err := columns.Err(); err != nil {
			columns.Close()
			return fmt.Errorf("inspect table %s: %w", table, err)
		}
		if err := columns.Close(); err != nil {
			return err
		}

		for _, column := range textColumns {
			quotedColumn := quoteIdentifier(column)
			query := `SELECT rowid, ` + quotedColumn + ` FROM ` + quoteIdentifier(table) +
				` WHERE typeof(` + quotedColumn + `) = 'text' AND (instr(lower(` + quotedColumn + `), 'uploadfile') > 0 OR instr(lower(` + quotedColumn + `), 'produppic') > 0)`
			rows, err := tx.QueryContext(ctx, query)
			if err != nil {
				return fmt.Errorf("inspect %s.%s values: %w", table, column, err)
			}
			updates := make([]struct {
				rowID int64
				value string
			}, 0)
			for rows.Next() {
				var rowID int64
				var value string
				if err := rows.Scan(&rowID, &value); err != nil {
					rows.Close()
					return fmt.Errorf("read %s.%s value: %w", table, column, err)
				}
				normalized := normalizeImageText(value)
				if normalized != value {
					updates = append(updates, struct {
						rowID int64
						value string
					}{rowID: rowID, value: normalized})
				}
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return fmt.Errorf("read %s.%s values: %w", table, column, err)
			}
			if err := rows.Close(); err != nil {
				return fmt.Errorf("close %s.%s values: %w", table, column, err)
			}
			for _, update := range updates {
				if _, err := tx.ExecContext(ctx, `UPDATE `+quoteIdentifier(table)+` SET `+quotedColumn+` = ? WHERE rowid = ?`, update.value, update.rowID); err != nil {
					return fmt.Errorf("normalize %s.%s: %w", table, column, err)
				}
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE "benming_ch_prod" SET "smallpic" = CASE WHEN lower(trim("smallpic")) IN ('/skin/dfpic.gif', 'skin/dfpic.gif') THEN '' ELSE "smallpic" END, "bigpic" = CASE WHEN lower(trim("bigpic")) IN ('/skin/dfpic.gif', 'skin/dfpic.gif') THEN '' ELSE "bigpic" END`); err != nil {
		return fmt.Errorf("normalize default content image: %w", err)
	}
	return tx.Commit()
}
