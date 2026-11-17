package site

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestAdminSessionIgnoresStaleSiteSelection(t *testing.T) {
	s, database, _ := newCategoryTestServer(t)
	if _, err := database.Exec(`
		INSERT INTO gocms_site (id, name, code) VALUES (2, 'Editor site', 'editor-site');
		INSERT INTO gocms_admin_group (name, permissions, site_ids) VALUES ('Editors', '["content"]', '[2]');
		INSERT INTO gocms_admin_user (username, group_id) SELECT 'editor', id FROM gocms_admin_group WHERE name='Editors'`); err != nil {
		t.Fatal(err)
	}
	token, err := s.createSession("editor")
	if err != nil {
		t.Fatal(err)
	}
	for _, selector := range []struct{ header, query string }{
		{header: "1"},
		{header: "999999"},
		{query: "?site_id=1"},
		{query: "?site_id=999999"},
	} {
		for _, endpoint := range []struct {
			path string
			want int
		}{
			{"/api/admin/session", http.StatusOK},
			{"/api/admin/sites", http.StatusOK},
			{"/api/admin/content", http.StatusForbidden},
		} {
			r := httptest.NewRequest(http.MethodGet, endpoint.path+selector.query, nil)
			r.Header.Set("X-Site-Id", selector.header)
			r.AddCookie(sessionCookie(token, 86400))
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if w.Code != endpoint.want {
				t.Fatalf("%s header=%q query=%q: got %d, want %d: %s", endpoint.path, selector.header, selector.query, w.Code, endpoint.want, w.Body.String())
			}
			if endpoint.path == "/api/admin/sites" {
				var sites []struct{ ID int64 }
				if err := json.Unmarshal(w.Body.Bytes(), &sites); err != nil || len(sites) != 1 || sites[0].ID != 2 {
					t.Fatalf("site recovery returned inaccessible sites: %s", w.Body.String())
				}
			}
		}
	}
}

func TestAdminSessionStorageFailureIsUnavailable(t *testing.T) {
	for _, table := range []string{"gocms_admin_session", "gocms_admin_user"} {
		t.Run(table, func(t *testing.T) {
			s, database, token := newCategoryTestServer(t)
			if _, err := database.Exec(`ALTER TABLE ` + table + ` RENAME TO unavailable_admin_table`); err != nil {
				t.Fatal(err)
			}
			w := categoryRequest(t, s, token, http.MethodGet, "/api/admin/session", "")
			if w.Code != http.StatusServiceUnavailable || len(w.Result().Cookies()) != 0 {
				t.Fatalf("storage failure treated as expired session: %d %s", w.Code, w.Body.String())
			}
			if _, err := database.Exec(`ALTER TABLE unavailable_admin_table RENAME TO ` + table); err != nil {
				t.Fatal(err)
			}
			w = categoryRequest(t, s, token, http.MethodGet, "/api/admin/session", "")
			if w.Code != http.StatusOK {
				t.Fatalf("session did not recover: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestAdminAuthResponsesAreNotCacheable(t *testing.T) {
	s, _, token := newCategoryTestServer(t)
	for _, tc := range []struct{ method, path, token, body string }{
		{http.MethodGet, "/api/admin/session", token, ""},
		{http.MethodGet, "/api/admin/session", "invalid", ""},
		{http.MethodGet, "/api/admin/setup-status", "", ""},
		{http.MethodPost, "/api/admin/login", "", `{"username":"missing","password":"incorrect"}`},
		{http.MethodPost, "/api/admin/logout", token, ""},
	} {
		w := categoryRequest(t, s, tc.token, tc.method, tc.path, tc.body)
		if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
			t.Fatalf("%s %s status %d may be cached", tc.method, tc.path, w.Code)
		}
	}
}
