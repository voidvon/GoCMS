package templateconfig

import (
	"context"
	"database/sql"
	"fmt"
	pathpkg "path"
	"sort"
	"strings"
)

const tableName = "gocms_template_assignment"

const (
	DimensionHome    = "home"
	DimensionList    = "list"
	DimensionDetail  = "detail"
	DimensionPublic  = "public"
	DimensionMessage = "message"
	DimensionOther   = "other"
)

const (
	RoleHomeIndex = "home_index"
	RoleMessage   = "message"
	RoleSearch    = "search"
)

const (
	// Categories choose their own templates. These are only the initial values
	// for a newly created category.
	DefaultListTemplate   = "category_list.html"
	DefaultCoverTemplate  = "category_cover.html"
	DefaultDetailTemplate = "content_detail.html"
)

type Assignment struct {
	SiteID        int64
	Key           string
	Label         string
	Dimension     string
	DimensionName string
	TemplatePath  string
	SortOrder     int
}

var defaults = []Assignment{
	{Key: RoleMessage, Label: "留言模板", Dimension: DimensionPublic, DimensionName: "公共模板", TemplatePath: "msg.html", SortOrder: 20},
	{Key: RoleSearch, Label: "搜索模板", Dimension: DimensionPublic, DimensionName: "公共模板", TemplatePath: "search.html", SortOrder: 30},
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func Defaults() []Assignment {
	items := make([]Assignment, len(defaults))
	copy(items, defaults)
	return items
}

func Default(key string) (Assignment, bool) {
	for _, item := range defaults {
		if item.Key == key {
			return item, true
		}
	}
	return Assignment{}, false
}

func Ensure(ctx context.Context, database *sql.DB) error {
	return EnsureForSite(ctx, database, 1)
}

func EnsureForSite(ctx context.Context, database *sql.DB, siteID int64) error {
	if siteID <= 0 {
		siteID = 1
	}

	var tableCount int
	_ = database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, tableName).Scan(&tableCount)

	if tableCount == 0 {
		if _, err := database.ExecContext(ctx, `
			CREATE TABLE IF NOT EXISTS "`+tableName+`" (
				"site_id" INTEGER NOT NULL DEFAULT 1,
				"key" TEXT NOT NULL,
				"template_path" TEXT NOT NULL,
				"updated_at" TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				PRIMARY KEY ("site_id", "key")
			)`); err != nil {
			return fmt.Errorf("create template assignment table: %w", err)
		}
	} else {
		var hasSiteID int
		_ = database.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('`+tableName+`') WHERE name = 'site_id'`).Scan(&hasSiteID)
		if hasSiteID == 0 {
			tx, err := database.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()

			if _, err := tx.ExecContext(ctx, `
				CREATE TABLE "gocms_template_assignment_v2" (
					"site_id" INTEGER NOT NULL DEFAULT 1,
					"key" TEXT NOT NULL,
					"template_path" TEXT NOT NULL,
					"updated_at" TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
					PRIMARY KEY ("site_id", "key")
				)`); err != nil {
				return fmt.Errorf("create migration template assignment table: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT OR REPLACE INTO "gocms_template_assignment_v2" ("site_id", "key", "template_path", "updated_at")
				SELECT 1, "key", "template_path", "updated_at" FROM "`+tableName+`"`); err != nil {
				return fmt.Errorf("migrate existing template assignments: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `DROP TABLE "`+tableName+`"`); err != nil {
				return fmt.Errorf("drop legacy template assignment table: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `ALTER TABLE "gocms_template_assignment_v2" RENAME TO "`+tableName+`"`); err != nil {
				return fmt.Errorf("rename template assignment table: %w", err)
			}
			if err := tx.Commit(); err != nil {
				return err
			}
		}
	}

	for _, item := range defaults {
		if _, err := database.ExecContext(ctx, `
			INSERT INTO "`+tableName+`" ("site_id", "key", "template_path") VALUES (?, ?, ?)
			ON CONFLICT ("site_id", "key") DO NOTHING`, siteID, item.Key, item.TemplatePath); err != nil {
			return fmt.Errorf("seed template assignment %s for site %d: %w", item.Key, siteID, err)
		}
	}
	return nil
}

func List(ctx context.Context, database *sql.DB) ([]Assignment, error) {
	return ListForSite(ctx, database, 1)
}

func ListForSite(ctx context.Context, database *sql.DB, siteID int64) ([]Assignment, error) {
	return load(ctx, database, siteID)
}

func Load(ctx context.Context, query queryer) (map[string]string, error) {
	return LoadForSite(ctx, query, 1)
}

func LoadForSite(ctx context.Context, query queryer, siteID int64) (map[string]string, error) {
	items, err := load(ctx, query, siteID)
	if err != nil {
		return nil, err
	}
	assignments := make(map[string]string, len(items))
	for _, item := range items {
		assignments[item.Key] = item.TemplatePath
	}
	return assignments, nil
}

func Update(ctx context.Context, database *sql.DB, key, templatePath string) error {
	return UpdateForSite(ctx, database, 1, key, templatePath)
}

func UpdateForSite(ctx context.Context, database *sql.DB, siteID int64, key, templatePath string) error {
	if siteID <= 0 {
		siteID = 1
	}
	if _, ok := Default(key); !ok {
		return fmt.Errorf("unknown template assignment: %s", key)
	}
	normalized, err := NormalizePath(templatePath)
	if err != nil {
		return err
	}
	result, err := database.ExecContext(ctx, `
		INSERT INTO "`+tableName+`" ("site_id", "key", "template_path", "updated_at")
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT ("site_id", "key") DO UPDATE SET "template_path" = excluded.template_path, "updated_at" = CURRENT_TIMESTAMP`,
		siteID, key, normalized)
	if err != nil {
		return fmt.Errorf("update template assignment %s: %w", key, err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("check template assignment %s: %w", key, err)
	} else if affected == 0 {
		return fmt.Errorf("template assignment could not be updated: %s", key)
	}
	return nil
}

func NormalizePath(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, `\`, "/"))
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, ":") {
		return "", fmt.Errorf("模板路径无效")
	}
	clean := pathpkg.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("模板路径无效")
	}
	if !strings.EqualFold(pathpkg.Ext(clean), ".html") {
		return "", fmt.Errorf("模板必须是 HTML 文件")
	}
	return clean, nil
}

func load(ctx context.Context, query queryer, siteID int64) ([]Assignment, error) {
	if siteID <= 0 {
		siteID = 1
	}
	byKey := make(map[string]Assignment, len(defaults))
	for _, item := range defaults {
		item.SiteID = siteID
		byKey[item.Key] = item
	}
	rows, err := query.QueryContext(ctx, `SELECT "key", "template_path" FROM "`+tableName+`" WHERE "site_id" = ?`, siteID)
	if err != nil {
		return nil, fmt.Errorf("read template assignments: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, templatePath string
		if err := rows.Scan(&key, &templatePath); err != nil {
			return nil, fmt.Errorf("scan template assignment: %w", err)
		}
		item, ok := byKey[key]
		if !ok {
			continue
		}
		item.TemplatePath, err = NormalizePath(templatePath)
		if err != nil {
			return nil, fmt.Errorf("template assignment %s: %w", key, err)
		}
		byKey[key] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read template assignments: %w", err)
	}
	items := make([]Assignment, 0, len(byKey))
	for _, item := range byKey {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].SortOrder == items[j].SortOrder {
			return items[i].Key < items[j].Key
		}
		return items[i].SortOrder < items[j].SortOrder
	})
	return items, nil
}
