package db

import (
	"context"
	"strings"
	"testing"
)

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
		"gocms_site_setting":        false,
		"gocms_admin_user":          false,
		"gocms_category":            false,
		"gocms_content":             false,
		"gocms_media":               false,
		"gocms_media_ref":           false,
		"gocms_message":             false,
		"gocms_template_assignment": false,
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
