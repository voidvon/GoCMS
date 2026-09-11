package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
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

// MigrateCategoryAndContentModels aligns existing categories and contents to their appropriate models.
func MigrateCategoryAndContentModels(ctx context.Context, database *sql.DB) error {
	if !tableExists(ctx, database, "gocms_category") || !tableExists(ctx, database, "gocms_content") || !tableExists(ctx, database, ModelTable) {
		return nil
	}

	var articleModelID int64
	_ = database.QueryRowContext(ctx, `SELECT "id" FROM "`+ModelTable+`" WHERE "name" = '文章系统模型'`).Scan(&articleModelID)
	if articleModelID == 0 {
		articleModelID = 1
	}

	var productModelID int64
	_ = database.QueryRowContext(ctx, `SELECT "id" FROM "`+ModelTable+`" WHERE "name" = '产品系统模型'`).Scan(&productModelID)
	if productModelID == 0 {
		return nil
	}

	return ensureCategoryAndContentModels(ctx, database, articleModelID, productModelID)
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
			VALUES ('product', '产品数据表', '用于机械、阀门、工业品等产品展示与技术参数管理', 0, ?)`, now)
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
		{"summary", "产品简介", "textarea", "", "产品简要概述", 30, 1},
		{"body", "详细说明", "editor", "", "详细技术图纸与规格说明", 40, 1},
		{"cover_image", "产品图片", "image", "", "产品主图或外观展示图", 50, 1},
		{"published_at", "发布时间", "date", "", "发布或更新时间", 60, 1},
		{"keywords", "关键词", "text", "", "页面SEO关键词", 70, 1},
		{"description", "描述", "textarea", "", "页面SEO描述", 80, 1},
		// Product extension fields
		{"spec", "规格型号", "text", "", "如 ANSI 150LB~600LB, Z41H系列等", 100, 0},
		{"material", "阀体材质", "select", "铸钢==铸钢\n不锈钢==不锈钢\n球墨铸铁==球墨铸铁\n铸铁==铸铁\n黄铜==黄铜\n合金钢==合金钢\n锻钢==锻钢\nPVC/塑料==PVC/塑料", "阀门阀体及关键部件材质", 110, 0},
		{"pressure", "公称压力", "select", "PN1.6MPa==PN1.6MPa\nPN2.5MPa==PN2.5MPa\nPN4.0MPa==PN4.0MPa\nPN6.4MPa==PN6.4MPa\nPN10.0MPa==PN10.0MPa\n150LB==150LB\n300LB==300LB\n600LB==600LB\n10K==10K\n20K==20K", "公称工作压力等级", 120, 0},
		{"caliber", "公称通径", "text", "", "如 DN15~DN600, 1/2\"~24\"", 130, 0},
		{"temperature", "适用温度", "text", "", "如 -20℃~425℃", 140, 0},
		{"medium", "适用介质", "text", "", "如 水、蒸汽、油品、气体、腐蚀性介质等", 150, 0},
		{"photo_list", "产品图集", "morepic", "", "产品多角度细节实拍与技术图纸", 160, 0},
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
			{"field": "spec", "label": "规格型号"},
			{"field": "material", "label": "阀体材质"},
			{"field": "pressure", "label": "公称压力"},
			{"field": "caliber", "label": "公称通径"},
			{"field": "temperature", "label": "适用温度"},
			{"field": "medium", "label": "适用介质"},
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
			VALUES ('产品系统模型', ?, '标准工业品及阀门系统模型，内置规格、材质、压力、通径、温度等技术参数', ?, ?, 0, 20, ?)`,
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
	} else {
		// Ensure photo_list is in entry_fields for existing product model
		var existingEntryFields string
		if err := database.QueryRowContext(ctx, `SELECT "entry_fields" FROM "`+ModelTable+`" WHERE "id" = ?`, modelID).Scan(&existingEntryFields); err == nil {
			if !strings.Contains(existingEntryFields, `"photo_list"`) {
				var items []map[string]string
				if err := json.Unmarshal([]byte(existingEntryFields), &items); err == nil {
					items = append(items, map[string]string{"field": "photo_list", "label": "产品图集"})
					updatedJSON, _ := json.Marshal(items)
					_, _ = database.ExecContext(ctx, `UPDATE "`+ModelTable+`" SET "entry_fields" = ? WHERE "id" = ?`, string(updatedJSON), modelID)
				}
			}
		}
	}

	return modelID, nil
}

