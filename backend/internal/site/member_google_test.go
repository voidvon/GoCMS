package site

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
	"gocms/internal/member"
	"gocms/internal/memberoauth"
)

func TestGoogleLoginLifecycle(t *testing.T) {
	s, database, admin := newCategoryTestServer(t)
	if _, e := database.Exec(`UPDATE gocms_site SET domain='example.com' WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	if e := s.ConfigureMemberLoginKey(t.TempDir()); e != nil {
		t.Fatal(e)
	}
	request := func(method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://example.com"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		for _, c := range cookies {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	ac := &http.Cookie{Name: "gocms_admin", Value: admin}
	expect := func(w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("status=%d want=%d: %s", w.Code, status, w.Body.String())
		}
	}
	expect(request("POST", "/api/admin/member-login/key", `{"mode":"generate"}`, ac), 200)
	config := `{"enabled":true,"auto_register":true,"client_id":"test.apps.googleusercontent.com","secret":"google-secret","revision":""}`
	expect(request("PUT", "/api/admin/member-login", config, ac), 200)
	status := request("GET", "/api/admin/member-login", "", ac)
	expect(status, 200)
	if strings.Contains(status.Body.String(), "google-secret") || strings.Contains(status.Body.String(), "secret_ciphertext") {
		t.Fatal("secret leaked")
	}
	var ciphertext string
	if e := database.QueryRow(`SELECT secret_ciphertext FROM gocms_platform_oauth_config`).Scan(&ciphertext); e != nil || strings.Contains(ciphertext, "google-secret") {
		t.Fatal("secret not encrypted")
	}
	expect(request("PUT", "/api/admin/member-login", config, ac), 409)
	expect(request("POST", "/api/admin/member-login/key", `{"mode":"generate"}`, ac), 409)
	expect(request("POST", "/api/admin/member-login/key/export", `{}`, ac), 200)
	subject, email, nonceOverride := "google-123", "person@example.com", ""
	s.googleIdentity = func(ctx context.Context, c memberoauth.Config, secret, code, verifier string) (googleClaims, error) {
		if secret != "google-secret" || verifier == "" {
			t.Fatal("missing server credentials/PKCE")
		}
		return googleClaims{Subject: subject, Email: email, Verified: true, Name: "Person", Nonce: nonceOverride}, nil
	}
	start := func(body string, cookies ...*http.Cookie) (string, *http.Cookie) {
		t.Helper()
		w := request("POST", "/api/auth/google/start", body, cookies...)
		expect(w, 200)
		var out map[string]string
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(e)
		}
		u, e := url.Parse(out["url"])
		if e != nil {
			t.Fatal(e)
		}
		if u.Query().Get("code_challenge_method") != "S256" {
			t.Fatal("missing PKCE")
		}
		nonceOverride = u.Query().Get("nonce")
		return u.Query().Get("state"), w.Result().Cookies()[0]
	}
	callback := func(state string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		return request("GET", "/api/auth/google/callback?code=code&state="+state, "", cookies...)
	}
	state, cookie := start(`{"next":"/shop/checkout"}`)
	expect(callback(state), 303) // No browser binding must not consume the real transaction.
	w := callback(state, cookie)
	expect(w, 303)
	if w.Header().Get("Location") != "/shop/checkout" {
		t.Fatalf("callback failed: %s", w.Header().Get("Location"))
	}
	var memberCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "gocms_user" {
			memberCookie = c
		}
	}
	if memberCookie == nil {
		t.Fatal("no member session")
	}
	var user int64
	var password string
	if e := database.QueryRow(`SELECT id,password_hash FROM gocms_user WHERE email=?`, email).Scan(&user, &password); e != nil || password != "" {
		t.Fatal("Google account should have no password")
	}
	expect(request("GET", "/api/v1/me", "", memberCookie), 200)
	w = callback(state, cookie)
	if !strings.Contains(w.Header().Get("Location"), "google_failed") {
		t.Fatal("state replay accepted")
	}
	// Passwordless users cannot remove their only login method.
	expect(request("DELETE", "/api/v1/me/identities/google", `{"password":""}`, memberCookie), 400)
	// Expired and cross-origin transactions cannot issue sessions.
	state, cookie = start(`{}`)
	if _, e := database.Exec(`UPDATE gocms_member_oauth_state SET expires_at=0 WHERE state_hash=?`, memberoauth.Hash(state)); e != nil {
		t.Fatal(e)
	}
	w = callback(state, cookie)
	if !strings.Contains(w.Header().Get("Location"), "google_failed") {
		t.Fatal("expired state accepted")
	}
	state, cookie = start(`{}`)
	other := httptest.NewRequest("GET", "https://other.example.com/api/auth/google/callback?code=code&state="+state, nil)
	other.AddCookie(cookie)
	otherW := httptest.NewRecorder()
	s.Handler().ServeHTTP(otherW, other)
	if !strings.Contains(otherW.Header().Get("Location"), "google_unavailable") {
		t.Fatal("wrong callback host accepted")
	}
	// Invalid nonce cannot issue a session.
	state, cookie = start(`{}`)
	nonceOverride = "wrong"
	w = callback(state, cookie)
	if !strings.Contains(w.Header().Get("Location"), "google_failed") {
		t.Fatal("nonce not verified")
	}
	// A different subject with the same email must never merge accounts.
	subject = "different-subject"
	state, cookie = start(`{}`)
	w = callback(state, cookie)
	if !strings.Contains(w.Header().Get("Location"), "google_conflict") {
		t.Fatal("email auto-merged")
	}
	subject = "google-123"
	// The shared session cap is enforced for Google logins too.
	if _, e := database.Exec(`UPDATE gocms_user SET max_sessions=1 WHERE id=?`, user); e != nil {
		t.Fatal(e)
	}
	state, cookie = start(`{}`)
	w = callback(state, cookie)
	if !strings.Contains(w.Header().Get("Location"), "session_limit_reached") {
		t.Fatal("session cap bypassed")
	}
	// Disabled members cannot sign in.
	if _, e := database.Exec(`UPDATE gocms_user SET status='disabled' WHERE id=?`, user); e != nil {
		t.Fatal(e)
	}
	state, cookie = start(`{}`)
	w = callback(state, cookie)
	if w.Header().Get("Location") == "/shop/cart" {
		t.Fatal("disabled member logged in")
	}
	// Disable/update invalidates pending callbacks and the public entry point.
	state, cookie = start(`{}`)
	var revision string
	if e := database.QueryRow(`SELECT revision FROM gocms_platform_oauth_config`).Scan(&revision); e != nil {
		t.Fatal(e)
	}
	body := `{"enabled":false,"auto_register":true,"client_id":"test.apps.googleusercontent.com","revision":"` + revision + `"}`
	expect(request("PUT", "/api/admin/member-login", body, ac), 200)
	expect(request("POST", "/api/auth/google/start", `{}`), 403)
	w = callback(state, cookie)
	if w.Header().Get("Location") == "/shop/cart" {
		t.Fatal("disabled callback accepted")
	}
	providers := request("GET", "/api/auth/providers", "")
	if !strings.Contains(providers.Body.String(), `"google":false`) {
		t.Fatal("disabled button advertised")
	}
	// Administrators can verify disabled configuration without creating a member session.
	testStart := request("POST", "/api/admin/member-login/test", `{}`, ac)
	expect(testStart, 200)
	var out map[string]string
	if e := json.Unmarshal(testStart.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	testURL, _ := url.Parse(out["url"])
	nonceOverride = testURL.Query().Get("nonce")
	testResult := callback(testURL.Query().Get("state"), testStart.Result().Cookies()[0], ac)
	if testResult.Header().Get("Location") != "/admin/member-login?oauth_test=success" {
		t.Fatal(testResult.Header().Get("Location"))
	}
	for _, ck := range testResult.Result().Cookies() {
		if ck.Name == "gocms_user" {
			t.Fatal("test created member session")
		}
	}
	// Configuration changes and API mutation requests must be same-origin.
	cross := httptest.NewRequest("PUT", "https://example.com/api/admin/member-login", strings.NewReader(config))
	cross.Header.Set("Content-Type", "application/json")
	cross.Header.Set("Origin", "https://evil.example")
	cross.AddCookie(ac)
	crossW := httptest.NewRecorder()
	s.Handler().ServeHTTP(crossW, cross)
	expect(crossW, 403)
	// A key can be restored into a missing-key server but never replaced arbitrarily.
	backup := request("POST", "/api/admin/member-login/key/export", `{}`, ac).Body.String()
	s.oauthMu.Lock()
	s.oauthVault = nil
	s.oauthKeyPath = ""
	s.oauthMu.Unlock()
	if e := s.ConfigureMemberLoginKey(t.TempDir()); e != nil {
		t.Fatal(e)
	}
	expect(request("POST", "/api/admin/member-login/key", `{"mode":"generate"}`, ac), 409)
	encoded, _ := json.Marshal(map[string]string{"mode": "import", "key": backup})
	expect(request("POST", "/api/admin/member-login/key", string(encoded), ac), 200)
}

func TestGoogleBindingAndPermissions(t *testing.T) {
	s, database, admin := newCategoryTestServer(t)
	ctx := context.Background()
	svc := member.Service{DB: database}
	_, e := database.Exec(`UPDATE gocms_site SET domain='example.com' WHERE id=1;INSERT INTO gocms_site(name,code,domain) VALUES('Other','other','other.example.com')`)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConfigureMemberLoginKey(t.TempDir()); e != nil {
		t.Fatal(e)
	}
	root := func(method, path, body string) *httptest.ResponseRecorder {
		return categoryRequest(t, s, admin, method, path, body)
	}
	if w := root("POST", "/api/admin/member-login/key", `{"mode":"generate"}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	config := `{"enabled":true,"auto_register":false,"client_id":"test.apps.googleusercontent.com","secret":"secret","revision":""}`
	if w := root("PUT", "/api/admin/member-login", config); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	c, e := memberoauth.Load(ctx, database, 1)
	if e != nil {
		t.Fatal(e)
	}
	p, e := svc.Register(ctx, "existing", "old@example.com", "password123", 1)
	if e != nil {
		t.Fatal(e)
	}
	_, token, e := svc.Login(ctx, "existing", "password123", "ip", "agent", "", 1)
	if e != nil {
		t.Fatal(e)
	}
	if e = svc.ConfirmExternalChange(ctx, p.ID, token, "bad"); e == nil {
		t.Fatal("bad reauthentication accepted")
	}
	if e = svc.ConfirmExternalChange(ctx, p.ID, token, "password123"); e != nil {
		t.Fatal(e)
	}
	if _, _, e = svc.ExternalLogin(ctx, 1, c.Revision, "subject", "old@example.com", "Name", true, 0, "", "ip", "agent", ""); e == nil {
		t.Fatal("registration allowed while off")
	}
	if _, _, e = svc.ExternalLogin(ctx, 1, c.Revision, "subject", "old@example.com", "Name", true, p.ID, member.TokenHash(token), "ip", "agent", ""); e != nil {
		t.Fatal(e)
	}
	if _, _, e = svc.ExternalLogin(ctx, 1, c.Revision, "subject", "old@example.com", "Name", true, 0, "", "ip", "agent", token); e != nil {
		t.Fatal(e)
	}
	if shared, _, e := svc.ExternalLogin(ctx, 2, c.Revision, "subject", "old@example.com", "Name", true, 0, "", "ip", "agent", ""); e != nil || shared.ID != p.ID {
		t.Fatalf("shared identity: %v", e)
	}

	if e = svc.UnbindGoogle(ctx, p.ID, token, "bad"); e == nil {
		t.Fatal("bad unbind password accepted")
	}
	// Shared sessions obey one global account cap; release test sessions before reauthentication.
	if _, e = database.Exec(`DELETE FROM gocms_user_session WHERE user_id=?`, p.ID); e != nil {
		t.Fatal(e)
	}
	// Login above rotated the original token; use the current session to unbind.
	_, token, e = svc.Login(ctx, "existing", "password123", "ip", "agent", "", 1)
	if e != nil {
		t.Fatal(e)
	}
	if e = svc.UnbindGoogle(ctx, p.ID, token, "password123"); e != nil {
		t.Fatal(e)
	}
	bound, _, e := svc.ExternalStatus(ctx, p.ID)
	if e != nil || bound {
		t.Fatal("identity not removed")
	}
	// Site administrators cannot access platform credentials even with legacy permissions.
	_, e = database.Exec(`INSERT INTO gocms_admin_group(name,permissions) VALUES('member-reader','["members"]');INSERT INTO gocms_admin_user(username,group_id) SELECT 'member-reader',id FROM gocms_admin_group WHERE name='member-reader'`)
	if e != nil {
		t.Fatal(e)
	}
	viewer, e := s.createSession("member-reader")
	if e != nil {
		t.Fatal(e)
	}
	if w := categoryRequest(t, s, viewer, "GET", "/api/admin/member-login", ""); w.Code != 403 {
		t.Fatal("missing view permission accepted")
	}
	_, e = database.Exec(`UPDATE gocms_admin_group SET permissions='["members","members.login.view"]' WHERE name='member-reader'`)
	if e != nil {
		t.Fatal(e)
	}
	if w := categoryRequest(t, s, viewer, "GET", "/api/admin/member-login", ""); w.Code != 403 {
		t.Fatal(w.Body.String())
	}
	if w := categoryRequest(t, s, viewer, "PUT", "/api/admin/member-login", config); w.Code != 403 {
		t.Fatal("missing configure permission accepted")
	}
	if w := categoryRequest(t, s, viewer, "POST", "/api/admin/member-login/key/export", "{}"); w.Code != 403 {
		t.Fatal("non-super key export accepted")
	}
}

func TestSafeOAuthNext(t *testing.T) {
	for _, v := range []string{"https://evil.example", "//evil.example", "/\\evil.example", "/api/auth/logout", "/admin", "/shop/login", "/%61pi/auth/logout", "/x/../admin", "/%5cevil.example"} {
		if got := safeOAuthNext(v); got != "/shop/cart" {
			t.Errorf("accepted %q as %q", v, got)
		}
	}
	if got := safeOAuthNext("/shop/checkout?coupon=test"); got != "/shop/checkout?coupon=test" {
		t.Fatal(got)
	}
}

func TestGoogleTokenVerification(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	signer, e := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, nil)
	if e != nil {
		t.Fatal(e)
	}
	jwks, _ := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, Algorithm: "RS256", Use: "sig"}}})
	claims := map[string]any{"iss": memberoauth.Issuer, "aud": "test.apps.googleusercontent.com", "sub": "subject", "nonce": "nonce", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "email": "a@example.com", "email_verified": true}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/keys" {
			_, _ = w.Write(jwks)
			return
		}
		signed, e := signer.Sign(mustJSON(t, claims))
		if e != nil {
			t.Error(e)
			return
		}
		jwt, e := signed.CompactSerialize()
		if e != nil {
			t.Error(e)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "unused", "token_type": "Bearer", "id_token": jwt})
	}))
	defer server.Close()
	oldClient, oldKeys := googleHTTPClient, googleKeySet
	defer func() { googleHTTPClient = oldClient; googleKeySet = oldKeys }()
	googleHTTPClient = &http.Client{Transport: googleTestTransport{server.URL}, Timeout: time.Second}
	googleKeySet = oidc.NewRemoteKeySet(context.Background(), server.URL+"/keys")
	c := memberoauth.Config{ClientID: "test.apps.googleusercontent.com", PublicURL: "https://example.com"}
	if got, e := verifyGoogle(context.Background(), c, "secret", "code", "verifier"); e != nil || got.Subject != "subject" {
		t.Fatalf("valid token: %v", e)
	}
	for _, field := range []string{"aud", "iss", "exp", "azp"} {
		old := claims[field]
		if field == "exp" {
			claims[field] = time.Now().Add(-time.Hour).Unix()
		} else {
			claims[field] = "invalid"
		}
		if _, e := verifyGoogle(context.Background(), c, "secret", "code", "verifier"); e == nil {
			t.Errorf("invalid %s accepted", field)
		}
		claims[field] = old
	}
}
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

type googleTestTransport struct{ url string }

func (t googleTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	u, e := url.Parse(t.url)
	if e != nil {
		return nil, e
	}
	r = r.Clone(r.Context())
	r.URL.Scheme = u.Scheme
	r.URL.Host = u.Host
	r.URL.Path = "/token"
	r.Host = u.Host
	return http.DefaultTransport.RoundTrip(r)
}
