package member

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"gocms/internal/db"
)

func TestLoginGrantAndTransactionalFailure(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.CreateSchema(ctx, database); err != nil {
		t.Fatal(err)
	}
	service := Service{DB: database}
	profile, err := service.Register(ctx, "grantuser", "", "password123", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE gocms_user SET max_sessions=1 WHERE id=?;
INSERT INTO gocms_site(name,code) VALUES('Second','second');
INSERT INTO gocms_user_group(site_id,name,slug,is_default) VALUES(2,'Default','second-default',1)`, profile.ID); err != nil {
		t.Fatal(err)
	}
	input := LoginInput{Identifier: profile.Username, Password: "password123", SiteID: 1}
	login, err := service.Login(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	var expires int64
	if err := database.QueryRow(`SELECT expires_at FROM gocms_user_session WHERE token_hash=?`, TokenHash(login.Session.Token)).Scan(&expires); err != nil {
		t.Fatal(err)
	}
	if login.User.ID != profile.ID || login.Session.ExpiresAt.Unix() != expires || time.Until(login.Session.ExpiresAt) > SessionLifetime {
		t.Fatal("grant differs from committed session")
	}
	identity, err := service.Authenticate(ctx, login.Session.Token, 1)
	if err != nil || identity.ID != profile.ID || identity.SessionID != SessionID(TokenHash(login.Session.Token)) || !identity.ExpiresAt.Equal(login.Session.ExpiresAt) || !identity.HasPassword {
		t.Fatal("authentication lost session identity", err)
	}
	for _, value := range []any{login, login.Session, identity} {
		encoded, err := json.Marshal(value)
		if err != nil || strings.Contains(string(encoded), login.Session.Token) || strings.Contains(string(encoded), TokenHash(login.Session.Token)) {
			t.Fatal("application result exposed a session credential", err)
		}
	}
	// A rejected new login must not join another site or assign its default group.
	input.SiteID = 2
	if _, err := service.Login(ctx, input); !errors.Is(err, ErrSessionLimit) {
		t.Fatal("missing session limit", err)
	}
	var sites string
	var groups int
	if err := database.QueryRow(`SELECT site_ids FROM gocms_user WHERE id=?`, profile.ID).Scan(&sites); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM gocms_user_group_member WHERE user_id=?`, profile.ID).Scan(&groups); err != nil {
		t.Fatal(err)
	}
	if sites != "[1]" || groups != 0 {
		t.Fatal("rejected login committed site or group membership")
	}
	// A failure while preparing public data must roll back rotation, preserving
	// both the old credential and its session slot.
	if _, err := database.Exec(`INSERT INTO gocms_user_group(site_id,name,slug) VALUES(1,'Broken','broken');
INSERT INTO gocms_user_group_member(user_id,group_id,started_at) SELECT ?,id,'invalid-time' FROM gocms_user_group WHERE slug='broken'`, profile.ID); err != nil {
		t.Fatal(err)
	}
	input.SiteID, input.CurrentToken = 1, login.Session.Token
	failed, err := service.Login(ctx, input)
	if err == nil || failed.Session.Token != "" || failed.User.ID != 0 {
		t.Fatal("failed login exposed an uncommitted grant")
	}
	if _, err := service.Authenticate(ctx, login.Session.Token, 1); err != nil {
		t.Fatal("failed rotation destroyed the old session", err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM gocms_user_session WHERE user_id=?`, profile.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("failed rotation changed session count", err)
	}
}
