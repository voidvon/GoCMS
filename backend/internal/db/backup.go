package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

const snapshotBusyWindow = 15 * time.Second

// ErrSnapshotBusy reports that the snapshot could not continue after lock retries.
var ErrSnapshotBusy = errors.New("SQLite snapshot remains busy")

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

// Snapshot creates a compact copy of the main database in a new private file.
// VACUUM INTO uses a consistent read transaction, includes committed WAL
// pages and preserves implicit rowids.
func Snapshot(ctx context.Context, database *sql.DB, filename string) (err error) {
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
		dsn.RawQuery = url.Values{"mode": {"ro"}, "_pragma": {"busy_timeout(1000)"}}.Encode()
		reader, err := sql.Open("sqlite", dsn.String())
		if err != nil {
			return err
		}
		defer reader.Close()
		database = reader
	}
	filename, err = filepath.Abs(filename)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.Remove(filename))
		}
	}()
	if err := file.Close(); err != nil {
		return err
	}

	var busyUntil time.Time
	for {
		_, err := database.ExecContext(ctx, "VACUUM INTO ?", filename)
		if err == nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		var sqliteErr *sqlite.Error
		if !errors.As(err, &sqliteErr) {
			return err
		}
		code := sqliteErr.Code() & 0xff
		if code != sqlite3.SQLITE_BUSY && code != sqlite3.SQLITE_LOCKED {
			return err
		}
		if busyUntil.IsZero() {
			busyUntil = time.Now().Add(snapshotBusyWindow)
		}
		remaining := time.Until(busyUntil)
		if remaining <= 0 {
			return fmt.Errorf("%w: %w", ErrSnapshotBusy, err)
		}
		// VACUUM INTO requires an empty destination and may have created pages
		// before failing. Only the private file created above is reset here.
		if err := os.Truncate(filename, 0); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(min(250*time.Millisecond, remaining)):
		}
	}
}

func sqliteFileURL(filename string) url.URL {
	path := filepath.ToSlash(filename)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return url.URL{Scheme: "file", Path: path}
}
