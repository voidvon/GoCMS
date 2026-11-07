package site

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"gocms/internal/memberoauth"
)

type facebookTransport func(*http.Request) (*http.Response, error)

func (f facebookTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func mockFacebook(t *testing.T) *string {
	t.Helper()
	mode := ""
	old := googleHTTPClient
	googleHTTPClient = &http.Client{Transport: facebookTransport(func(r *http.Request) (*http.Response, error) {
		t.Helper()
		if r.URL.Host != "graph.facebook.com" {
			t.Fatalf("unexpected host %s", r.URL.Host)
		}
		body := ""
		status := 200
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth/access_token"):
			if e := r.ParseForm(); e != nil {
				t.Fatal(e)
			}
			if r.Form.Get("client_id") != "123456" || r.Form.Get("client_secret") != "facebook-secret" {
				t.Fatal("missing credentials")
			}
			body = `{"access_token":"user-token","token_type":"bearer","expires_in":3600}`
		case strings.HasSuffix(r.URL.Path, "/debug_token"):
			if r.Header.Get("Authorization") != "Bearer 123456|facebook-secret" || r.URL.Query().Get("input_token") != "user-token" {
				t.Fatal("missing debug credentials")
			}
			app := "123456"
			expires := time.Now().Add(time.Hour).Unix()
			if mode == "wrong-app" {
				app = "other"
			}
			if mode == "expired" {
				expires = 1
			}
			body = fmt.Sprintf(`{"data":{"is_valid":%t,"app_id":%q,"user_id":"fb-person","expires_at":%d}}`, mode != "invalid", app, expires)
		case strings.HasSuffix(r.URL.Path, "/me"):
			if r.Header.Get("Authorization") != "Bearer user-token" || len(r.URL.Query().Get("appsecret_proof")) != 64 {
				t.Fatal("missing proof")
			}
			id := "fb-person"
			if mode == "wrong-user" {
				id = "other"
			}
			body = fmt.Sprintf(`{"id":%q,"name":"Person","email":"existing@example.com"}`, id)
		default:
			t.Fatalf("unexpected endpoint %s", r.URL.Path)
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	t.Cleanup(func() { googleHTTPClient = old })
	return &mode
}
func TestFacebookTokenValidation(t *testing.T) {
	mode := mockFacebook(t)
	c := memberoauth.Config{Provider: "facebook", ClientID: "123456", PublicURL: "https://example.com"}
	for _, m := range []string{"", "wrong-app", "expired", "invalid", "wrong-user"} {
		t.Run(m, func(t *testing.T) {
			*mode = m
			claims, e := verifyFacebook(context.Background(), c, "facebook-secret", "code")
			if m == "" {
				if e != nil || claims.Subject != "fb-person" || claims.Verified {
					t.Fatalf("claims=%+v err=%v", claims, e)
				}
			} else if e == nil {
				t.Fatal("accepted invalid token")
			}
		})
	}
}
func TestFacebookPlatformLogin(t *testing.T) {
	mockFacebook(t)
	s, database, admin := newCategoryTestServer(t)
	if _, e := database.Exec(`UPDATE gocms_site SET domain='example.com' WHERE id=1; INSERT INTO gocms_site(name,code,domain) VALUES('Second','second','second.example.com')`); e != nil {
		t.Fatal(e)
	}
	if e := s.ConfigureMemberLoginKey(t.TempDir()); e != nil {
		t.Fatal(e)
	}
	request := func(host, method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://"+host+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		for _, c := range cookies {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	ac := &http.Cookie{Name: "gocms_admin", Value: admin}
	check := func(w *httptest.ResponseRecorder, code int) {
		t.Helper()
		if w.Code != code {
			t.Fatalf("%d: %s", w.Code, w.Body.String())
		}
	}
	check(request("example.com", "POST", "/api/admin/member-login/key", `{"mode":"generate"}`, ac), 200)
	check(request("example.com", "PUT", "/api/admin/member-login?provider=facebook", `{"enabled":true,"auto_register":true,"client_id":"123456","secret":"facebook-secret","revision":""}`, ac), 200)
	status := request("example.com", "GET", "/api/admin/member-login?provider=facebook", "", ac)
	check(status, 200)
	if strings.Contains(status.Body.String(), "facebook-secret") || !strings.Contains(status.Body.String(), "/api/auth/facebook/callback") {
		t.Fatal(status.Body.String())
	}
	for _, host := range []string{"example.com", "second.example.com"} {
		w := request(host, "GET", "/api/auth/providers", "")
		if !strings.Contains(w.Body.String(), `"facebook":true`) || !strings.Contains(w.Body.String(), `"google":false`) {
			t.Fatal(w.Body.String())
		}
	}
	start := func(host string) (string, *http.Cookie) {
		t.Helper()
		w := request(host, "POST", "/api/auth/facebook/start", `{"next":"/shop/account"}`)
		check(w, 200)
		var out map[string]string
		json.Unmarshal(w.Body.Bytes(), &out)
		u, _ := url.Parse(out["url"])
		if u.Host != "www.facebook.com" || u.Query().Get("redirect_uri") != "https://"+host+"/api/auth/facebook/callback" {
			t.Fatal(out)
		}
		return u.Query().Get("state"), w.Result().Cookies()[0]
	}
	// A Google save must not invalidate a pending Facebook authorization.
	pending, pendingCookie := start("example.com")
	check(request("example.com", "PUT", "/api/admin/member-login", `{"enabled":false,"auto_register":true,"client_id":"test.apps.googleusercontent.com","revision":""}`, ac), 200)
	w := request("example.com", "GET", "/api/auth/facebook/callback?error=access_denied&state="+pending, "", pendingCookie)
	if !strings.Contains(w.Header().Get("Location"), "facebook_cancelled") {
		t.Fatalf("unrelated config invalidated Facebook: %s", w.Header())
	}
	// Provider state may not be consumed through Google's callback, even with the browser cookie.
	pending, pendingCookie = start("example.com")
	w = request("example.com", "GET", "/api/auth/google/callback?code=code&state="+pending, "", pendingCookie)
	if !strings.Contains(w.Header().Get("Location"), "google_failed") {
		t.Fatal("cross-provider state accepted")
	}
	w = request("example.com", "GET", "/api/auth/facebook/callback?error=access_denied&state="+pending, "", pendingCookie)
	if !strings.Contains(w.Header().Get("Location"), "facebook_cancelled") {
		t.Fatal("wrong provider consumed state")
	}

	var uid int64
	for _, host := range []string{"example.com", "second.example.com"} {
		state, ck := start(host)
		w := request(host, "GET", "/api/auth/facebook/callback?code=code&state="+state, "", ck)
		check(w, 303)
		if w.Header().Get("Location") != "/shop/account" {
			t.Fatal(w.Header())
		}
		var session *http.Cookie
		for _, c := range w.Result().Cookies() {
			if c.Name == "gocms_user" {
				session = c
			}
		}
		if session == nil {
			t.Fatal("no session")
		}
		w = request(host, "GET", "/api/v1/me/identities/facebook", "", session)
		check(w, 200)
		if !strings.Contains(w.Body.String(), `"bound":true`) {
			t.Fatal(w.Body.String())
		}
		w = request(host, "GET", "/api/v1/me/identities/google", "", session)
		if !strings.Contains(w.Body.String(), `"bound":false`) {
			t.Fatal("provider identity leaked")
		}
		w = request(host, "GET", "/api/auth/facebook/callback?code=code&state="+state, "", ck)
		if !strings.Contains(w.Header().Get("Location"), "facebook_failed") {
			t.Fatal("replay accepted")
		}
	}
	var count int
	var email, sites string
	if e := database.QueryRow(`SELECT COUNT(*),MIN(id),MIN(email),MIN(site_ids) FROM gocms_user WHERE username LIKE 'facebook_%'`).Scan(&count, &uid, &email, &sites); e != nil || count != 1 || email != "" || !strings.Contains(sites, "2") {
		t.Fatalf("count %d email %s sites %s: %v", count, email, sites, e)
	}
	state, ck := start("example.com")
	w = request("second.example.com", "GET", "/api/auth/facebook/callback?code=code&state="+state, "", ck)
	if !strings.Contains(w.Header().Get("Location"), "facebook_unavailable") {
		t.Fatal("cross-origin accepted")
	}
	c, e := memberoauth.LoadProvider(context.Background(), database, 0, "facebook")
	if e != nil {
		t.Fatal(e)
	}
	// Secret ciphertext cannot be used as a Google secret.
	if _, e = s.oauthVault.Open(0, c.Encrypted); e == nil {
		t.Fatal("provider secret isolation failed")
	}
	check(request("example.com", "PUT", "/api/admin/member-login?provider=facebook", fmt.Sprintf(`{"enabled":false,"auto_register":true,"client_id":"123456","revision":%q}`, c.Revision), ac), 200)
	w = request("second.example.com", "GET", "/api/auth/providers", "")
	if strings.Contains(w.Body.String(), `"facebook":true`) {
		t.Fatal("disable did not apply")
	}
	check(request("example.com", "POST", "/api/auth/facebook/start", `{}`), 403)
}
