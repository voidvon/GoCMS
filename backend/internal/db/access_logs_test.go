package db

import (
	"context"
	"path/filepath"
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

func TestOpenAccessLogsMigratesRetainedRowsOnce(t *testing.T) {
	ctx := context.Background()
	business, err := Open(filepath.Join(t.TempDir(), "site #1.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer business.Close()
	if err := EnsureAccessLog(ctx, business); err != nil {
		t.Fatal(err)
	}
	if _, err := business.Exec(`INSERT INTO gocms_access_log (id, site_id, host, path, visited_at) VALUES
		(10, 4, 'one.test', '/retained', '2026-11-20 00:00:00'),
		(11, 5, 'two.test', '/retained', '2026-11-21 00:00:00'),
		(12, 4, 'one.test', '/expired', '2026-11-01 00:00:00')`); err != nil {
		t.Fatal(err)
	}
	const cutoff = "2026-11-18 00:00:00"
	logs, err := OpenAccessLogs(ctx, business, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := logs.QueryRow(`SELECT COUNT(*) FROM gocms_access_log`).Scan(&count); err != nil || count != 2 {
		logs.Close()
		t.Fatalf("migrated count=%d, err=%v", count, err)
	}
	var siteID int
	var host string
	if err := logs.QueryRow(`SELECT site_id, host FROM gocms_access_log WHERE id=11`).Scan(&siteID, &host); err != nil || siteID != 5 || host != "two.test" {
		logs.Close()
		t.Fatalf("migrated row site=%d, host=%q, err=%v", siteID, host, err)
	}
	_, err = logs.Exec(`DELETE FROM gocms_access_log WHERE id=10`)
	logs.Close()
	if err != nil {
		t.Fatal(err)
	}
	logs, err = OpenAccessLogs(ctx, business, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	if err := logs.QueryRow(`SELECT COUNT(*) FROM gocms_access_log`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("count after reopening=%d, err=%v", count, err)
	}
	if err := business.QueryRow(`SELECT COUNT(*) FROM gocms_access_log`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("legacy count=%d, err=%v", count, err)
	}
}
