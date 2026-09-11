package apikey

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"gocms/internal/db"
)

func setupTestDB(t *testing.T) (*sql.DB, int64) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := db.CreateSchema(ctx, database); err != nil {
		database.Close()
		t.Fatal(err)
	}

	result, err := database.ExecContext(ctx, `
		INSERT INTO "gocms_admin_user" ("username", "password_hash", "flags")
		VALUES ('admin', 'hashed', 'all')`)
	if err != nil {
		database.Close()
		t.Fatal(err)
	}

	adminID, err := result.LastInsertId()
	if err != nil {
		database.Close()
		t.Fatal(err)
	}

	return database, adminID
}

func TestCreateAndAuthenticate(t *testing.T) {
	database, adminID := setupTestDB(t)
	defer database.Close()
	ctx := context.Background()

	created, err := Create(ctx, database, adminID, adminID, CreateInput{
		Name: "Content Sync Key",
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	if !strings.HasPrefix(created.Key, KeyPrefix) {
		t.Fatalf("key %q does not have prefix %q", created.Key, KeyPrefix)
	}
	if created.Status != "active" {
		t.Fatalf("expected status 'active', got %q", created.Status)
	}
	if created.Name != "Content Sync Key" {
		t.Fatalf("expected name 'Content Sync Key', got %q", created.Name)
	}

	ident, err := Authenticate(ctx, database, created.Key)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if ident == nil {
		t.Fatal("expected authenticated identity, got nil")
	}
	if ident.AdminID != adminID || ident.Username != "admin" {
		t.Fatalf("expected admin %d (admin), got %d (%s)", adminID, ident.AdminID, ident.Username)
	}

	// Invalid token
	invalid, err := Authenticate(ctx, database, created.Key+"invalid")
	if err != nil {
		t.Fatalf("authenticate invalid: %v", err)
	}
	if invalid != nil {
		t.Fatalf("expected nil identity for invalid key, got %+v", invalid)
	}
}

func TestRotateApiKey(t *testing.T) {
	database, adminID := setupTestDB(t)
	defer database.Close()
	ctx := context.Background()

	created, err := Create(ctx, database, adminID, adminID, CreateInput{
		Name: "Rotatable Key",
	}, "192.168.1.100")
	if err != nil {
		t.Fatal(err)
	}

	rotated, err := Rotate(ctx, database, created.ID, adminID, "192.168.1.101")
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}

	if rotated.ID == created.ID {
		t.Fatalf("expected new key id, got same %d", rotated.ID)
	}
	if rotated.Key == created.Key {
		t.Fatal("expected new key string, got same")
	}

	// Old key must no longer authenticate
	oldAuth, err := Authenticate(ctx, database, created.Key)
	if err != nil {
		t.Fatal(err)
	}
	if oldAuth != nil {
		t.Fatal("old key must not authenticate after rotation")
	}

	// New key must authenticate
	newAuth, err := Authenticate(ctx, database, rotated.Key)
	if err != nil {
		t.Fatal(err)
	}
	if newAuth == nil {
		t.Fatal("new key must authenticate")
	}

	// Rotating old key again must fail with ErrRevoked
	_, err = Rotate(ctx, database, created.ID, adminID, "127.0.0.1")
	if err != ErrRevoked {
		t.Fatalf("expected ErrRevoked, got %v", err)
	}
}

func TestRevokeApiKey(t *testing.T) {
	database, adminID := setupTestDB(t)
	defer database.Close()
	ctx := context.Background()

	created, err := Create(ctx, database, adminID, adminID, CreateInput{
		Name: "To Revoke",
	}, "10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	revoked, err := Revoke(ctx, database, created.ID, adminID, "10.0.0.2", "security breach")
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if revoked.Status != "revoked" {
		t.Fatalf("expected status 'revoked', got %q", revoked.Status)
	}
	if revoked.RevokedAt == nil {
		t.Fatal("expected revoked_at to be set")
	}

	auth, err := Authenticate(ctx, database, created.Key)
	if err != nil {
		t.Fatal(err)
	}
	if auth != nil {
		t.Fatal("revoked key should not authenticate")
	}

	events, err := ListEvents(ctx, database, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events (created, revoked), got %d", len(events))
	}
	if events[0].EventType != "revoked" {
		t.Fatalf("expected latest event 'revoked', got %q", events[0].EventType)
	}
	if reason, ok := events[0].Metadata["reason"].(string); !ok || reason != "security breach" {
		t.Fatalf("expected reason 'security breach', got %v", events[0].Metadata["reason"])
	}
}

func TestTouchUsage(t *testing.T) {
	database, adminID := setupTestDB(t)
	defer database.Close()
	ctx := context.Background()

	created, err := Create(ctx, database, adminID, adminID, CreateInput{
		Name: "Usage Key",
	}, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	TouchUsage(ctx, database, created.ID, "203.0.113.195")

	fetched, err := GetByID(ctx, database, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.LastUsedAt == nil {
		t.Fatal("expected last_used_at to be set")
	}
	if fetched.LastUsedIP != "203.0.113.195" {
		t.Fatalf("expected last_used_ip '203.0.113.195', got %q", fetched.LastUsedIP)
	}
}

func TestExpiration(t *testing.T) {
	database, adminID := setupTestDB(t)
	defer database.Close()
	ctx := context.Background()

	// Past expiration should fail validation
	past := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)
	_, err := Create(ctx, database, adminID, adminID, CreateInput{
		Name:      "Past Key",
		ExpiresAt: &past,
	}, "127.0.0.1")
	if err != ErrExpiresPast {
		t.Fatalf("expected ErrExpiresPast, got %v", err)
	}

	// Future expiration works
	future := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	created, err := Create(ctx, database, adminID, adminID, CreateInput{
		Name:      "Future Key",
		ExpiresAt: &future,
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("create future key: %v", err)
	}
	if created.Status != "active" {
		t.Fatalf("expected status 'active', got %q", created.Status)
	}

	// Force expire in database
	pastTime := time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339)
	_, err = database.ExecContext(ctx, `UPDATE "`+db.ApiKeyTable+`" SET "expires_at" = ? WHERE "id" = ?`, pastTime, created.ID)
	if err != nil {
		t.Fatal(err)
	}

	auth, err := Authenticate(ctx, database, created.Key)
	if err != nil {
		t.Fatal(err)
	}
	if auth != nil {
		t.Fatal("expired key must not authenticate")
	}

	expiredKey, err := GetByID(ctx, database, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if expiredKey.Status != "expired" {
		t.Fatalf("expected status 'expired', got %q", expiredKey.Status)
	}

	// Rotating expired key must fail with ErrExpired
	_, err = Rotate(ctx, database, created.ID, adminID, "127.0.0.1")
	if err != ErrExpired {
		t.Fatalf("expected ErrExpired, got %v", err)
	}
}
