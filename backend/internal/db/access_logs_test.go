package db

import (
	"context"
	"testing"
)

func TestEnsureAccessLogAddsHostToExistingTable(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	if err := EnsureAccessLog(ctx, database); err != nil {
		t.Fatal(err)
	}
	// Simulate an installation created before the request-host column existed.
	for _, statement := range []string{
		`DROP INDEX IF EXISTS "idx_gocms_access_log_site_host"`,
		`DROP INDEX IF EXISTS "idx_gocms_access_log_site_host_path"`,
		`ALTER TABLE "` + AccessLogTable + `" DROP COLUMN "host"`,
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := EnsureAccessLog(ctx, database); err != nil {
		t.Fatal(err)
	}
	var columns int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('`+AccessLogTable+`') WHERE name = 'host'`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if columns != 1 {
		t.Fatalf("host column count = %d, want 1", columns)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO "`+AccessLogTable+`" ("site_id", "path") VALUES (1, '/legacy')`); err != nil {
		t.Fatal(err)
	}
	var host string
	if err := database.QueryRowContext(ctx, `SELECT "host" FROM "`+AccessLogTable+`" WHERE "path" = '/legacy'`).Scan(&host); err != nil {
		t.Fatal(err)
	}
	if host != "" {
		t.Fatalf("legacy row host = %q, want empty", host)
	}
}
