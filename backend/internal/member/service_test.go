package member

import (
	"context"
	"gocms/internal/db"
	"testing"
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

	// 2. Authenticate on Site 2 should fail (not a member of site 2)
	_, err = svc.Authenticate(ctx, token, 2)
	if err != ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized on site 2, got %v", err)
	}

	// 3. Add to Site 2
	if _, err := database.Exec("INSERT INTO gocms_site_member(site_id, user_id, display_name, status) VALUES(2, ?, 'AliceSub', 'active')", p.ID); err != nil {
		t.Fatal(err)
	}

	// 4. Authenticate on Site 2 should now succeed with Site 2 display name
	auth2, err := svc.Authenticate(ctx, token, 2)
	if err != nil || auth2.ID != p.ID {
		t.Fatalf("expected auth on site 2 to succeed, got %v", err)
	}
	if auth2.DisplayName != "AliceSub" {
		t.Fatalf("expected display name 'AliceSub', got %q", auth2.DisplayName)
	}

	// 5. Ban/disable member on Site 2
	if _, err := database.Exec("UPDATE gocms_site_member SET status='disabled' WHERE site_id=2 AND user_id=?", p.ID); err != nil {
		t.Fatal(err)
	}

	// 6. Authenticate on Site 2 should fail
	_, err = svc.Authenticate(ctx, token, 2)
	if err != ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized on site 2 after ban, got %v", err)
	}

	// 7. Authenticate on Site 1 should still succeed
	auth1After, err := svc.Authenticate(ctx, token, 1)
	if err != nil || auth1After.ID != p.ID {
		t.Fatalf("expected auth on site 1 to still succeed, got %v", err)
	}
}
