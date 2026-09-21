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
