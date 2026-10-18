package db

import (
	"context"
	"database/sql"
)

// AccessLogTable stores public (front-end) request rows used by the site
// dashboard. Rows are pruned by the access log maintenance loop.
const AccessLogTable = "gocms_access_log"

// EnsureAccessLog creates the public access log table. The table is
// deliberately site-scoped: every dashboard query filters by site_id, so the
// same database can host many independent public sites.
func EnsureAccessLog(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS "`+AccessLogTable+`" (
		"id" INTEGER PRIMARY KEY AUTOINCREMENT,
		"site_id" INTEGER NOT NULL DEFAULT 1,
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
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS "idx_gocms_access_log_site_time" ON "` + AccessLogTable + `" ("site_id", "id" DESC)`,
		`CREATE INDEX IF NOT EXISTS "idx_gocms_access_log_site_day" ON "` + AccessLogTable + `" ("site_id", "day_key")`,
		`CREATE INDEX IF NOT EXISTS "idx_gocms_access_log_site_status" ON "` + AccessLogTable + `" ("site_id", "status", "id" DESC)`,
		`CREATE INDEX IF NOT EXISTS "idx_gocms_access_log_site_path" ON "` + AccessLogTable + `" ("site_id", "path", "status")`,
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
