package site

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
	"gocms/internal/memberoauth"
	"golang.org/x/oauth2"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func mockMicrosoft(t *testing.T) (map[string]any, *string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, nil)
	if err != nil {
		t.Fatal(err)
	}
	jwks := mustJSON(t, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, Algorithm: "RS256", Use: "sig"}}})
	nonce := "nonce"
	claims := map[string]any{"iss": microsoftAuthority + "/9188040d-6c67-4c5b-b112-36a304b66dad/v2.0", "tid": "9188040d-6c67-4c5b-b112-36a304b66dad", "ver": "2.0", "aud": "123456", "sub": "ms-person", "name": "Person", "email": "existing@example.com", "email_verified": true, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix()}
	oldClient, oldKeys := googleHTTPClient, microsoftKeySet
	googleHTTPClient = &http.Client{Transport: facebookTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "login.microsoftonline.com" {
			t.Fatalf("unexpected host: %s", r.URL.Host)
		}
		body := string(jwks)
		if strings.HasSuffix(r.URL.Path, "/token") {
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("client_id") != "123456" || r.Form.Get("client_secret") != "microsoft-secret" || r.Form.Get("code_verifier") == "" {
				t.Fatal("missing credentials or PKCE")
			}
			claims["nonce"] = nonce
			signed, err := signer.Sign(mustJSON(t, claims))
			if err != nil {
				t.Fatal(err)
			}
			raw, err := signed.CompactSerialize()
			if err != nil {
				t.Fatal(err)
			}
			body = string(mustJSON(t, map[string]any{"token_type": "Bearer", "access_token": "unused", "id_token": raw}))
		} else if !strings.HasSuffix(r.URL.Path, "/keys") {
			t.Fatal("unexpected Microsoft endpoint")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	microsoftKeySet = oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), googleHTTPClient), microsoftAuthority+"/common/discovery/v2.0/keys")
	t.Cleanup(func() { googleHTTPClient = oldClient; microsoftKeySet = oldKeys })
	return claims, &nonce
}
func TestMicrosoftTokenValidation(t *testing.T) {
	claims, _ := mockMicrosoft(t)
	c := memberoauth.Config{Provider: "microsoft", ClientID: "123456", PublicURL: "https://example.com"}
	got, err := verifyMicrosoft(context.Background(), c, "microsoft-secret", "code", "verifier")
	if err != nil || got.Subject != "9188040d-6c67-4c5b-b112-36a304b66dad:ms-person" || got.Email != "" || got.Verified {
		t.Fatalf("claims=%+v err=%v", got, err)
	}
	for _, field := range []string{"aud", "iss", "exp", "azp", "tid", "ver", "sub"} {
		t.Run(field, func(t *testing.T) {
			old := claims[field]
			switch field {
			case "exp":
				claims[field] = time.Now().Add(-time.Hour).Unix()
			case "sub":
				claims[field] = ""
			default:
				claims[field] = "invalid"
			}
			if _, err := verifyMicrosoft(context.Background(), c, "microsoft-secret", "code", "verifier"); err == nil {
				t.Fatal("invalid claim accepted")
			}
			claims[field] = old
		})
	}
	// Even a validly signed token cannot use a different tenant's issuer.
	claims["tid"] = "11111111-2222-3333-4444-555555555555"
	if _, err := verifyMicrosoft(context.Background(), c, "microsoft-secret", "code", "verifier"); err == nil {
		t.Fatal("tenant/issuer mismatch accepted")
	}
	claims["tid"] = "9188040d-6c67-4c5b-b112-36a304b66dad"
	config := microsoftOAuth(c, "microsoft-secret")
	token, err := config.Exchange(oidc.ClientContext(context.Background(), googleHTTPClient), "code", oauth2.VerifierOption("verifier"))
	if err != nil {
		t.Fatal(err)
	}
	raw := token.Extra("id_token").(string)
	wrongKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifyMicrosoftIDToken(context.Background(), c, raw, &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&wrongKey.PublicKey}}); err == nil {
		t.Fatal("invalid signature accepted")
	}

}
func TestMicrosoftPlatformLogin(t *testing.T) {
	_, nonce := mockMicrosoft(t)
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
	check(request("example.com", "PUT", "/api/admin/member-login?provider=microsoft", `{"enabled":true,"auto_register":true,"client_id":"123456","secret":"microsoft-secret","revision":""}`, ac), 200)
	status := request("example.com", "GET", "/api/admin/member-login?provider=microsoft", "", ac)
	check(status, 200)
	if strings.Contains(status.Body.String(), "microsoft-secret") || !strings.Contains(status.Body.String(), "/api/auth/microsoft/callback") {
		t.Fatal(status.Body.String())
	}
	for _, host := range []string{"example.com", "second.example.com"} {
		w := request(host, "GET", "/api/auth/providers", "")
		if !strings.Contains(w.Body.String(), `"microsoft":true`) || !strings.Contains(w.Body.String(), `"google":false`) {
			t.Fatal(w.Body.String())
		}
	}
	start := func(host string) (string, *http.Cookie) {
		t.Helper()
		w := request(host, "POST", "/api/auth/microsoft/start", `{"next":"/shop/account"}`)
		check(w, 200)
		var out map[string]string
		json.Unmarshal(w.Body.Bytes(), &out)
		u, _ := url.Parse(out["url"])
		if u.Host != "login.microsoftonline.com" || u.Query().Get("redirect_uri") != "https://"+host+"/api/auth/microsoft/callback" {
			t.Fatal(out)
		}
		if u.Query().Get("scope") != "openid profile" || u.Query().Get("code_challenge_method") != "S256" || u.Query().Get("nonce") == "" {
			t.Fatal("missing OIDC / PKCE protection")
		}
		*nonce = u.Query().Get("nonce")
		return u.Query().Get("state"), w.Result().Cookies()[0]
	}
	// A Google save must not invalidate a pending Microsoft authorization.
	pending, pendingCookie := start("example.com")
	check(request("example.com", "PUT", "/api/admin/member-login", `{"enabled":false,"auto_register":true,"client_id":"test.apps.googleusercontent.com","revision":""}`, ac), 200)
	w := request("example.com", "GET", "/api/auth/microsoft/callback?error=access_denied&state="+pending, "", pendingCookie)
	if !strings.Contains(w.Header().Get("Location"), "microsoft_cancelled") {
		t.Fatalf("unrelated config invalidated Microsoft: %s", w.Header())
	}
	// Provider state may not be consumed through Google's callback, even with the browser cookie.
	pending, pendingCookie = start("example.com")
	w = request("example.com", "GET", "/api/auth/google/callback?code=code&state="+pending, "", pendingCookie)
	if !strings.Contains(w.Header().Get("Location"), "google_failed") {
		t.Fatal("cross-provider state accepted")
	}
	w = request("example.com", "GET", "/api/auth/microsoft/callback?error=access_denied&state="+pending, "", pendingCookie)
	if !strings.Contains(w.Header().Get("Location"), "microsoft_cancelled") {
		t.Fatal("wrong provider consumed state")
	}

	// A correctly signed token from a different authorization must not authenticate.
	pending, pendingCookie = start("example.com")
	*nonce = "wrong-nonce"
	w = request("example.com", "GET", "/api/auth/microsoft/callback?code=code&state="+pending, "", pendingCookie)
	if !strings.Contains(w.Header().Get("Location"), "microsoft_failed") {
		t.Fatal("nonce mismatch accepted")
	}

	var uid int64
	for _, host := range []string{"example.com", "second.example.com"} {
		state, ck := start(host)
		w := request(host, "GET", "/api/auth/microsoft/callback?code=code&state="+state, "", ck)
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
		w = request(host, "GET", "/api/v1/me/identities/microsoft", "", session)
		check(w, 200)
		if !strings.Contains(w.Body.String(), `"bound":true`) {
			t.Fatal(w.Body.String())
		}
		w = request(host, "GET", "/api/v1/me/identities/google", "", session)
		if !strings.Contains(w.Body.String(), `"bound":false`) {
			t.Fatal("provider identity leaked")
		}
		w = request(host, "GET", "/api/auth/microsoft/callback?code=code&state="+state, "", ck)
		if !strings.Contains(w.Header().Get("Location"), "microsoft_failed") {
			t.Fatal("replay accepted")
		}
	}
	var count int
	var email, sites string
	if e := database.QueryRow(`SELECT COUNT(*),MIN(id),MIN(email),MIN(site_ids) FROM gocms_user WHERE username LIKE 'microsoft_%'`).Scan(&count, &uid, &email, &sites); e != nil || count != 1 || email != "" || !strings.Contains(sites, "2") {
		t.Fatalf("count %d email %s sites %s: %v", count, email, sites, e)
	}
	state, ck := start("example.com")
	w = request("second.example.com", "GET", "/api/auth/microsoft/callback?code=code&state="+state, "", ck)
	if !strings.Contains(w.Header().Get("Location"), "microsoft_unavailable") {
		t.Fatal("cross-origin accepted")
	}
	c, e := memberoauth.LoadProvider(context.Background(), database, 0, "microsoft")
	if e != nil {
		t.Fatal(e)
	}
	// Secret ciphertext cannot be used as a Google secret.
	if _, e = s.oauthVault.Open(0, c.Encrypted); e == nil {
		t.Fatal("provider secret isolation failed")
	}
	check(request("example.com", "PUT", "/api/admin/member-login?provider=microsoft", fmt.Sprintf(`{"enabled":false,"auto_register":true,"client_id":"123456","revision":%q}`, c.Revision), ac), 200)
	w = request("second.example.com", "GET", "/api/auth/providers", "")
	if strings.Contains(w.Body.String(), `"microsoft":true`) {
		t.Fatal("disable did not apply")
	}
	check(request("example.com", "POST", "/api/auth/microsoft/start", `{}`), 403)
}
