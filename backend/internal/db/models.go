package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	ModelTableTable = "gocms_model_table"
	ModelFieldTable = "gocms_model_field"
	ModelTable      = "gocms_model"
)

// EnsureModels creates the system model tables aligned with EmpireCMS (enewstable, enewsf, enewsmod).
func EnsureModels(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS "`+ModelTableTable+`" (
			"id" INTEGER PRIMARY KEY AUTOINCREMENT,
			"table_name" TEXT NOT NULL UNIQUE,
			"name" TEXT NOT NULL,
			"description" TEXT NOT NULL DEFAULT '',
			"is_default" INTEGER NOT NULL DEFAULT 0,
			"created_at" TEXT NOT NULL DEFAULT ''
		)`); err != nil {
			return fmt.Errorf("create model table table: %w", err)
	}

	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS "`+ModelFieldTable+`" (
			"id" INTEGER PRIMARY KEY AUTOINCREMENT,
			"table_id" INTEGER NOT NULL DEFAULT 1,
			"field_name" TEXT NOT NULL,
			"field_label" TEXT NOT NULL,
			"field_type" TEXT NOT NULL DEFAULT 'text',
			"field_options" TEXT NOT NULL DEFAULT '',
			"description" TEXT NOT NULL DEFAULT '',
			"sort_order" INTEGER NOT NULL DEFAULT 0,
			"is_system" INTEGER NOT NULL DEFAULT 0,
			"is_translatable" INTEGER NOT NULL DEFAULT 0,
			UNIQUE("table_id", "field_name")
		)`); err != nil {
		return fmt.Errorf("create model field table: %w", err)
	}

	if err := ensureModelFieldColumn(ctx, database, "is_translatable", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}

	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS "`+ModelTable+`" (
			"id" INTEGER PRIMARY KEY AUTOINCREMENT,
			"name" TEXT NOT NULL,
			"table_id" INTEGER NOT NULL DEFAULT 1,
			"description" TEXT NOT NULL DEFAULT '',
			"entry_fields" TEXT NOT NULL DEFAULT '[]',
			"must_fields" TEXT NOT NULL DEFAULT '[]',
			"is_default" INTEGER NOT NULL DEFAULT 0,
			"sort_order" INTEGER NOT NULL DEFAULT 0,
			"created_at" TEXT NOT NULL DEFAULT ''
		)`); err != nil {
		return fmt.Errorf("create model table: %w", err)
	}

	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_gocms_model_field_table ON "` + ModelFieldTable + `" ("table_id", "sort_order", "id")`,
		`CREATE INDEX IF NOT EXISTS idx_gocms_model_table ON "` + ModelTable + `" ("table_id", "sort_order", "id")`,
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create model indexes: %w", err)
		}
	}

	return seedDefaultModels(ctx, database)
}

func seedDefaultModels(ctx context.Context, database *sql.DB) error {
	now := time.Now().Format("2006-01-02 15:04:05")

	if _, err := ensureArticleModel(ctx, database, now); err != nil {
		return fmt.Errorf("ensure article model: %w", err)
	}

	if _, err := ensureProductModel(ctx, database, now); err != nil {
		return fmt.Errorf("ensure product model: %w", err)
	}

	_, _ = database.ExecContext(ctx, `
		UPDATE "`+ModelFieldTable+`"
		SET "is_translatable" = 1
		WHERE "field_name" IN ('title', 'summary', 'body', 'keywords', 'description')
	`)

	if err := MigrateCategoryAndContentModels(ctx, database); err != nil {
		return fmt.Errorf("migrate category and content models: %w", err)
	}

	return nil
}

// MigrateCategoryAndContentModels ensures existing categories and contents have valid model assignments.
func MigrateCategoryAndContentModels(ctx context.Context, database *sql.DB) error {
	if !tableExists(ctx, database, "gocms_category") || !tableExists(ctx, database, "gocms_content") {
		return nil
	}

	// Ensure any category with invalid model_id is set to default model (1)
	if _, err := database.ExecContext(ctx, `
		UPDATE "gocms_category"
		SET "model_id" = 1
		WHERE "model_id" <= 0 OR "model_id" IS NULL`); err != nil {
		return fmt.Errorf("align category default model: %w", err)
	}

	// Ensure any content with invalid model_id inherits from its category or defaults to 1
	if _, err := database.ExecContext(ctx, `
		UPDATE "gocms_content"
		SET "model_id" = COALESCE(
			(SELECT "c"."model_id" FROM "gocms_category" "c" WHERE "c"."id" = "gocms_content"."category_id"),
			1
		)
		WHERE "model_id" <= 0 OR "model_id" IS NULL`); err != nil {
		return fmt.Errorf("align content model_id: %w", err)
	}

	return nil
}

