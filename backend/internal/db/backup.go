package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"modernc.org/sqlite"
)

func EnsureDatabaseBackup(ctx context.Context, database *sql.DB) error {
	_, err := database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS gocms_database_backup (
		id INTEGER PRIMARY KEY CHECK (id=1),
		namespace TEXT NOT NULL,
		endpoint TEXT NOT NULL,
		bucket TEXT NOT NULL,
		region TEXT NOT NULL,
		access_key TEXT NOT NULL,
		secret_ciphertext TEXT NOT NULL,
		automatic INTEGER NOT NULL,
		daily_at TEXT NOT NULL,
		time_zone TEXT NOT NULL,
		last_scheduled_day TEXT NOT NULL DEFAULT '',
		last_run_at TEXT NOT NULL DEFAULT '',
		last_source TEXT NOT NULL DEFAULT '',
		last_state TEXT NOT NULL DEFAULT '',
		last_message TEXT NOT NULL DEFAULT ''
	)`)
	return err
}

// Snapshot copies the main SQLite database, including committed WAL pages and
// original rowids. For an on-disk database, use a separate reader connection.
func Snapshot(ctx context.Context, database *sql.DB, filename string) error {
	var sequence int
	var schema, source string
	if err := database.QueryRowContext(ctx, "PRAGMA database_list").Scan(&sequence, &schema, &source); err != nil {
		return err
	}
	if schema != "main" {
		return fmt.Errorf("expected SQLite main database, got %q", schema)
	}
	if source != "" {
		dsn := sqliteFileURL(source)
		dsn.RawQuery = url.Values{"mode": {"ro"}, "_pragma": {"busy_timeout(5000)"}}.Encode()
		reader, err := sql.Open("sqlite", dsn.String())
		if err != nil {
			return err
		}
		defer reader.Close()
		database = reader
	}
	filename, err := filepath.Abs(filename)
	if err != nil {
		return err
	}
	dest := sqliteFileURL(filename)
	conn, err := database.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	return conn.Raw(func(driverConn any) (err error) {
		source, ok := driverConn.(interface {
			NewBackup(string) (*sqlite.Backup, error)
		})
		if !ok {
			return errors.New("SQLite online backup is unavailable")
		}
		backup, err := source.NewBackup(dest.String())
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, backup.Finish()) }()
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			more, err := backup.Step(512)
			if err != nil || !more {
				return err
			}
		}
	})
}

func sqliteFileURL(filename string) url.URL {
	path := filepath.ToSlash(filename)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return url.URL{Scheme: "file", Path: path}
}
