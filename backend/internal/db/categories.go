package db

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"bilvie/internal/routing"
	"bilvie/internal/templateconfig"
)

const unifiedCategoryTable = "bilvie_category"

const (
	legacyCatalogSource = "benming_ch_ProdCat"
	legacyArticleSource = "benming_ch_NewsCat"
	// These values are preserved only for a fresh import of the historical
	// site. They are never used as defaults by the runtime CMS.
	legacyRootListPath  = "valve"
	legacyChildListPath = "Products"
	legacyDetailPath    = "Product"
)

type legacyCategory struct {
	ID                int64
	Name              string
	ParentID          int64
	OrderID           int64
	ListPath          string
	ListFilePattern   string
	ListTemplate      string
	DetailPath        string
	DetailFilePattern string
	DetailTemplate    string
}

type legacyCategorySource struct {
	sourceTable           string
	categoryQuery         string
	contentCategoryUpdate string
	pageSize              int64
	withRouteFields       bool
	applyRouteDefaults    func([]legacyCategory, int64, *legacyCategory)
}

// These adapters are used only while importing the old Access database. The
// runtime CMS has one category table and does not branch on the source table.
var legacyCategorySources = []legacyCategorySource{
	{
		sourceTable: legacyCatalogSource,
		categoryQuery: `SELECT "id", COALESCE("CatName", ''), COALESCE("Root", 0), COALESCE("Orderid", 0),
			COALESCE("ListPath", ''), COALESCE("ListFilePattern", ''), COALESCE("ListTemplate", ''),
			COALESCE("DetailPath", ''), COALESCE("DetailFilePattern", ''), COALESCE("DetailTemplate", '')
			FROM "benming_ch_ProdCat" ORDER BY "id"`,
		contentCategoryUpdate: `UPDATE "benming_ch_prod" SET "CatId" = ? WHERE "CatId" = ?`,
		pageSize:              14,
		withRouteFields:       true,
		applyRouteDefaults:    applyLegacyCatalogRoutes,
	},
	{
		sourceTable: legacyArticleSource,
		categoryQuery: `SELECT "id", COALESCE("CatName", ''), COALESCE("Root", 0), COALESCE("ORderID", 0)
			FROM "benming_ch_NewsCat" ORDER BY "id"`,
		contentCategoryUpdate: `UPDATE "benming_ch_news" SET "Typeid" = ? WHERE "Typeid" = ?`,
		pageSize:              6,
		applyRouteDefaults:    applyLegacyArticleRoutes,
	},
}

// EnsureUnifiedCategories creates the runtime category model. It deliberately
// does not inspect the imported source tables; those are handled by the
// explicit one-time migration command.
func EnsureUnifiedCategories(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS "`+unifiedCategoryTable+`" (
			"id" INTEGER PRIMARY KEY,
			"name" TEXT NOT NULL,
			"parent_id" INTEGER NOT NULL DEFAULT 0,
			"order_id" INTEGER NOT NULL DEFAULT 0,
			"list_page_size" INTEGER NOT NULL DEFAULT 14,
			"route_id" INTEGER NOT NULL,
			"list_path" TEXT NOT NULL,
			"list_file_pattern" TEXT NOT NULL,
			"list_template" TEXT NOT NULL,
			"detail_path" TEXT NOT NULL,
			"detail_file_pattern" TEXT NOT NULL,
			"detail_template" TEXT NOT NULL,
			"source_table" TEXT,
			"source_id" INTEGER,
			UNIQUE ("source_table", "source_id")
		)`); err != nil {
		return fmt.Errorf("create unified category table: %w", err)
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_bilvie_category_parent ON "bilvie_category" ("parent_id", "order_id", "id")`,
		`CREATE INDEX IF NOT EXISTS idx_bilvie_category_source ON "bilvie_category" ("source_table", "source_id")`,
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create unified category index: %w", err)
		}
	}
	if err := ensureCategoryPageSize(ctx, database); err != nil {
		return err
	}

	return nil
}

// MigrateLegacyCategories imports the historical category tables into the
// runtime model. It is intentionally called only by ImportAccess.
func MigrateLegacyCategories(ctx context.Context, database *sql.DB) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin unified category migration: %w", err)
	}
	defer tx.Rollback()

	for _, source := range legacyCategorySources {
		if err := migrateLegacyCategories(ctx, tx, source); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit unified category migration: %w", err)
	}
	return nil
}

func migrateLegacyCategories(ctx context.Context, tx *sql.Tx, source legacyCategorySource) error {
	rows, err := readLegacyCategories(ctx, tx, source.categoryQuery, source.sourceTable, source.withRouteFields)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })

	ids := make(map[int64]int64, len(rows))
	var maxID int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX("id"), 0) FROM "bilvie_category"`).Scan(&maxID); err != nil {
		return fmt.Errorf("read unified category id: %w", err)
	}
	for _, item := range rows {
		mapped, exists, err := existingSourceCategoryID(ctx, tx, source.sourceTable, item.ID)
		if err != nil {
			return err
		}
		if exists {
			ids[item.ID] = mapped
			continue
		}
		candidate := item.ID
		if candidate < 1 || categoryIDExists(ids, candidate) || unifiedCategoryIDExists(ctx, tx, candidate) {
			maxID++
			candidate = maxID
		} else if candidate > maxID {
			maxID = candidate
		}
		ids[item.ID] = candidate
	}

	for _, item := range rows {
		if _, exists, err := existingSourceCategoryID(ctx, tx, source.sourceTable, item.ID); err != nil {
			return err
		} else if exists {
			continue
		}
		parentID := int64(0)
		if item.ParentID > 0 {
			parentID = ids[item.ParentID]
		}
		pageSize := source.pageSize
		source.applyRouteDefaults(rows, item.ID, &item)
		_, err := tx.ExecContext(ctx, `
			INSERT INTO "bilvie_category"
			("id", "name", "parent_id", "order_id", "list_page_size", "route_id",
			 "list_path", "list_file_pattern", "list_template", "detail_path",
			 "detail_file_pattern", "detail_template", "source_table", "source_id")
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			ids[item.ID], item.Name, parentID, item.OrderID, pageSize, item.ID,
			item.ListPath, item.ListFilePattern, item.ListTemplate, item.DetailPath,
			item.DetailFilePattern, item.DetailTemplate, source.sourceTable, item.ID)
		if err != nil {
			return fmt.Errorf("migrate %s category %d: %w", source.sourceTable, item.ID, err)
		}
	}

	for oldID, newID := range ids {
		if _, err := tx.ExecContext(ctx, source.contentCategoryUpdate, newID, oldID); err != nil {
			return fmt.Errorf("migrate %s content reference %d: %w", source.sourceTable, oldID, err)
		}
	}
	return nil
}

