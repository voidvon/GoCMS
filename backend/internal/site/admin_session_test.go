package site

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gocms/internal/apikey"
	"gocms/internal/db"
)

func TestAdminSessionRenewal(t *testing.T) {
	for _, tc := range []struct {
		name       string
		idle       time.Duration
		absolute   time.Duration
		background bool
		status     int
		renew      bool
	}{
		{"active", time.Hour, 6 * 24 * time.Hour, false, 200, true},
		{"recently renewed", 24*time.Hour - time.Minute, 6 * 24 * time.Hour, false, 200, false},
		{"background poll", time.Hour, 6 * 24 * time.Hour, true, 200, false},
		{"expired background poll", -time.Second, 6 * 24 * time.Hour, true, 401, false},
		{"idle expired", -time.Second, 6 * 24 * time.Hour, false, 401, false},
		{"absolute expired", time.Hour, -time.Second, false, 401, false},
		{"near absolute limit", time.Hour, 2 * time.Hour, false, 200, true},
		{"at absolute limit", time.Hour, time.Hour, false, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, database, token := newCategoryTestServer(t)
			now := time.Now().Unix()
			idle, absolute := now+int64(tc.idle/time.Second), now+int64(tc.absolute/time.Second)
			if _, err := database.Exec(`UPDATE gocms_admin_session SET expires_at=?,absolute_expires_at=? WHERE token=?`, idle, absolute, token); err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodGet, "/api/admin/session", nil)
			r.AddCookie(sessionCookie(token, 86400))
			if tc.background {
				r.Header.Set("X-GoCMS-Background", "1")
			}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			var got, gotAbsolute int64
			if err := database.QueryRow(`SELECT expires_at,absolute_expires_at FROM gocms_admin_session WHERE token=?`, token).Scan(&got, &gotAbsolute); err != nil {
				t.Fatal(err)
			}
			if gotAbsolute != absolute {
				t.Fatal("renewal moved absolute expiry")
			}
			cookies := w.Result().Cookies()
			if !tc.renew {
				if got != idle || len(cookies) != 0 {
					t.Fatalf("unexpected renewal: expiry=%d cookies=%v", got, cookies)
				}
				return
			}
			want := min(now+86400, absolute)
			if got < want || got > want+2 || got > absolute {
				t.Fatalf("expiry %d, want approximately %d", got, want)
			}
			if len(cookies) != 1 || cookies[0].Value != token || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].MaxAge < int(want-now)-2 || cookies[0].MaxAge > int(want-now)+2 {
				t.Fatalf("cookie not synchronized: %v", cookies)
			}
		})
	}
}

func TestAdminSessionCreationAndAPIKeyIsolation(t *testing.T) {
	s, database, token := newCategoryTestServer(t)
	var idle, absolute, adminID int64
	if err := database.QueryRow(`SELECT expires_at,absolute_expires_at FROM gocms_admin_session WHERE token=?`, token).Scan(&idle, &absolute); err != nil {
		t.Fatal(err)
	}
	if absolute-idle != 6*86400 || idle < time.Now().Unix()+86400-2 {
		t.Fatalf("incorrect new session lifetime: %d %d", idle, absolute)
	}
	if err := database.QueryRow(`SELECT id FROM gocms_admin_user WHERE username='gocms'`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	key, err := apikey.Create(context.Background(), database, adminID, adminID, apikey.CreateInput{Name: "session isolation"}, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	idle = time.Now().Add(time.Hour).Unix()
	if _, err := database.Exec(`UPDATE gocms_admin_session SET expires_at=? WHERE token=?`, idle, token); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/admin/session", nil)
	r.AddCookie(sessionCookie(token, 86400))
	r.Header.Set("Authorization", "Bearer "+key.Key)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("API key request renewed browser session: %d", w.Code)
	}
	var got int64
	if err := database.QueryRow(`SELECT expires_at FROM gocms_admin_session WHERE token=?`, token).Scan(&got); err != nil || got != idle {
		t.Fatalf("API key changed browser expiry: %d, %v", got, err)
	}
}

func TestAdminSessionLogoutAndDisabled(t *testing.T) {
	for _, disable := range []bool{false, true} {
		s, database, token := newCategoryTestServer(t)
		if _, err := database.Exec(`UPDATE gocms_admin_session SET expires_at=? WHERE token=?`, time.Now().Add(time.Hour).Unix(), token); err != nil {
			t.Fatal(err)
		}
		if disable {
			if _, err := database.Exec(`UPDATE gocms_admin_user SET disabled=1 WHERE username='gocms'`); err != nil {
				t.Fatal(err)
			}
		} else {
			w := categoryRequest(t, s, token, "POST", "/api/admin/logout", "")
			if w.Code != 200 || len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].MaxAge != -1 {
				t.Fatal("logout did not clear cookie")
			}
		}
		w := categoryRequest(t, s, token, "GET", "/api/admin/session", "")
		if w.Code != 401 || len(w.Result().Cookies()) != 0 {
			t.Fatalf("invalid session renewed: %d", w.Code)
		}
	}
}

func TestAdminSessionLegacyMigration(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`CREATE TABLE gocms_admin_session (token TEXT PRIMARY KEY, username TEXT NOT NULL, expires_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	expiry := time.Now().Add(time.Hour).Unix()
	if _, err := database.Exec(`INSERT INTO gocms_admin_session VALUES ('legacy','admin',?)`, expiry); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := ensureAdminSessions(database); err != nil {
			t.Fatal(err)
		}
		var got, absolute int64
		if err := database.QueryRow(`SELECT expires_at,absolute_expires_at FROM gocms_admin_session WHERE token='legacy'`).Scan(&got, &absolute); err != nil {
			t.Fatal(err)
		}
		if got != expiry || absolute != expiry+6*86400 {
			t.Fatalf("migration changed original session lifetime: %d %d", got, absolute)
		}
	}
}
