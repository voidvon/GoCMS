package db

import (
	"context"
	"testing"
)

func TestAdminMigrationPreservesExistingAccessOnlyOnce(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	_, err = database.Exec(`CREATE TABLE gocms_admin_user (id INTEGER PRIMARY KEY, username TEXT, password_hash TEXT, flags TEXT, last_login TEXT, last_login_ip TEXT);
	INSERT INTO gocms_admin_user (username) VALUES ('existing')`)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := EnsureAdminUsers(ctx, database); err != nil {
		t.Fatal(err)
	}
	var super bool
	if err := database.QueryRow(`SELECT is_super FROM gocms_admin_user WHERE username = 'existing'`).Scan(&super); err != nil || !super {
		t.Fatalf("legacy access not preserved: %v, %v", super, err)
	}
	if _, err := database.Exec(`INSERT INTO gocms_admin_user (username) VALUES ('restricted'); UPDATE gocms_admin_user SET is_super = 0`); err != nil {
		t.Fatal(err)
	}
	if err := EnsureAdminUsers(ctx, database); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM gocms_admin_user WHERE is_super != 0`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("restart promoted accounts: %d, %v", count, err)
	}
}
