package db

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

const unifiedContentTable = "bilvie_content"

type legacyContentRecord struct {
	RouteID     int64
	CategoryID  int64
	Title       string
	Code        string
	Summary     string
	Body        string
	Image       string
	PublishedAt string
	Source      string
	Keywords    string
	Description string
	OrderID     int64
	Featured    int64
	Visible     int64
}

type legacyContentSource struct {
	table string
	query string
	scan  func(*sql.Rows) (legacyContentRecord, error)
}

// Each adapter maps one historical table into the same runtime content
// record. The publisher never uses these source-specific queries.
var legacyContentSources = []legacyContentSource{
	{
		table: "benming_ch_prod",
		query: `SELECT "id", COALESCE("CatId", 0), COALESCE("prodName", ''), COALESCE("prodCode", ''),
			COALESCE("remark", ''), COALESCE("itemize", ''), COALESCE("smallpic", ''),
			COALESCE("bigpic", ''), COALESCE("key", ''), COALESCE("orderid", 0),
			COALESCE("tjhome", 0), COALESCE("show", 0)
			FROM "benming_ch_prod"`,
		scan: scanLegacyCatalogContent,
	},
	{
		table: "benming_ch_news",
		query: `SELECT "newsid", COALESCE("Typeid", 0), COALESCE("Title", ''), COALESCE("Content", ''),
			COALESCE("Picture", ''), COALESCE("Dateandtime", ''), COALESCE("Nfrom", ''),
			COALESCE("key", ''), COALESCE("desc", ''), COALESCE("tjhome", 0)
			FROM "benming_ch_news"`,
		scan: scanLegacyArticleContent,
	},
}

// EnsureContent creates the runtime content table. Legacy source tables are
// imported only by the explicit migration command below.
func EnsureContent(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS "`+unifiedContentTable+`" (
			"id" INTEGER PRIMARY KEY AUTOINCREMENT,
			"category_id" INTEGER NOT NULL DEFAULT 0,
			"route_key" TEXT NOT NULL,
			"title" TEXT NOT NULL DEFAULT '',
			"code" TEXT NOT NULL DEFAULT '',
			"summary" TEXT NOT NULL DEFAULT '',
			"body" TEXT NOT NULL DEFAULT '',
			"cover_image" TEXT NOT NULL DEFAULT '',
			"published_at" TEXT NOT NULL DEFAULT '',
			"source" TEXT NOT NULL DEFAULT '',
			"keywords" TEXT NOT NULL DEFAULT '',
			"description" TEXT NOT NULL DEFAULT '',
			"sort_order" INTEGER NOT NULL DEFAULT 0,
			"featured" INTEGER NOT NULL DEFAULT 0,
			"visible" INTEGER NOT NULL DEFAULT 1,
			"source_table" TEXT,
			"source_id" INTEGER,
			UNIQUE ("source_table", "source_id")
		)`); err != nil {
		return fmt.Errorf("create unified content table: %w", err)
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_bilvie_content_category ON "bilvie_content" ("category_id", "sort_order", "id")`,
		`CREATE INDEX IF NOT EXISTS idx_bilvie_content_visible ON "bilvie_content" ("visible", "id")`,
		`CREATE INDEX IF NOT EXISTS idx_bilvie_content_search ON "bilvie_content" ("title", "code", "keywords")`,
		`CREATE INDEX IF NOT EXISTS idx_bilvie_content_source ON "bilvie_content" ("source_table", "source_id")`,
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create unified content index: %w", err)
		}
	}

	return nil
}

// MigrateLegacyContent imports historical rows into the runtime content
// table. It is intentionally called only by ImportAccess.
func MigrateLegacyContent(ctx context.Context, database *sql.DB) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin unified content migration: %w", err)
	}
	defer tx.Rollback()
	for _, source := range legacyContentSources {
		if err := migrateLegacyContentSource(ctx, tx, source); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit unified content migration: %w", err)
	}
	return nil
}

func migrateLegacyContentSource(ctx context.Context, tx *sql.Tx, source legacyContentSource) error {
	rows, err := tx.QueryContext(ctx, source.query)
	if err != nil {
		return fmt.Errorf("read legacy content source %s: %w", source.table, err)
	}
	defer rows.Close()
	for rows.Next() {
		record, err := source.scan(rows)
		if err != nil {
			return fmt.Errorf("scan legacy content source %s: %w", source.table, err)
		}
		if err := insertMigratedContent(ctx, tx, record, source.table); err != nil {
			return fmt.Errorf("migrate legacy content %d from %s: %w", record.RouteID, source.table, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read legacy content source %s: %w", source.table, err)
	}
	return nil
}

func scanLegacyCatalogContent(rows *sql.Rows) (legacyContentRecord, error) {
	var record legacyContentRecord
	var smallImage, largeImage string
	if err := rows.Scan(&record.RouteID, &record.CategoryID, &record.Title, &record.Code, &record.Summary, &record.Body, &smallImage, &largeImage, &record.Keywords, &record.OrderID, &record.Featured, &record.Visible); err != nil {
		return legacyContentRecord{}, err
	}
	record.Image = largeImage
	if record.Image == "" || strings.Contains(strings.ToLower(record.Image), "dfpic.gif") {
		record.Image = smallImage
	}
	return record, nil
}

func scanLegacyArticleContent(rows *sql.Rows) (legacyContentRecord, error) {
	var record legacyContentRecord
	if err := rows.Scan(&record.RouteID, &record.CategoryID, &record.Title, &record.Body, &record.Image, &record.PublishedAt, &record.Source, &record.Keywords, &record.Description, &record.Featured); err != nil {
		return legacyContentRecord{}, err
	}
	record.Summary = record.Description
	record.OrderID = record.RouteID
	record.Visible = 1
	return record, nil
}

func insertMigratedContent(ctx context.Context, tx *sql.Tx, record legacyContentRecord, sourceTable string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO "bilvie_content"
		("category_id", "route_key", "title", "code", "summary", "body", "cover_image", "published_at", "source", "keywords", "description", "sort_order", "featured", "visible", "source_table", "source_id")
		SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		WHERE NOT EXISTS (
			SELECT 1 FROM "bilvie_content" WHERE "source_table" = ? AND "source_id" = ?
		)`, record.CategoryID, strconv.FormatInt(record.RouteID, 10), record.Title, record.Code, record.Summary, record.Body, record.Image, record.PublishedAt, record.Source, record.Keywords, record.Description, record.OrderID, record.Featured, record.Visible, sourceTable, record.RouteID, sourceTable, record.RouteID)
	return err
}
