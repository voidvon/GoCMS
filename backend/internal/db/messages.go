package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

const (
	unifiedMessageTable = "gocms_message"
	FeedbackClassTable  = "gocms_feedback_class"
	FeedbackFieldTable  = "gocms_feedback_field"
)

// EnsureMessages creates the message/feedback model aligned with EmpireCMS (enewsfeedback, enewsfeedbackclass, enewsfeedbackf).
func EnsureMessages(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS "`+unifiedMessageTable+`" (
			"id" INTEGER PRIMARY KEY AUTOINCREMENT,
			"class_id" INTEGER NOT NULL DEFAULT 1,
			"title" TEXT NOT NULL DEFAULT '',
			"name" TEXT NOT NULL DEFAULT '',
			"phone" TEXT NOT NULL DEFAULT '',
			"mobile" TEXT NOT NULL DEFAULT '',
			"fax" TEXT NOT NULL DEFAULT '',
			"email" TEXT NOT NULL DEFAULT '',
			"address" TEXT NOT NULL DEFAULT '',
			"content" TEXT NOT NULL DEFAULT '',
			"created_at" TEXT NOT NULL DEFAULT '',
			"state" INTEGER NOT NULL DEFAULT 0,
			"content_id" INTEGER NOT NULL DEFAULT 0,
			"extra_data" TEXT NOT NULL DEFAULT '{}',
			"ip" TEXT NOT NULL DEFAULT ''
		)`); err != nil {
		return fmt.Errorf("create unified message table: %w", err)
	}

	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS "`+FeedbackClassTable+`" (
			"id" INTEGER PRIMARY KEY AUTOINCREMENT,
			"name" TEXT NOT NULL,
			"description" TEXT NOT NULL DEFAULT '',
			"fields_config" TEXT NOT NULL DEFAULT '[]',
			"must_fields" TEXT NOT NULL DEFAULT '[]',
			"sort_order" INTEGER NOT NULL DEFAULT 0,
			"created_at" TEXT NOT NULL DEFAULT ''
		)`); err != nil {
		return fmt.Errorf("create feedback class table: %w", err)
	}

	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS "`+FeedbackFieldTable+`" (
			"id" INTEGER PRIMARY KEY AUTOINCREMENT,
			"field_name" TEXT NOT NULL UNIQUE,
			"field_label" TEXT NOT NULL,
			"field_type" TEXT NOT NULL DEFAULT 'text',
			"field_options" TEXT NOT NULL DEFAULT '',
			"description" TEXT NOT NULL DEFAULT '',
			"sort_order" INTEGER NOT NULL DEFAULT 0,
			"is_system" INTEGER NOT NULL DEFAULT 0
		)`); err != nil {
		return fmt.Errorf("create feedback field table: %w", err)
	}

	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_gocms_message_state ON "gocms_message" ("state", "id")`,
		`CREATE INDEX IF NOT EXISTS idx_gocms_message_content ON "gocms_message" ("content_id", "id")`,
		`CREATE INDEX IF NOT EXISTS idx_gocms_message_class ON "gocms_message" ("class_id", "id")`,
		`CREATE INDEX IF NOT EXISTS idx_gocms_feedback_class_sort ON "` + FeedbackClassTable + `" ("sort_order", "id")`,
		`CREATE INDEX IF NOT EXISTS idx_gocms_feedback_field_sort ON "` + FeedbackFieldTable + `" ("sort_order", "id")`,
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create unified message index: %w", err)
		}
	}

	// Seamless migration for existing gocms_message tables
	for name, definition := range map[string]string{
		"class_id":   "INTEGER NOT NULL DEFAULT 1",
		"extra_data": "TEXT NOT NULL DEFAULT '{}'",
		"ip":         "TEXT NOT NULL DEFAULT ''",
	} {
		if err := ensureMessageColumn(ctx, database, name, definition); err != nil {
			return err
		}
	}

	return seedDefaultFeedback(ctx, database)
}

func ensureMessageColumn(ctx context.Context, database *sql.DB, name, definition string) error {
	var exists int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('`+unifiedMessageTable+`') WHERE name = ?`, name).Scan(&exists); err != nil {
		return fmt.Errorf("inspect message %s column: %w", name, err)
	}
	if exists == 0 {
		if _, err := database.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE "%s" ADD COLUMN "%s" %s`, unifiedMessageTable, name, definition)); err != nil {
			return fmt.Errorf("add message column %s: %w", name, err)
		}
	}
	return nil
}

func seedDefaultFeedback(ctx context.Context, database *sql.DB) error {
	var classCount int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM "`+FeedbackClassTable+`"`).Scan(&classCount); err != nil {
		return fmt.Errorf("check feedback class count: %w", err)
	}
	if classCount > 0 {
		return nil
	}

	now := time.Now().Format("2006-01-02 15:04:05")

	type fDef struct {
		name      string
		label     string
		fieldType string
		options   string
		desc      string
		order     int
		isSystem  int
	}

	fields := []fDef{
		{"title", "信息标题", "text", "", "反馈主题或咨询标题", 10, 1},
		{"name", "您的姓名", "text", "", "联系人姓名", 20, 1},
		{"phone", "联系电话", "text", "", "固话或联系手机", 30, 1},
		{"email", "电子邮箱", "text", "", "常用电子邮箱", 40, 1},
		{"company", "公司名称", "text", "", "工作单位或企业名称", 50, 1},
		{"address", "联系地址", "text", "", "通讯地址", 60, 1},
		{"content", "反馈内容", "textarea", "", "具体的反馈或咨询需求", 70, 1},
	}

	for _, f := range fields {
		if _, err := database.ExecContext(ctx, `
			INSERT OR IGNORE INTO "`+FeedbackFieldTable+`" ("field_name", "field_label", "field_type", "field_options", "description", "sort_order", "is_system")
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			f.name, f.label, f.fieldType, f.options, f.desc, f.order, f.isSystem); err != nil {
			return fmt.Errorf("seed default feedback field %s: %w", f.name, err)
		}
	}

	fieldsConfigJSON, _ := json.Marshal([]map[string]string{
		{"field": "title", "label": "信息标题"},
		{"field": "name", "label": "您的姓名"},
		{"field": "phone", "label": "联系电话"},
		{"field": "email", "label": "电子邮箱"},
		{"field": "company", "label": "公司名称"},
		{"field": "address", "label": "联系地址"},
		{"field": "content", "label": "反馈内容"},
	})
	mustFieldsJSON, _ := json.Marshal([]string{"title", "name", "phone"})

	if _, err := database.ExecContext(ctx, `
		INSERT INTO "`+FeedbackClassTable+`" ("name", "description", "fields_config", "must_fields", "sort_order", "created_at")
		VALUES ('默认反馈分类', '系统默认客户留言与业务咨询分类', ?, ?, 10, ?)`,
		string(fieldsConfigJSON), string(mustFieldsJSON), now); err != nil {
		return fmt.Errorf("seed default feedback class: %w", err)
	}

	return nil
}