func ensureCategoryPageSize(ctx context.Context, database *sql.DB) error {
	var exists int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('bilvie_category') WHERE name = 'list_page_size'`).Scan(&exists); err != nil {
		return fmt.Errorf("inspect category page size column: %w", err)
	}
	if exists == 0 {
		if _, err := database.ExecContext(ctx, `ALTER TABLE "bilvie_category" ADD COLUMN "list_page_size" INTEGER NOT NULL DEFAULT 14`); err != nil {
			return fmt.Errorf("add category page size column: %w", err)
		}
	}
	return nil
}

func readLegacyCategories(ctx context.Context, tx *sql.Tx, query, sourceTable string, withRouteFields bool) ([]legacyCategory, error) {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("read %s categories: %w", sourceTable, err)
	}
	defer rows.Close()
	items := make([]legacyCategory, 0)
	for rows.Next() {
		var item legacyCategory
		values := []any{&item.ID, &item.Name, &item.ParentID, &item.OrderID}
		if withRouteFields {
			values = append(values, &item.ListPath, &item.ListFilePattern, &item.ListTemplate, &item.DetailPath, &item.DetailFilePattern, &item.DetailTemplate)
		}
		err = rows.Scan(values...)
		if err != nil {
			return nil, fmt.Errorf("scan %s category: %w", sourceTable, err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read %s categories: %w", sourceTable, err)
	}
	return items, nil
}

func applyLegacyCatalogRoutes(_ []legacyCategory, _ int64, item *legacyCategory) {
	if strings.TrimSpace(item.ListPath) == "" {
		item.ListPath = legacyChildListPath
		if item.ParentID == 0 {
			item.ListPath = legacyRootListPath
		}
	}
	if strings.TrimSpace(item.ListFilePattern) == "" {
		item.ListFilePattern = routing.DefaultListPattern
	}
	if strings.TrimSpace(item.ListTemplate) == "" {
		item.ListTemplate = templateconfig.DefaultListTemplate
	}
	if strings.TrimSpace(item.DetailPath) == "" {
		item.DetailPath = legacyDetailPath
	}
	if strings.TrimSpace(item.DetailFilePattern) == "" {
		item.DetailFilePattern = routing.DefaultDetailPattern
	}
	if strings.TrimSpace(item.DetailTemplate) == "" {
		item.DetailTemplate = templateconfig.DefaultDetailTemplate
	}
}

func applyLegacyArticleRoutes(rows []legacyCategory, id int64, item *legacyCategory) {
	rootID := id
	byID := make(map[int64]legacyCategory, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	seen := map[int64]bool{}
	for {
		parent, ok := byID[rootID]
		if !ok || parent.ParentID == 0 || seen[rootID] {
			break
		}
		seen[rootID] = true
		rootID = parent.ParentID
	}
	for _, rule := range legacyArticleRouteRules {
		if rule.RootID == rootID || rule.RootID == legacyRouteFallback {
			item.ListPath = rule.ListPath
			item.DetailPath = rule.DetailPath
			break
		}
	}
	item.ListFilePattern = routing.DefaultListPattern
	item.ListTemplate = templateconfig.DefaultListTemplate
	item.DetailFilePattern = routing.DefaultListPattern
	item.DetailTemplate = templateconfig.DefaultDetailTemplate
}

const legacyRouteFallback int64 = -1

type legacyCategoryRouteRule struct {
	RootID     int64
	ListPath   string
	DetailPath string
}

// Historical URL aliases are migration input. They are stored on each
// imported category and are never consulted by the runtime route resolver.
var legacyArticleRouteRules = []legacyCategoryRouteRule{
	{RootID: 12, ListPath: "service", DetailPath: "service/detail"},
	{RootID: legacyRouteFallback, ListPath: "news", DetailPath: "news/detail"},
}

func existingSourceCategoryID(ctx context.Context, tx *sql.Tx, source string, sourceID int64) (int64, bool, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT "id" FROM "bilvie_category" WHERE "source_table" = ? AND "source_id" = ?`, source, sourceID).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read migrated category %s/%d: %w", source, sourceID, err)
	}
	return id, true, nil
}

func unifiedCategoryIDExists(ctx context.Context, tx *sql.Tx, id int64) bool {
	var count int
	return tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM "bilvie_category" WHERE "id" = ?`, id).Scan(&count) == nil && count > 0
}

func categoryIDExists(ids map[int64]int64, id int64) bool {
	for _, mapped := range ids {
		if mapped == id {
			return true
		}
	}
	return false
}
