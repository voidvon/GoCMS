package site

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMemberV1Lifecycle(t *testing.T) {
	s, _, _ := newCategoryTestServer(t)
	call := func(method, path, body string, cookie *http.Cookie, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("sensitive response cached")
		}
		return w
	}
	call("POST", "/api/v1/auth/register", `{"username":"alice","password":"initial-password"}`, nil, 201)
	login := call("POST", "/api/v1/auth/login", `{"username":"alice","password":"initial-password"}`, nil, 200)
	cookie := login.Result().Cookies()[0]
	call("GET", "/api/v1/me", "", nil, 401)
	call("PATCH", "/api/v1/me", `{"status":"active","groups":["vip"]}`, cookie, 400)
	call("PATCH", "/api/v1/me", `{"display_name":"Alice","avatar_url":"https://example.org/avatar.png"}`, cookie, 200)
	me := call("GET", "/api/v1/me", "", cookie, 200)
	if !strings.Contains(me.Body.String(), "Alice") || strings.Contains(me.Body.String(), "password") {
		t.Fatal(me.Body.String())
	}
	sessions := call("GET", "/api/v1/me/sessions", "", cookie, 200)
	var response struct {
		Data []struct {
			ID string `json:"id"`
		}
	}
	if err := json.Unmarshal(sessions.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data) != 1 {
		t.Fatal("missing session")
	}
	call("POST", "/api/v1/auth/register", `{"username":"other","password":"initial-password"}`, nil, 201)
	other := call("POST", "/api/v1/auth/login", `{"username":"other","password":"initial-password"}`, nil, 200).Result().Cookies()[0]
	call("DELETE", "/api/v1/me/sessions/"+response.Data[0].ID, "", other, 404)
	call("PUT", "/api/v1/me/password", `{"old_password":"wrong","new_password":"new-password"}`, cookie, 401)
	call("PUT", "/api/v1/me/password", `{"old_password":"initial-password","new_password":"new-password"}`, cookie, 200)
	call("GET", "/api/v1/me", "", cookie, 401)
	call("POST", "/api/v1/auth/login", `{"username":"alice","password":"initial-password"}`, nil, 401)
	fresh := call("POST", "/api/v1/auth/login", `{"username":"alice","password":"new-password"}`, nil, 200).Result().Cookies()[0]
	call("GET", "/api/v1/me/memberships", "", fresh, 200)
	call("GET", "/api/v1/auth/logout", "", fresh, 405)
	r := httptest.NewRequest("PATCH", "/api/v1/me", strings.NewReader(`{"display_name":"forged"}`))
	r.Header.Set("Origin", "https://evil.example")
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(fresh)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross origin mutation allowed")
	}
}
func TestMemberLoginLimit(t *testing.T) {
	s, _, _ := newCategoryTestServer(t)
	for i := 0; i < 21; i++ {
		r := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"username":"missing","password":"wrong"}`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		want := 401
		if i == 20 {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("attempt %d: %d %s", i, w.Code, w.Body.String())
		}
	}
}

func TestMemberSessionLimitAPI(t *testing.T) {
	s, _, root := newCategoryTestServer(t)
	call := func(path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if w := call("/api/v1/auth/register", `{"username":"limituser","password":"password123"}`, nil); w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	var id int64
	if err := s.database.QueryRow("SELECT id FROM gocms_user WHERE username='limituser'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"id":%d,"max_sessions":1}`, id)
	w := categoryRequest(t, s, root, "PATCH", "/api/admin/site-users", body)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	credentials := `{"username":"limituser","password":"password123"}`
	first := call("/api/v1/auth/login", credentials, nil)
	if first.Code != 200 {
		t.Fatal(first.Body.String())
	}
	cookie := first.Result().Cookies()[0]
	for _, path := range []string{"/api/v1/auth/login", "/api/auth/login"} {
		denied := call(path, credentials, nil)
		if denied.Code != 409 || !strings.Contains(denied.Body.String(), "session_limit_reached") || len(denied.Result().Cookies()) != 0 {
			t.Fatal(denied.Code, denied.Body.String())
		}
	}
	rotated := call("/api/v1/auth/login", credentials, cookie)
	if rotated.Code != 200 || rotated.Result().Cookies()[0].Value == cookie.Value {
		t.Fatal("rotation failed")
	}
	w = categoryRequest(t, s, root, "PATCH", "/api/admin/site-users", fmt.Sprintf(`{"id":%d,"revoke_sessions":true}`, id))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := call("/api/v1/auth/login", credentials, nil); w.Code != 200 {
		t.Fatal("revocation did not release slot")
	}
}
