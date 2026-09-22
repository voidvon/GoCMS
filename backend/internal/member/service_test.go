package member

import (
	"context"
	"strings"
	"testing"

	"gocms/internal/db"
)

func TestMembershipTimeSemantics(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	if err := db.CreateSchema(ctx, database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO gocms_user(id,username,password_hash) VALUES(1,'test','unused')"); err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct {
		slug, start   string
		end           any
		group, status string
	}{
		{"permanent", "2020-01-01 00:00:00", nil, "active", "active"},
		{"expired", "2020-01-01 00:00:00", "2021-01-01T12:00:00+08:00", "active", "expired"},
		{"future", "2099-01-01T00:00:00Z", nil, "active", "scheduled"},
		{"disabled", "2020-01-01 00:00:00", nil, "disabled", "disabled"},
	} {
		result, err := database.Exec("INSERT INTO gocms_user_group(name,slug,status) VALUES(?,?,?)", v.slug, v.slug, v.group)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := result.LastInsertId()
		if _, err := database.Exec("INSERT INTO gocms_user_group_member(user_id,group_id,started_at,expires_at) VALUES(1,?,?,?)", id, v.start, v.end); err != nil {
			t.Fatal(err)
		}
		memberships, err := (Service{DB: database}).Memberships(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		if got := memberships[len(memberships)-1]; got.Status != v.status {
			t.Fatalf("%s: %+v", v.slug, got)
		}
	}
}

func TestAuthenticateMultiSiteScoping(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	if err := db.CreateSchema(ctx, database); err != nil {
		t.Fatal(err)
	}

	svc := Service{DB: database}
	p, err := svc.Register(ctx, "alice", "alice@example.com", "password123", 1)
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	_, token, err := svc.Login(ctx, "alice", "password123", "127.0.0.1", "agent", "", 1)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	// 1. Authenticate on Site 1 should succeed
	auth1, err := svc.Authenticate(ctx, token, 1)
	if err != nil || auth1.ID != p.ID {
		t.Fatalf("expected auth on site 1 to succeed, got %v", err)
	}

	// 2. Authenticate on Site 2 should succeed and auto-join Site 2
	auth2, err := svc.Authenticate(ctx, token, 2)
	if err != nil || auth2.ID != p.ID {
		t.Fatalf("expected auth on site 2 to succeed, got %v", err)
	}
	if auth2.DisplayName != "alice" {
		t.Fatalf("expected display name 'alice', got %q", auth2.DisplayName)
	}

	// 3. Verify user's site_ids in DB contains [1, 2]
	var sidsRaw string
	if err := database.QueryRow("SELECT site_ids FROM gocms_user WHERE id=?", p.ID).Scan(&sidsRaw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sidsRaw, "1") || !strings.Contains(sidsRaw, "2") {
		t.Fatalf("expected site_ids to contain 1 and 2, got %q", sidsRaw)
	}

	// 4. Disable user globally
	if _, err := database.Exec("UPDATE gocms_user SET status='disabled' WHERE id=?", p.ID); err != nil {
		t.Fatal(err)
	}

	// 5. Authenticate on Site 2 and Site 1 should fail
	if _, err = svc.Authenticate(ctx, token, 2); err != ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized on site 2 after disable, got %v", err)
	}
	if _, err = svc.Authenticate(ctx, token, 1); err != ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized on site 1 after disable, got %v", err)
	}

	// 6. Re-enable user and test unified Profile & UpdateProfile
	if _, err := database.Exec("UPDATE gocms_user SET status='active' WHERE id=?", p.ID); err != nil {
		t.Fatal(err)
	}
	prof, err := svc.Profile(ctx, p.ID)
	if err != nil || prof.DisplayName != "alice" {
		t.Fatalf("expected profile to return 'alice', got %q, err: %v", prof.DisplayName, err)
	}
	newName := "AliceGlobal"
	if err := svc.UpdateProfile(ctx, p.ID, &newName, nil); err != nil {
		t.Fatalf("update profile failed: %v", err)
	}
	profUpdated, err := svc.Profile(ctx, p.ID)
	if err != nil || profUpdated.DisplayName != "AliceGlobal" {
		t.Fatalf("expected updated profile to return 'AliceGlobal', got %q", profUpdated.DisplayName)
	}
	authS2Updated, err := svc.Authenticate(ctx, token, 2)
	if err != nil || authS2Updated.DisplayName != "AliceGlobal" {
		t.Fatalf("expected authenticate on site 2 to return 'AliceGlobal', got %q", authS2Updated.DisplayName)
	}
}

func TestRegisterWithDefaultGroup(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	if err := db.CreateSchema(ctx, database); err != nil {
		t.Fatal(err)
	}

	// Insert default groups for site 1 and site 2
	if _, err := database.Exec(`INSERT INTO gocms_user_group(site_id, name, slug, is_default, status) VALUES(1, 'Site1 Default', 'basic', 1, 'active')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO gocms_user_group(site_id, name, slug, is_default, status) VALUES(2, 'Site2 Default', 'site2-basic', 1, 'active')`); err != nil {
		t.Fatal(err)
	}

	svc := Service{DB: database}

	// 1. Register on site 1
	p, err := svc.Register(ctx, "member1", "m1@example.com", "password123", 1)
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	memsSite1, err := svc.Memberships(ctx, p.ID, 1)
	if err != nil {
		t.Fatalf("memberships site 1 failed: %v", err)
	}
	if len(memsSite1) != 1 || memsSite1[0].Slug != "basic" {
		t.Fatalf("expected membership in 'basic', got: %+v", memsSite1)
	}

	// 2. Login specifying site 2 -> auto-joins site 2 and joins site 2 default group
	_, _, err = svc.Login(ctx, "member1", "password123", "127.0.0.1", "test-agent", "", 2)
	if err != nil {
		t.Fatalf("login on site 2 failed: %v", err)
	}

	memsSite2, err := svc.Memberships(ctx, p.ID, 2)
	if err != nil {
		t.Fatalf("memberships site 2 failed: %v", err)
	}
	if len(memsSite2) != 1 || memsSite2[0].Slug != "site2-basic" {
		t.Fatalf("expected membership in 'site2-basic', got: %+v", memsSite2)
	}
}

