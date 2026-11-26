package db

import (
	"context"
	"strings"
	"testing"
)

func TestMigrateAdminName(t *testing.T) {
	for _, tc := range []struct{ defaultName, want string }{
		{"  默认站点名称  ", "默认站点名称"},
		{"", "分站名称"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			ctx := context.Background()
			database, err := Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			if err := EnsureSites(ctx, database); err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec(`ALTER TABLE gocms_site ADD COLUMN admin_name TEXT NOT NULL DEFAULT '';
				INSERT INTO gocms_site (name, code, admin_name) VALUES ('分站', 'other', '分站名称')`); err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec(`UPDATE gocms_site SET admin_name = ? WHERE is_default = 1`, tc.defaultName); err != nil {
				t.Fatal(err)
			}
			if err := EnsureAdminSettings(ctx, database); err != nil {
				t.Fatal(err)
			}
			if got, err := LoadAdminName(ctx, database); err != nil || got != tc.want {
				t.Fatalf("migrated admin name = %q, want %q: %v", got, tc.want, err)
			}
			if err := SaveAdminName(ctx, database, ""); err != nil {
				t.Fatal(err)
			}
			if err := EnsureAdminSettings(ctx, database); err != nil {
				t.Fatal(err)
			}
			if got, err := LoadAdminName(ctx, database); err != nil || got != "" {
				t.Fatalf("admin name after clearing and restarting = %q: %v", got, err)
			}
		})
	}
}

func TestMigrateContentUpdateTime(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	if err := CreateSchema(ctx, database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`ALTER TABLE gocms_content DROP COLUMN updated_at;
		INSERT INTO gocms_content (id, title, published_at) VALUES (1, '历史内容', '2024-03-02 12:34:00')`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := CreateSchema(ctx, database); err != nil {
			t.Fatalf("upgrade database: %v", err)
		}
	}
	var updatedAt, publishedAt string
	err = database.QueryRow(`SELECT updated_at, published_at FROM gocms_content WHERE id = 1`).Scan(&updatedAt, &publishedAt)
	if err != nil || updatedAt != "" || publishedAt != "2024-03-02 12:34:00" {
		t.Fatalf("dates after upgrade: updated_at = %q, published_at = %q: %v", updatedAt, publishedAt, err)
	}
}

func TestCreateSchemaContainsOnlyRuntimeTables(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := CreateSchema(context.Background(), database); err != nil {
		t.Fatal(err)
	}

	rows, err := database.Query(`SELECT "name" FROM sqlite_master WHERE "type" = 'table' AND "name" NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := map[string]bool{
		"gocms_commerce_shipment": false, "gocms_commerce_shipment_item": false, "gocms_commerce_order_event": false,
		"gocms_commerce_product":        false,
		"gocms_commerce_dimension":      false,
		"gocms_commerce_pricing":        false,
		"gocms_commerce_offer":          false,
		"gocms_commerce_order":          false,
		"gocms_commerce_order_item":     false,
		"gocms_payment_account":         false,
		"gocms_payment_attempt":         false,
		"gocms_payment_refund":          false,
		"gocms_payment_event":           false,
		"gocms_commerce_outbox":         false,
		"gocms_site":                    false,
		"gocms_site_setting":            false,
		"gocms_admin_setting":           false,
		"gocms_language":                false,
		"gocms_admin_user":              false,
		"gocms_admin_group":             false,
		"gocms_admin_operation":         false,
		"gocms_access_log":              false,
		"gocms_redirect":                false,
		"gocms_admin_login":             false,
		"gocms_user_event":              false,
		"gocms_user":                    false,
		"gocms_user_session":            false,
		"gocms_user_login":              false,
		"gocms_user_group":              false,
		"gocms_user_group_member":       false,
		"gocms_category":                false,
		"gocms_category_translation":    false,
		"gocms_schema_migration":        false,
		"gocms_content":                 false,
		"gocms_content_translation":     false,
		"gocms_media":                   false,
		"gocms_platform_media_storage":  false,
		"gocms_database_backup":         false,
		"gocms_media_storage":           false,
		"gocms_media_ref":               false,
		"gocms_model_table":             false,
		"gocms_model_field":             false,
		"gocms_model":                   false,
		"gocms_feedback_class":          false,
		"gocms_feedback_field":          false,
		"gocms_message":                 false,
		"gocms_admin_notification":      false,
		"gocms_api_key":                 false,
		"gocms_api_key_event":           false,
		"gocms_template_assignment":     false,
		"gocms_template_label_category": false,
		"gocms_template_label":          false,
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(name, "gocms_") {
			t.Fatalf("non-runtime table created: %s", name)
		}
		if _, ok := want[name]; !ok {
			t.Fatalf("unexpected runtime table created: %s", name)
		}
		want[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for name, found := range want {
		if !found {
			t.Fatalf("runtime table missing: %s", name)
		}
	}
}
