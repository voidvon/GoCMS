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
	DimensionMessage = "message"
	DimensionOther   = "other"
)

const (
	RoleHomeIndex   = "home_index"
	RoleAboutDetail = "about_detail"
	RoleMessage     = "message"
	RoleJobList     = "job_list"
	RoleJobDetail   = "job_detail"
)

const (
	// Categories choose their own templates. These are only the initial values
	// for a newly created category.
	DefaultListTemplate   = "category_list.html"
	DefaultDetailTemplate = "content_detail.html"
)

type Assignment struct {
	Key           string
	Label         string
	Dimension     string
	DimensionName string
	TemplatePath  string
	SortOrder     int
}

var defaults = []Assignment{
	{Key: RoleHomeIndex, Label: "首页模板", Dimension: DimensionHome, DimensionName: "首页", TemplatePath: "index.html", SortOrder: 10},
	{Key: RoleJobList, Label: "招聘列表模板", Dimension: DimensionList, DimensionName: "列表模板", TemplatePath: "job_sort.html", SortOrder: 20},
	{Key: RoleAboutDetail, Label: "公司介绍详情模板", Dimension: DimensionDetail, DimensionName: "详情模板", TemplatePath: "corporation.html", SortOrder: 30},
	{Key: RoleJobDetail, Label: "招聘详情模板", Dimension: DimensionDetail, DimensionName: "详情模板", TemplatePath: "job_detail.html", SortOrder: 40},
	{Key: RoleMessage, Label: "留言模板", Dimension: DimensionMessage, DimensionName: "留言模板", TemplatePath: "msg.html", SortOrder: 50},
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
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS "`+tableName+`" (
			"key" TEXT PRIMARY KEY,
			"template_path" TEXT NOT NULL,
			"updated_at" TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		return fmt.Errorf("create template assignment table: %w", err)
	}
	for _, item := range defaults {
		if _, err := database.ExecContext(ctx, `
			INSERT INTO "`+tableName+`" ("key", "template_path") VALUES (?, ?)
			ON CONFLICT ("key") DO NOTHING`, item.Key, item.TemplatePath); err != nil {
			return fmt.Errorf("seed template assignment %s: %w", item.Key, err)
		}
	}
	return nil
}

func List(ctx context.Context, database *sql.DB) ([]Assignment, error) {
	return load(ctx, database)
}

func Load(ctx context.Context, query queryer) (map[string]string, error) {
	items, err := load(ctx, query)
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
	if _, ok := Default(key); !ok {
		return fmt.Errorf("unknown template assignment: %s", key)
	}
	normalized, err := NormalizePath(templatePath)
	if err != nil {
		return err
	}
	result, err := database.ExecContext(ctx, `
		UPDATE "`+tableName+`"
		SET "template_path" = ?, "updated_at" = CURRENT_TIMESTAMP
		WHERE "key" = ?`, normalized, key)
	if err != nil {
		return fmt.Errorf("update template assignment %s: %w", key, err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("check template assignment %s: %w", key, err)
	} else if affected != 1 {
		return fmt.Errorf("template assignment does not exist: %s", key)
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

func load(ctx context.Context, query queryer) ([]Assignment, error) {
	byKey := make(map[string]Assignment, len(defaults))
	for _, item := range defaults {
		byKey[item.Key] = item
	}
	rows, err := query.QueryContext(ctx, `SELECT "key", "template_path" FROM "`+tableName+`"`)
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
