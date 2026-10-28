package db

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestDefaultModelSEOOrder(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := EnsureModels(ctx, database); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"article", "product"} {
		t.Run(table, func(t *testing.T) {
			var id int64
			var raw string
			if err := database.QueryRowContext(ctx, `SELECT m.id, m.entry_fields FROM gocms_model m JOIN gocms_model_table t ON t.id = m.table_id WHERE t.table_name = ?`, table).Scan(&id, &raw); err != nil {
				t.Fatal(err)
			}
			var defaults []map[string]string
			if err := json.Unmarshal([]byte(raw), &defaults); err != nil {
				t.Fatal(err)
			}
			summary := -1
			for i, entry := range defaults {
				if entry["field"] == "summary" {
					summary = i
				}
			}
			if summary < 0 || defaults[summary+1]["field"] != "seo_title" || defaults[summary+2]["field"] != "description" {
				t.Fatalf("SEO fields must follow summary: %s", raw)
			}
			// Restore the old default order, including a customized label.
			defaults[summary+1]["label"] = "自定义 SEO 标题"
			legacy := append([]map[string]string{}, defaults[:summary+1]...)
			legacy = append(legacy, defaults[summary+3:]...)
			legacy = append(legacy, defaults[summary+1:summary+3]...)
			save := func(entries []map[string]string) {
				t.Helper()
				encoded, err := json.Marshal(entries)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := database.ExecContext(ctx, `UPDATE gocms_model SET entry_fields = ? WHERE id = ?`, string(encoded), id); err != nil {
					t.Fatal(err)
				}
			}
			check := func(want []map[string]string) {
				t.Helper()
				// Repeated initialization must retain the resulting order.
				for range 2 {
					if err := EnsureModels(ctx, database); err != nil {
						t.Fatal(err)
					}
				}
				if err := database.QueryRowContext(ctx, `SELECT entry_fields FROM gocms_model WHERE id = ?`, id).Scan(&raw); err != nil {
					t.Fatal(err)
				}
				var got []map[string]string
				if err := json.Unmarshal([]byte(raw), &got); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("got %v, want %v", got, want)
				}
			}
			save(legacy)
			check(defaults)
			// An explicitly customized order must survive initialization.
			custom := append([]map[string]string{}, defaults...)
			custom[0], custom[1] = custom[1], custom[0]
			save(custom)
			check(custom)
		})
	}
}
