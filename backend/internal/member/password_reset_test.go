package member

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"gocms/internal/db"
	"gocms/internal/membermail"
	"gocms/internal/memberoauth"
)

func TestPasswordRecoveryLifecycle(t *testing.T) {
	ctx := context.Background()
	database, e := db.Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer database.Close()
	for _, ensure := range []func(context.Context, *sql.DB) error{db.CreateSchema, memberoauth.Ensure, membermail.Ensure, EnsurePasswordRecovery} {
		if e = ensure(ctx, database); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = database.Exec(`UPDATE gocms_site SET domain='a.example' WHERE id=1;INSERT INTO gocms_site(name,code,domain) VALUES('B','b','b.example');INSERT INTO gocms_platform_mail_config VALUES(1,'{"enabled":true}','','mail-v1')`); e != nil {
		t.Fatal(e)
	}
	svc := Service{DB: database}
	p, e := svc.Register(ctx, "recover", "person@example.com", "password123", 1)
	if e != nil {
		t.Fatal(e)
	}
	_, session, e := svc.Login(ctx, "recover", "password123", "127.0.0.1", "test", "", 1)
	if e != nil {
		t.Fatal(e)
	}
	issue := func() string {
		t.Helper()
		token, id, e := svc.IssuePasswordReset(ctx, 1, "person@example.com", "mail-v1")
		if e != nil || id != p.ID || len(token) != 43 {
			t.Fatalf("issue: %s %d %v", token, id, e)
		}
		return token
	}
	for _, tc := range []struct {
		email string
		site  int64
	}{{"unknown@example.com", 1}, {"person@example.com", 2}} {
		token, _, e := svc.IssuePasswordReset(ctx, tc.site, tc.email, "mail-v1")
		if e != nil || token != "" {
			t.Fatal("unknown or unrelated account eligible", e)
		}
	}
	first := issue()
	token := issue()
	if e = svc.ResetPassword(ctx, 1, first, "newpassword123"); !errors.Is(e, ErrResetToken) {
		t.Fatal("superseded link accepted", e)
	}
	if e = svc.ResetPassword(ctx, 2, token, "newpassword123"); !errors.Is(e, ErrResetToken) {
		t.Fatal("wrong site accepted", e)
	}
	var stored string
	if e = database.QueryRow(`SELECT token_hash FROM gocms_member_password_reset`).Scan(&stored); e != nil || stored == token || stored != TokenHash(token) {
		t.Fatal("token not hashed", e)
	}
	if e = svc.ResetPassword(ctx, 1, token, "newpassword123"); e != nil {
		t.Fatal(e)
	}
	if e = svc.ResetPassword(ctx, 1, token, "anotherpassword"); !errors.Is(e, ErrResetToken) {
		t.Fatal("replay accepted", e)
	}
	if _, e = svc.Authenticate(ctx, session, 1); e == nil {
		t.Fatal("old session survived")
	}
	if _, _, e = svc.Login(ctx, "recover", "password123", "", "", "", 1); e == nil {
		t.Fatal("old password survived")
	}
	if _, _, e = svc.Login(ctx, "recover", "newpassword123", "", "", "", 1); e != nil {
		t.Fatal(e)
	}
	for _, scenario := range []string{"expired", "disabled-user", "email-change", "password-change", "mail-change"} {
		t.Run(scenario, func(t *testing.T) {
			token := issue()
			switch scenario {
			case "expired":
				database.Exec(`UPDATE gocms_member_password_reset SET expires_at=1`)
			case "disabled-user":
				database.Exec(`UPDATE gocms_user SET status='disabled' WHERE id=?`, p.ID)
			case "email-change":
				database.Exec(`UPDATE gocms_user SET email='different@example.com' WHERE id=?`, p.ID)
			case "password-change":
				if e := svc.ChangePassword(ctx, p.ID, "newpassword123", "newpassword123"); e != nil {
					t.Fatal(e)
				}
			case "mail-change":
				database.Exec(`UPDATE gocms_platform_mail_config SET revision='changed'`)
			}
			if e := svc.ResetPassword(ctx, 1, token, "shouldnotwork"); !errors.Is(e, ErrResetToken) {
				t.Fatal("stale proof accepted", e)
			}
			database.Exec(`UPDATE gocms_user SET email='person@example.com',status='active' WHERE id=?`, p.ID)
			database.Exec(`UPDATE gocms_platform_mail_config SET revision='mail-v1'`)
		})
	}
}
func TestLinkedProviderPasswordRecovery(t *testing.T) {
	ctx := context.Background()
	database, e := db.Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer database.Close()
	for _, ensure := range []func(context.Context, *sql.DB) error{db.CreateSchema, memberoauth.Ensure, membermail.Ensure, EnsurePasswordRecovery} {
		if e = ensure(ctx, database); e != nil {
			t.Fatal(e)
		}
	}
	svc := Service{DB: database}
	p, e := svc.Register(ctx, "linked", "", "password123", 1)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = database.Exec(`UPDATE gocms_user SET password_hash='' WHERE id=?;INSERT INTO gocms_platform_oauth_config(provider,enabled,auto_register,client_id,revision) VALUES('google',1,1,'test.apps.googleusercontent.com','g1'),('facebook',1,1,'123456','f1'),('microsoft',1,1,'ms-client','m1');INSERT INTO gocms_platform_identity(user_id,provider,issuer,subject) VALUES(?,'google','https://accounts.google.com','linked-sub'),(?,'facebook','https://www.facebook.com/123456','linked-sub'),(?,'microsoft','https://login.microsoftonline.com/ms-client','linked-sub')`, p.ID, p.ID, p.ID, p.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.IssueExternalPasswordReset(ctx, 1, "google", "g1", "unbound"); e == nil {
		t.Fatal("unbound identity eligible")
	}
	var count int
	database.QueryRow(`SELECT COUNT(*) FROM gocms_user`).Scan(&count)
	if count != 1 {
		t.Fatal("recovery registered a member")
	}
	for _, provider := range []string{"google", "facebook", "microsoft"} {
		t.Run(provider, func(t *testing.T) {
			rev := "g1"
			if provider == "facebook" {
				rev = "f1"
			}
			if provider == "microsoft" {
				rev = "m1"
			}
			issue := func() string {
				t.Helper()
				token, e := svc.IssueExternalPasswordReset(ctx, 1, provider, rev, "linked-sub")
				if e != nil {
					t.Fatal(e)
				}
				return token
			}
			token := issue()
			if e := svc.ResetPassword(ctx, 1, token, "fromthirdparty"); e != nil {
				t.Fatal("recovery needs no email", e)
			}
			if _, _, e := svc.Login(ctx, "linked", "fromthirdparty", "", "", "", 1); e != nil {
				t.Fatal(e)
			}
			token = issue()
			database.Exec(`UPDATE gocms_platform_oauth_config SET enabled=0 WHERE provider=?`, provider)
			if e := svc.ResetPassword(ctx, 1, token, "shouldnotwork"); !errors.Is(e, ErrResetToken) {
				t.Fatal("disabled provider accepted", e)
			}
			database.Exec(`UPDATE gocms_platform_oauth_config SET enabled=1 WHERE provider=?`, provider)
			token = issue()
			database.Exec(`DELETE FROM gocms_platform_identity WHERE provider=?`, provider)
			if e := svc.ResetPassword(ctx, 1, token, "shouldnotwork"); !errors.Is(e, ErrResetToken) {
				t.Fatal("unlinked identity accepted", e)
			}
		})
	}
}