func ensureCategoryAndContentModels(ctx context.Context, database *sql.DB, articleModelID, productModelID int64) error {
	// 1. Bind product categories (valve, Products) to productModelID
	if _, err := database.ExecContext(ctx, `
		UPDATE "gocms_category"
		SET "model_id" = ?
		WHERE "list_path" IN ('valve', 'Products') AND "model_id" != ?`,
		productModelID, productModelID); err != nil {
		return fmt.Errorf("bind product categories: %w", err)
	}

	// 2. Bind article categories (news, service, about) to articleModelID
	if _, err := database.ExecContext(ctx, `
		UPDATE "gocms_category"
		SET "model_id" = ?
		WHERE "list_path" IN ('news', 'service', 'about', '') AND "model_id" != ?`,
		articleModelID, articleModelID); err != nil {
		return fmt.Errorf("bind article categories: %w", err)
	}

	// 3. Align content model_id with its category model_id
	if _, err := database.ExecContext(ctx, `
		UPDATE "gocms_content"
		SET "model_id" = (
			SELECT "c"."model_id" FROM "gocms_category" "c" WHERE "c"."id" = "gocms_content"."category_id"
		)
		WHERE "category_id" IN (SELECT "id" FROM "gocms_category")
		  AND "model_id" != (
			SELECT "c"."model_id" FROM "gocms_category" "c" WHERE "c"."id" = "gocms_content"."category_id"
		  )`); err != nil {
		return fmt.Errorf("align content model_id: %w", err)
	}

	// 4. Extract structured parameters for legacy product contents
	if err := extractLegacyProductParameters(ctx, database, productModelID); err != nil {
		return fmt.Errorf("extract legacy product parameters: %w", err)
	}

	return nil
}

var (
	reSpec        = regexp.MustCompile(`(?:型号|规格)[：:\s]+([^，,；;\s\n\r]+)`)
	reCaliber     = regexp.MustCompile(`(?:口径|通径)[：:\s]+([^，,；;\s\n\r]+)`)
	reMaterial    = regexp.MustCompile(`(?:材质|阀体材质)[：:\s]+([^，,；;\s\n\r]+)`)
	rePressure    = regexp.MustCompile(`(?:压力|公称压力)[：:\s]+([^，,；;\s\n\r]+)`)
	reTemperature = regexp.MustCompile(`(?:温度|适用温度|工作温度)[：:\s]+([^，,；;\s\n\r]+)`)
	reMedium      = regexp.MustCompile(`(?:介质|适用介质)[：:\s]+([^，,；;\s\n\r]+)`)
)

func cleanParam(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "，,。;；:：")
	return s
}

func extractLegacyProductParameters(ctx context.Context, database *sql.DB, productModelID int64) error {
	rows, err := database.QueryContext(ctx, `
		SELECT "id", "code", "summary"
		FROM "gocms_content"
		WHERE "model_id" = ? AND ("extra_data" = '' OR "extra_data" = '{}' OR "extra_data" IS NULL)`,
		productModelID)
	if err != nil {
		return err
	}
	defer rows.Close()

	type itemUpdate struct {
		id   int64
		data string
	}
	var updates []itemUpdate

	for rows.Next() {
		var id int64
		var code, summary string
		if err := rows.Scan(&id, &code, &summary); err != nil {
			return err
		}

		extra := make(map[string]string)
		if m := reSpec.FindStringSubmatch(summary); len(m) > 1 {
			extra["spec"] = cleanParam(m[1])
		} else if strings.TrimSpace(code) != "" {
			extra["spec"] = strings.TrimSpace(code)
		}

		if m := reCaliber.FindStringSubmatch(summary); len(m) > 1 {
			extra["caliber"] = cleanParam(m[1])
		}
		if m := reMaterial.FindStringSubmatch(summary); len(m) > 1 {
			extra["material"] = cleanParam(m[1])
		}
		if m := rePressure.FindStringSubmatch(summary); len(m) > 1 {
			extra["pressure"] = cleanParam(m[1])
		}
		if m := reTemperature.FindStringSubmatch(summary); len(m) > 1 {
			extra["temperature"] = cleanParam(m[1])
		}
		if m := reMedium.FindStringSubmatch(summary); len(m) > 1 {
			extra["medium"] = cleanParam(m[1])
		}

		if len(extra) > 0 {
			b, err := json.Marshal(extra)
			if err == nil {
				updates = append(updates, itemUpdate{id: id, data: string(b)})
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	stmt, err := database.PrepareContext(ctx, `UPDATE "gocms_content" SET "extra_data" = ? WHERE "id" = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, u := range updates {
		if _, err := stmt.ExecContext(ctx, u.data, u.id); err != nil {
			return err
		}
	}

	return nil
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