func tableExists(ctx context.Context, database *sql.DB, tableName string) bool {
	var count int
	_ = database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, tableName).Scan(&count)
	return count > 0
}

func ensureArticleModel(ctx context.Context, database *sql.DB, now string) (int64, error) {
	var tableID int64
	err := database.QueryRowContext(ctx, `SELECT "id" FROM "`+ModelTableTable+`" WHERE "table_name" = 'article'`).Scan(&tableID)
	if err == sql.ErrNoRows {
		res, err := database.ExecContext(ctx, `
			INSERT INTO "`+ModelTableTable+`" ("table_name", "name", "description", "is_default", "created_at")
			VALUES ('article', '文章数据表', '系统默认基础文章数据表', 1, ?)`, now)
		if err != nil {
			return 0, fmt.Errorf("seed article table: %w", err)
		}
		tableID, err = res.LastInsertId()
		if err != nil {
			return 0, err
		}
	} else if err != nil {
		return 0, fmt.Errorf("query article table: %w", err)
	}

	type fieldDef struct {
		name      string
		label     string
		fieldType string
		options   string
		desc      string
		order     int
		isSystem  int
	}

	fields := []fieldDef{
		{"title", "信息标题", "text", "", "内容的标题，必填", 10, 1},
		{"summary", "内容摘要", "textarea", "", "内容简要概述", 20, 1},
		{"body", "正文内容", "editor", "", "正文详细内容", 30, 1},
		{"cover_image", "缩略图", "image", "", "列表或详情展示图", 40, 1},
		{"published_at", "发布时间", "date", "", "发布或更新时间", 50, 1},
		{"source", "信息来源", "text", "", "如转载网站或作者单位", 60, 1},
		{"keywords", "关键词", "text", "", "页面SEO关键词", 70, 1},
		{"description", "描述", "textarea", "", "页面SEO描述", 80, 1},
	}

	for _, f := range fields {
		if _, err := database.ExecContext(ctx, `
			INSERT OR IGNORE INTO "`+ModelFieldTable+`" ("table_id", "field_name", "field_label", "field_type", "field_options", "description", "sort_order", "is_system")
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			tableID, f.name, f.label, f.fieldType, f.options, f.desc, f.order, f.isSystem); err != nil {
			return 0, fmt.Errorf("seed article field %s: %w", f.name, err)
		}
	}

	var modelID int64
	err = database.QueryRowContext(ctx, `SELECT "id" FROM "`+ModelTable+`" WHERE "table_id" = ? AND "name" = '文章系统模型'`, tableID).Scan(&modelID)
	if err == sql.ErrNoRows {
		entryFieldsJSON, _ := json.Marshal([]map[string]string{
			{"field": "title", "label": "信息标题"},
			{"field": "summary", "label": "内容摘要"},
			{"field": "body", "label": "正文内容"},
			{"field": "cover_image", "label": "缩略图"},
			{"field": "published_at", "label": "发布时间"},
			{"field": "source", "label": "信息来源"},
			{"field": "keywords", "label": "关键词"},
			{"field": "description", "label": "描述"},
		})
		mustFieldsJSON, _ := json.Marshal([]string{"title"})

		res, err := database.ExecContext(ctx, `
			INSERT INTO "`+ModelTable+`" ("name", "table_id", "description", "entry_fields", "must_fields", "is_default", "sort_order", "created_at")
			VALUES ('文章系统模型', ?, '系统默认文章模型', ?, ?, 1, 10, ?)`,
			tableID, string(entryFieldsJSON), string(mustFieldsJSON), now)
		if err != nil {
			return 0, fmt.Errorf("seed article model: %w", err)
		}
		modelID, err = res.LastInsertId()
		if err != nil {
			return 0, err
		}
	} else if err != nil {
		return 0, fmt.Errorf("query article model: %w", err)
	} else {
		// Clean up code from article table and model if present
		_, _ = database.ExecContext(ctx, `DELETE FROM "`+ModelFieldTable+`" WHERE "table_id" = ? AND "field_name" = 'code'`, tableID)
		var existingEntryFields string
		if err := database.QueryRowContext(ctx, `SELECT "entry_fields" FROM "`+ModelTable+`" WHERE "id" = ?`, modelID).Scan(&existingEntryFields); err == nil {
			if strings.Contains(existingEntryFields, `"code"`) {
				var items []map[string]string
				if err := json.Unmarshal([]byte(existingEntryFields), &items); err == nil {
					var filtered []map[string]string
					for _, item := range items {
						if item["field"] != "code" {
							filtered = append(filtered, item)
						}
					}
					updatedJSON, _ := json.Marshal(filtered)
					_, _ = database.ExecContext(ctx, `UPDATE "`+ModelTable+`" SET "entry_fields" = ? WHERE "id" = ?`, string(updatedJSON), modelID)
				}
			}
		}
	}

	return modelID, nil
}

func ensureProductModel(ctx context.Context, database *sql.DB, now string) (int64, error) {
	var tableID int64
	err := database.QueryRowContext(ctx, `SELECT "id" FROM "`+ModelTableTable+`" WHERE "table_name" = 'product'`).Scan(&tableID)
	if err == sql.ErrNoRows {
		res, err := database.ExecContext(ctx, `
			INSERT INTO "`+ModelTableTable+`" ("table_name", "name", "description", "is_default", "created_at")
			VALUES ('product', '产品数据表', '系统默认产品与商品数据表', 0, ?)`, now)
		if err != nil {
			return 0, fmt.Errorf("seed product table: %w", err)
		}
		tableID, err = res.LastInsertId()
		if err != nil {
			return 0, err
		}
	} else if err != nil {
		return 0, fmt.Errorf("query product table: %w", err)
	}

	type fieldDef struct {
		name      string
		label     string
		fieldType string
		options   string
		desc      string
		order     int
		isSystem  int
	}

	fields := []fieldDef{
		{"title", "产品名称", "text", "", "产品名称或信息标题，必填", 10, 1},
		{"code", "产品型号", "text", "", "产品型号或统一编号", 20, 1},
		{"price", "参考价格", "text", "", "产品参考价格或指导价", 25, 0},
		{"summary", "产品简介", "textarea", "", "产品简要概述", 30, 1},
		{"body", "详细说明", "editor", "", "详细规格说明与图文介绍", 40, 1},
		{"cover_image", "产品图片", "image", "", "产品主图或外观展示图", 50, 1},
		{"photo_list", "产品图集", "morepic", "", "产品多角度细节实拍与图集展示", 60, 0},
		{"published_at", "发布时间", "date", "", "发布或更新时间", 70, 1},
		{"keywords", "关键词", "text", "", "页面SEO关键词", 80, 1},
		{"description", "描述", "textarea", "", "页面SEO描述", 90, 1},
	}

	for _, f := range fields {
		if _, err := database.ExecContext(ctx, `
			INSERT OR IGNORE INTO "`+ModelFieldTable+`" ("table_id", "field_name", "field_label", "field_type", "field_options", "description", "sort_order", "is_system")
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			tableID, f.name, f.label, f.fieldType, f.options, f.desc, f.order, f.isSystem); err != nil {
			return 0, fmt.Errorf("seed product field %s: %w", f.name, err)
		}
	}

	var modelID int64
	err = database.QueryRowContext(ctx, `SELECT "id" FROM "`+ModelTable+`" WHERE "table_id" = ? AND "name" = '产品系统模型'`, tableID).Scan(&modelID)
	if err == sql.ErrNoRows {
		entryFieldsJSON, _ := json.Marshal([]map[string]string{
			{"field": "title", "label": "产品名称"},
			{"field": "code", "label": "产品型号"},
			{"field": "price", "label": "参考价格"},
			{"field": "summary", "label": "产品简介"},
			{"field": "cover_image", "label": "产品图片"},
			{"field": "photo_list", "label": "产品图集"},
			{"field": "body", "label": "详细说明"},
			{"field": "published_at", "label": "发布时间"},
			{"field": "keywords", "label": "关键词"},
			{"field": "description", "label": "描述"},
		})
		mustFieldsJSON, _ := json.Marshal([]string{"title"})

		res, err := database.ExecContext(ctx, `
			INSERT INTO "`+ModelTable+`" ("name", "table_id", "description", "entry_fields", "must_fields", "is_default", "sort_order", "created_at")
			VALUES ('产品系统模型', ?, '系统默认产品模型，包含型号、图集、详细说明等通用参数', ?, ?, 0, 20, ?)`,
			tableID, string(entryFieldsJSON), string(mustFieldsJSON), now)
		if err != nil {
			return 0, fmt.Errorf("seed product model: %w", err)
		}
		modelID, err = res.LastInsertId()
		if err != nil {
			return 0, err
		}
	} else if err != nil {
		return 0, fmt.Errorf("query product model: %w", err)
	}

	return modelID, nil
}

func ensureModelFieldColumn(ctx context.Context, database *sql.DB, name, definition string) error {
	var exists int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('`+ModelFieldTable+`') WHERE name = ?`, name).Scan(&exists); err != nil {
		return fmt.Errorf("inspect model field %s column: %w", name, err)
	}
	if exists == 0 {
		if _, err := database.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE "%s" ADD COLUMN "%s" %s`, ModelFieldTable, name, definition)); err != nil {
			return fmt.Errorf("add model field column %s: %w", name, err)
		}
	}
	return nil
}

