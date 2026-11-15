package site

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gocms/internal/member"
)

func memberTokenRequest(t *testing.T, s *Server, method, path, body, token string, cookie *http.Cookie, want int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != want {
		t.Fatalf("%s %s: want %d, got %d %s", method, path, want, w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("member response must not be cached")
	}
	return w
}

func memberAccessToken(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var payload struct {
		Data struct {
			User struct {
				ID int64 `json:"id"`
			} `json:"user"`
			AccessToken string `json:"access_token"`
			TokenType   string `json:"token_type"`
			ExpiresIn   int    `json:"expires_in"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.User.ID == 0 || len(payload.Data.AccessToken) < 32 || payload.Data.TokenType != "Bearer" || (payload.Data.ExpiresIn <= 0 || payload.Data.ExpiresIn > int(member.SessionLifetime/time.Second)) {
		t.Fatal("missing token login metadata")
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("token login must not set browser cookies")
	}
	return payload.Data.AccessToken
}

func TestMemberBearerLifecycle(t *testing.T) {
	s, database, _ := newCategoryTestServer(t)
	credentials := `{"username":"nativeuser","password":"initial-password"}`
	memberTokenRequest(t, s, "POST", "/api/v1/auth/register", credentials, "", nil, 201)
	started := time.Now().Unix()
	token := memberAccessToken(t, memberTokenRequest(t, s, "POST", "/api/v1/auth/token", credentials, "", nil, 200))
	var stored string
	var expires int64
	if err := database.QueryRow("SELECT token_hash,expires_at FROM gocms_user_session").Scan(&stored, &expires); err != nil {
		t.Fatal(err)
	}
	if stored == token || stored != member.TokenHash(token) || expires < started+int64(member.SessionLifetime/time.Second) || expires > time.Now().Add(member.SessionLifetime).Unix() {
		t.Fatal("session credential or expiry stored incorrectly")
	}
	memberTokenRequest(t, s, "PATCH", "/api/v1/me", `{"display_name":"Native User"}`, token, nil, 200)
	me := memberTokenRequest(t, s, "GET", "/api/v1/me", "", token, nil, 200)
	if !strings.Contains(me.Body.String(), "Native User") || strings.Contains(me.Body.String(), token) || strings.Contains(me.Body.String(), stored) {
		t.Fatal("profile should contain public data only")
	}
	memberTokenRequest(t, s, "GET", "/api/v1/me/memberships", "", token, nil, 200)
	// The legacy member helper must also accept the new transport.
	r := httptest.NewRequest("GET", "/api/auth/session", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Native User") {
		t.Fatal("legacy member authentication failed", w.Code, w.Body.String())
	}

	browser := memberTokenRequest(t, s, "POST", "/api/v1/auth/login", credentials, "", nil, 200)
	if strings.Contains(browser.Body.String(), "access_token") || len(browser.Result().Cookies()) != 1 {
		t.Fatal("browser login contract changed")
	}
	cookie := browser.Result().Cookies()[0]
	sessions := memberTokenRequest(t, s, "GET", "/api/v1/me/sessions", "", token, cookie, 200)
	var payload struct {
		Data []struct {
			ID      string `json:"id"`
			Current bool   `json:"current"`
		} `json:"data"`
	}
	if err := json.Unmarshal(sessions.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	current := ""
	for _, session := range payload.Data {
		if session.Current {
			if current != "" {
				t.Fatal("multiple sessions marked current")
			}
			current = session.ID
		}
	}
	if len(payload.Data) != 2 || current != member.SessionID(stored) || strings.Contains(sessions.Body.String(), token) || strings.Contains(sessions.Body.String(), stored) {
		t.Fatal("incorrect current session or exposed credential")
	}
	w = memberTokenRequest(t, s, "DELETE", "/api/v1/me/sessions/"+current, "", token, cookie, 200)
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("bearer revocation changed browser cookie")
	}
	memberTokenRequest(t, s, "GET", "/api/v1/me", "", token, cookie, 401)
	memberTokenRequest(t, s, "GET", "/api/v1/me", "", "", cookie, 200)

	token = memberAccessToken(t, memberTokenRequest(t, s, "POST", "/api/v1/auth/token", credentials, "", nil, 200))
	w = memberTokenRequest(t, s, "PUT", "/api/v1/me/password", `{"old_password":"initial-password","new_password":"changed-password"}`, token, cookie, 200)
	if len(w.Result().Cookies()) != 0 || !strings.Contains(w.Body.String(), `"reauthenticate":true`) {
		t.Fatal("incorrect password change response")
	}
	memberTokenRequest(t, s, "GET", "/api/v1/me", "", token, nil, 401)
	memberTokenRequest(t, s, "GET", "/api/v1/me", "", "", cookie, 401)
}

func TestMemberBearerTransportIsolation(t *testing.T) {
	s, _, _ := newCategoryTestServer(t)
	for _, username := range []string{"tokenuser", "cookieuser"} {
		memberTokenRequest(t, s, "POST", "/api/v1/auth/register", fmt.Sprintf(`{"username":%q,"password":"password123"}`, username), "", nil, 201)
	}
	credentials := `{"username":"tokenuser","password":"password123"}`
	token := memberAccessToken(t, memberTokenRequest(t, s, "POST", "/api/v1/auth/token", credentials, "", nil, 200))
	cookie := memberTokenRequest(t, s, "POST", "/api/v1/auth/login", `{"username":"cookieuser","password":"password123"}`, "", nil, 200).Result().Cookies()[0]
	for _, headers := range [][]string{{""}, {"Bearer"}, {"Basic abc"}, {"Bearer invalid"}, {"Bearer " + token, "Bearer " + token}} {
		r := httptest.NewRequest("GET", "/api/v1/me", nil)
		for _, header := range headers {
			r.Header.Add("Authorization", header)
		}
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 401 || !strings.Contains(w.Header().Get("WWW-Authenticate"), `error="invalid_token"`) {
			t.Fatal("invalid Authorization fell back to cookie", w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/api/v1/me", nil)
	r.Header.Set("Authorization", "bEaReR "+token)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"username":"tokenuser"`) {
		t.Fatal("bearer must select its own identity")
	}
	memberTokenRequest(t, s, "GET", "/api/v1/me?access_token="+token, "", "", nil, 401)
	// One member cannot revoke another member's session.
	memberTokenRequest(t, s, "DELETE", "/api/v1/me/sessions/"+member.SessionID(hashToken(cookie.Value)), "", token, nil, 404)
	w = memberTokenRequest(t, s, "POST", "/api/v1/auth/logout", `{}`, token, cookie, 200)
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("bearer logout changed browser cookie")
	}
	memberTokenRequest(t, s, "GET", "/api/v1/me", "", token, cookie, 401)
	memberTokenRequest(t, s, "GET", "/api/v1/me", "", "", cookie, 200)
	memberTokenRequest(t, s, "POST", "/api/v1/auth/logout", `{}`, token, nil, 200)
}

func TestMemberBearerSessionLimitsAndRevocation(t *testing.T) {
	s, database, admin := newCategoryTestServer(t)
	credentials := `{"username":"limitednative","password":"password123"}`
	memberTokenRequest(t, s, "POST", "/api/v1/auth/register", credentials, "", nil, 201)
	var id int64
	if err := database.QueryRow("SELECT id FROM gocms_user WHERE username='limitednative'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	setLimit := categoryRequest(t, s, admin, "PATCH", "/api/admin/site-users", fmt.Sprintf(`{"id":%d,"max_sessions":1}`, id))
	if setLimit.Code != 200 {
		t.Fatal(setLimit.Body.String())
	}
	cookie := memberTokenRequest(t, s, "POST", "/api/v1/auth/login", credentials, "", nil, 200).Result().Cookies()[0]
	// Token login must not silently replace a browser's session.
	denied := memberTokenRequest(t, s, "POST", "/api/v1/auth/token", credentials, "", cookie, 409)
	if !strings.Contains(denied.Body.String(), "session_limit_reached") || len(denied.Result().Cookies()) != 0 {
		t.Fatal("token login did not preserve the browser session")
	}
	memberTokenRequest(t, s, "POST", "/api/v1/auth/logout", `{}`, "", cookie, 200)
	token := memberAccessToken(t, memberTokenRequest(t, s, "POST", "/api/v1/auth/token", credentials, "", nil, 200))
	memberTokenRequest(t, s, "POST", "/api/v1/auth/login", credentials, "", nil, 409)
	rotated := memberAccessToken(t, memberTokenRequest(t, s, "POST", "/api/v1/auth/token", credentials, token, nil, 200))
	if rotated == token {
		t.Fatal("login must rotate the token")
	}
	memberTokenRequest(t, s, "GET", "/api/v1/me", "", token, nil, 401)
	memberTokenRequest(t, s, "GET", "/api/v1/me", "", rotated, nil, 200)
	if _, err := database.Exec("UPDATE gocms_user_session SET expires_at=? WHERE token_hash=?", time.Now().Unix(), hashToken(rotated)); err != nil {
		t.Fatal(err)
	}
	memberTokenRequest(t, s, "GET", "/api/v1/me", "", rotated, nil, 401)
	token = memberAccessToken(t, memberTokenRequest(t, s, "POST", "/api/v1/auth/token", credentials, "", nil, 200))
	if _, err := database.Exec("UPDATE gocms_user SET status='disabled' WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	memberTokenRequest(t, s, "GET", "/api/v1/me", "", token, nil, 401)
	if _, err := database.Exec("UPDATE gocms_user SET status='active' WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	revoked := categoryRequest(t, s, admin, "PATCH", "/api/admin/site-users", fmt.Sprintf(`{"id":%d,"revoke_sessions":true}`, id))
	if revoked.Code != 200 {
		t.Fatal(revoked.Body.String())
	}
	memberTokenRequest(t, s, "GET", "/api/v1/me", "", token, nil, 401)
	token = memberAccessToken(t, memberTokenRequest(t, s, "POST", "/api/v1/auth/token", credentials, "", nil, 200))
	r := httptest.NewRequest("GET", "/api/admin/stats", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("member token granted administrator access", w.Code)
	}
	key := apiKeyForTest(t, s, "gocms")
	memberTokenRequest(t, s, "GET", "/api/v1/me", "", key.Key, nil, 401)
}

func TestMemberBearerLoginRequestGuards(t *testing.T) {
	s, _, _ := newCategoryTestServer(t)
	for _, tc := range []struct {
		method, body, contentType, origin string
		want                              int
	}{
		{"GET", "", "", "", 405},
		{"POST", `{}`, "text/plain", "", 415},
		{"POST", `{}`, "application/json", "https://evil.example", 403},
		{"POST", `{"unknown":true}`, "application/json", "", 400},
		{"POST", `{"username":"` + strings.Repeat("x", 16385) + `","password":"password123"}`, "application/json", "", 400},
	} {
		r := httptest.NewRequest(tc.method, "/api/v1/auth/token", strings.NewReader(tc.body))
		r.Header.Set("Content-Type", tc.contentType)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("request guard: want %d, got %d", tc.want, w.Code)
		}
	}
	// Switching login transport must not bypass the shared rate limit.
	for i := 0; i < 21; i++ {
		path := "/api/v1/auth/token"
		if i%2 == 1 {
			path = "/api/v1/auth/login"
		}
		want := 401
		if i == 20 {
			want = 429
		}
		w := memberTokenRequest(t, s, "POST", path, `{"username":"missing","password":"wrong"}`, "", nil, want)
		if want == 429 && w.Header().Get("Retry-After") == "" {
			t.Fatal("rate limit omitted Retry-After")
		}
	}
}

func TestMemberAuthenticationMembershipConsistency(t *testing.T) {
	s, database, _ := newCategoryTestServer(t)
	credentials := `{"username":"groupuser","password":"password123"}`
	memberTokenRequest(t, s, "POST", "/api/v1/auth/register", credentials, "", nil, 201)
	if _, err := database.Exec(`UPDATE gocms_site SET domain='member.example' WHERE id=1;
INSERT INTO gocms_site(name,code,domain) VALUES('Other','other','other.example');
INSERT INTO gocms_user_group(site_id,name,slug,status) VALUES
(1,'Active','active','active'),(1,'Expired','expired','active'),
(1,'Future','future','active'),(1,'Disabled','disabled','disabled'),
(2,'Other site','other-site','active');
INSERT INTO gocms_user_group_member(user_id,group_id,started_at,expires_at)
SELECT u.id,g.id,CASE WHEN g.slug='future' THEN '2099-01-01 00:00:00' ELSE '2020-01-01 00:00:00' END,
CASE WHEN g.slug='expired' THEN '2021-01-01 00:00:00' ELSE NULL END
FROM gocms_user u CROSS JOIN gocms_user_group g WHERE u.username='groupuser'`); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"login", "token"} {
		// The configured host determines the site for the entire request, even
		// when a caller supplies another site's selector.
		response := memberTokenRequest(t, s, "POST", "https://member.example/api/v1/auth/"+endpoint+"?site_id=2", credentials, "", nil, 200)
		var payload struct {
			Data struct {
				User        siteUser `json:"user"`
				AccessToken string   `json:"access_token"`
			} `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Data.User.Groups) != 1 || payload.Data.User.Groups[0] != "active" {
			t.Fatal("login included inactive or other-site groups", payload.Data.User.Groups)
		}
		var cookie *http.Cookie
		if endpoint == "login" {
			cookie = response.Result().Cookies()[0]
			var expires int64
			if err := database.QueryRow(`SELECT expires_at FROM gocms_user_session WHERE token_hash=?`, hashToken(cookie.Value)).Scan(&expires); err != nil || cookie.Expires.Unix() != expires {
				t.Fatal("cookie expiry differs from persisted session", err)
			}
		}
		memberships := memberTokenRequest(t, s, "GET", "https://member.example/api/v1/me/memberships?site_id=2", "", payload.Data.AccessToken, cookie, 200)
		var details struct {
			Data []member.Membership `json:"data"`
		}
		if err := json.Unmarshal(memberships.Body.Bytes(), &details); err != nil {
			t.Fatal(err)
		}
		active := 0
		for _, membership := range details.Data {
			if membership.Status == "active" {
				active++
				if membership.Slug != payload.Data.User.Groups[0] {
					t.Fatal("login and membership endpoint disagree")
				}
			}
		}
		if len(details.Data) != 4 || active != 1 {
			t.Fatal("membership site or validity changed across authentication transports")
		}
	}
}

func TestMemberBearerIdentityUnbinding(t *testing.T) {
	s, database, _ := newCategoryTestServer(t)
	credentials := `{"username":"identityuser","password":"password123"}`
	memberTokenRequest(t, s, "POST", "/api/v1/auth/register", credentials, "", nil, 201)
	token := memberAccessToken(t, memberTokenRequest(t, s, "POST", "/api/v1/auth/token", credentials, "", nil, 200))
	for _, provider := range []string{"google", "facebook", "microsoft"} {
		if _, err := database.Exec(`INSERT INTO gocms_platform_identity(user_id,provider,issuer,subject)
SELECT id,?,'test-issuer','test-subject' FROM gocms_user WHERE username='identityuser'`, provider); err != nil {
			t.Fatal(err)
		}
		path := "/api/v1/me/identities/" + provider
		before := memberTokenRequest(t, s, "GET", path, "", token, nil, 200)
		if !strings.Contains(before.Body.String(), `"bound":true`) {
			t.Fatal("fixture identity is not bound")
		}
		memberTokenRequest(t, s, "DELETE", path, `{"password":"incorrect"}`, token, nil, 401)
		memberTokenRequest(t, s, "DELETE", path, `{"password":"password123"}`, token, nil, 200)
		after := memberTokenRequest(t, s, "GET", path, "", token, nil, 200)
		if !strings.Contains(after.Body.String(), `"bound":false`) {
			t.Fatal("bearer identity was not unbound")
		}
	}
}

func TestMemberAuthenticationInfrastructureFailure(t *testing.T) {
	s, database, _ := newCategoryTestServer(t)
	credentials := `{"username":"failureuser","password":"password123"}`
	memberTokenRequest(t, s, "POST", "/api/v1/auth/register", credentials, "", nil, 201)
	token := memberAccessToken(t, memberTokenRequest(t, s, "POST", "/api/v1/auth/token", credentials, "", nil, 200))
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	response := memberTokenRequest(t, s, "GET", "/api/v1/me", "", token, nil, 500)
	if !strings.Contains(response.Body.String(), `"code":"internal_error"`) || response.Header().Get("WWW-Authenticate") != "" {
		t.Fatal("infrastructure failure was misreported as an invalid login")
	}
}
