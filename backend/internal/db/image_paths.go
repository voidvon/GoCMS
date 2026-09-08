package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

var legacyImagePathReplacements = [][2]string{
	{"http://www.bilvie.com/UploadFile/produppic/", "/images/"},
	{"https://www.bilvie.com/UploadFile/produppic/", "/images/"},
	{"http://www.bilvie.com/uploadfile/produppic/", "/images/"},
	{"https://www.bilvie.com/uploadfile/produppic/", "/images/"},
	{"/UploadFile/produppic/", "/images/"},
	{"/uploadfile/produppic/", "/images/"},
	{`\UploadFile\produppic\`, "/images/"},
	{`\uploadfile\produppic\`, "/images/"},
	{"UploadFile/produppic/", "/images/"},
	{"uploadfile/produppic/", "/images/"},
	{"http://www.bilvie.com/UploadFile/", "/images/"},
	{"https://www.bilvie.com/UploadFile/", "/images/"},
	{"http://www.bilvie.com/uploadfile/", "/images/"},
	{"https://www.bilvie.com/uploadfile/", "/images/"},
	{"/UploadFile/", "/images/"},
	{"/uploadfile/", "/images/"},
	{`\UploadFile\`, "/images/"},
	{`\uploadfile\`, "/images/"},
	{"UploadFile/", "/images/"},
	{"uploadfile/", "/images/"},
	{"/produppic/", "/images/"},
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
			expression := quotedColumn
			for _, replacement := range legacyImagePathReplacements {
				expression = fmt.Sprintf("replace(%s, %s, %s)", expression, quoteSQLString(replacement[0]), quoteSQLString(replacement[1]))
			}
			query := `UPDATE ` + quoteIdentifier(table) + ` SET ` + quotedColumn + ` = ` + expression +
				` WHERE instr(lower(` + quotedColumn + `), 'uploadfile') > 0 OR instr(lower(` + quotedColumn + `), 'produppic') > 0`
			if _, err := tx.ExecContext(ctx, query); err != nil {
				return fmt.Errorf("normalize %s.%s: %w", table, column, err)
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE "benming_ch_prod" SET "smallpic" = CASE WHEN lower(trim("smallpic")) IN ('/skin/dfpic.gif', 'skin/dfpic.gif') THEN '' ELSE "smallpic" END, "bigpic" = CASE WHEN lower(trim("bigpic")) IN ('/skin/dfpic.gif', 'skin/dfpic.gif') THEN '' ELSE "bigpic" END`); err != nil {
		return fmt.Errorf("normalize default product image: %w", err)
	}
	return tx.Commit()
}

func quoteSQLString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
