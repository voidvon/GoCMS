package db

import (
	"context"
	"database/sql"
)

// AccessLogTable stores public (front-end) request rows used by the site
// dashboard. Rows are pruned by the access log maintenance loop.
const AccessLogTable = "gocms_access_log"

// AccessLogIgnoreTable stores path-only suppressions for the dashboard error
// statistics. Rules are scoped to one site but apply to every domain/alias of
// that site, which is what operators expect for scanner paths such as
// /wp-admin/install.php.
const AccessLogIgnoreTable = "gocms_access_log_ignore"

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
	return EnsureAccessLogIgnore(ctx, database)
}

// EnsureAccessLogIgnore creates the per-site ignored-path table. Ignore rules
// are independent of the access log retention window: they survive log cleanup
// so an ignored scanner path does not reappear after three days.
func EnsureAccessLogIgnore(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS "`+AccessLogIgnoreTable+`" (
		"site_id" INTEGER NOT NULL DEFAULT 1,
		"path" TEXT NOT NULL,
		"created_at" TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY ("site_id", "path")
	)`); err != nil {
		return err
	}
	return nil
}
