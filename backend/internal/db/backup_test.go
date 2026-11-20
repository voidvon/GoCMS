package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSnapshotRetriesAfterWriterLock(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	dir := t.TempDir()
	source := filepath.Join(dir, "live.db")
	database, err := Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.ExecContext(ctx, "CREATE TABLE items (value TEXT)"); err != nil {
		t.Fatal(err)
	}
	writer, err := Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	conn, err := writer.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN EXCLUSIVE; INSERT INTO items VALUES ('committed')"); err != nil {
		t.Fatal(err)
	}

	commit := make(chan error, 1)
	timer := time.AfterFunc(5500*time.Millisecond, func() {
		_, err := conn.ExecContext(ctx, "COMMIT")
		commit <- err
	})
	defer func() {
		if !timer.Stop() {
			if err := <-commit; err != nil {
				t.Error(err)
			}
		}
	}()

	filename := filepath.Join(dir, "snapshot.db")
	if err := Snapshot(ctx, database, filename); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	var integrity, value string
	if err := snapshot.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("snapshot integrity: %q, %v", integrity, err)
	}
	if err := snapshot.QueryRowContext(ctx, "SELECT value FROM items").Scan(&value); err != nil || value != "committed" {
		t.Fatalf("snapshot did not contain the committed write: %q, %v", value, err)
	}
}
