package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
)

// AccessLogTable stores public (front-end) request rows used by the site
// dashboard. Rows are pruned by the access log maintenance loop.
const AccessLogTable = "gocms_access_log"

const AccessLogColumns = `"site_id", "host", "day_key", "path", "query", "method", "status", "kind", "content_type", "ip",
	"referer", "referer_host", "referer_kind", "user_agent", "agent_kind", "agent_label", "device_kind", "visited_at"`

// OpenAccessLogs opens a private database next to the business database and
// copies retained legacy records once. In-memory databases keep their shared
// connection for callers that do not have an on-disk installation.
func OpenAccessLogs(ctx context.Context, business *sql.DB, cutoff string) (_ *sql.DB, err error) {
	var sequence int
	var schema, source string
	if err := business.QueryRowContext(ctx, "PRAGMA database_list").Scan(&sequence, &schema, &source); err != nil {
		return nil, err
	}
	if source == "" {
		return nil, nil
	}
	filename := source + ".access-logs.db"
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err == nil {
		if err := file.Close(); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	dsn := sqliteFileURL(filename)
	dsn.RawQuery = url.Values{"_pragma": {"busy_timeout(1000)"}}.Encode()
	database, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = database.Close()
		}
	}()
	database.SetMaxOpenConns(3)
	database.SetMaxIdleConns(3)
	var mode string
	if err := database.QueryRowContext(ctx, "PRAGMA journal_mode = WAL").Scan(&mode); err != nil {
		return nil, err
	}
	if mode != "wal" {
		return nil, fmt.Errorf("expected access log journal mode WAL, got %q", mode)
	}
	if err := EnsureAccessLog(ctx, database); err != nil {
		return nil, err
	}
	if err := importAccessLogs(ctx, database, source, cutoff); err != nil {
		return nil, fmt.Errorf("import legacy access logs: %w", err)
	}
	return database, nil
}

func importAccessLogs(ctx context.Context, database *sql.DB, source, cutoff string) error {
	if _, err := database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS access_log_migration (id INTEGER PRIMARY KEY CHECK (id=1))`); err != nil {
		return err
	}
	var migrated bool
	if err := database.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM access_log_migration WHERE id=1)`).Scan(&migrated); err != nil || migrated {
		return err
	}
	connection, err := database.Conn(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()
	legacy := sqliteFileURL(source)
	legacy.RawQuery = "mode=ro"
	if _, err := connection.ExecContext(ctx, "ATTACH DATABASE ? AS legacy_access", legacy.String()); err != nil {
		return err
	}
	defer func() {
		_, _ = connection.ExecContext(context.Background(), "DETACH DATABASE legacy_access")
	}()
	transaction, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	marker, err := transaction.ExecContext(ctx, `INSERT OR IGNORE INTO access_log_migration (id) VALUES (1)`)
	if err != nil {
		return err
	}
	affected, err := marker.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 1 {
		if _, err := transaction.ExecContext(ctx, `INSERT INTO main."`+AccessLogTable+`" ("id", `+AccessLogColumns+`)
			SELECT "id", `+AccessLogColumns+` FROM legacy_access."`+AccessLogTable+`" WHERE "visited_at" >= ?`, cutoff); err != nil {
			return err
		}
	}
	return transaction.Commit()
}

// EnsureAccessLog creates the public access log table. The table is
// deliberately site-scoped: every dashboard query filters by site_id, so the
// same database can host many independent public sites.
func EnsureAccessLog(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS "`+AccessLogTable+`" (
		"id" INTEGER PRIMARY KEY AUTOINCREMENT,
		"site_id" INTEGER NOT NULL DEFAULT 1,
		"host" TEXT NOT NULL DEFAULT '',
		"day_key" TEXT NOT NULL DEFAULT '',
		"path" TEXT NOT NULL DEFAULT '/',
		"query" TEXT NOT NULL DEFAULT '',
		"method" TEXT NOT NULL DEFAULT 'GET',
		"status" INTEGER NOT NULL DEFAULT 200,
		"kind" TEXT NOT NULL DEFAULT 'page',
		"content_type" TEXT NOT NULL DEFAULT '',
		"ip" TEXT NOT NULL DEFAULT '',
		"referer" TEXT NOT NULL DEFAULT '',
		"referer_host" TEXT NOT NULL DEFAULT '',
		"referer_kind" TEXT NOT NULL DEFAULT 'none',
		"user_agent" TEXT NOT NULL DEFAULT '',
		"agent_kind" TEXT NOT NULL DEFAULT 'other',
		"agent_label" TEXT NOT NULL DEFAULT '',
		"device_kind" TEXT NOT NULL DEFAULT 'other',
		"visited_at" TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return err
	}
	// Existing installations predate the request-host column. Add it in place
	// so historical rows stay readable (host='') and new rows can distinguish
	// the main domain from standalone language domains.
	if err := ensureTableColumn(ctx, database, AccessLogTable, "host", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS "idx_gocms_access_log_site_time" ON "` + AccessLogTable + `" ("site_id", "id" DESC)`,
		`CREATE INDEX IF NOT EXISTS "idx_gocms_access_log_site_day" ON "` + AccessLogTable + `" ("site_id", "day_key")`,
		`CREATE INDEX IF NOT EXISTS "idx_gocms_access_log_site_status" ON "` + AccessLogTable + `" ("site_id", "status", "id" DESC)`,
		`CREATE INDEX IF NOT EXISTS "idx_gocms_access_log_site_host" ON "` + AccessLogTable + `" ("site_id", "host", "status", "id" DESC)`,
		`CREATE INDEX IF NOT EXISTS "idx_gocms_access_log_site_path" ON "` + AccessLogTable + `" ("site_id", "path", "status")`,
		`CREATE INDEX IF NOT EXISTS "idx_gocms_access_log_site_host_path" ON "` + AccessLogTable + `" ("site_id", "host", "path", "status")`,
		`CREATE INDEX IF NOT EXISTS "idx_gocms_access_log_site_ip" ON "` + AccessLogTable + `" ("site_id", "ip")`,
		`CREATE INDEX IF NOT EXISTS "idx_gocms_access_log_visited_at" ON "` + AccessLogTable + `" ("visited_at")`,
	}
	for _, statement := range indexes {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
