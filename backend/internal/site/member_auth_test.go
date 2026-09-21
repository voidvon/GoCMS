package site

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gocms/internal/db"
)

func TestSiteUserRegisterLoginSession(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.CreateSchema(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	s, err := New(database, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO gocms_admin_user(username,is_super) VALUES('gocms',1)`); err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if got := request(http.MethodPost, "/api/auth/register", `{"username":"alice","email":"a@example.com","password":"password123"}`, nil); got.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", got.Code, got.Body.String())
	}
	adminToken, err := s.createSession("gocms")
	if err != nil {
		t.Fatal(err)
	}
	if got := requestWithCookie(s, http.MethodPost, "/api/admin/member-groups", `{"name":"VIP","slug":"vip"}`, adminToken); got.Code != http.StatusCreated {
		t.Fatalf("member group: %d %s", got.Code, got.Body.String())
	}
	var userID, groupID int64
	if err := database.QueryRow(`SELECT id FROM gocms_user WHERE username='alice'`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT id FROM gocms_user_group WHERE slug='vip'`).Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	if got := requestWithCookie(s, http.MethodPost, "/api/admin/member-groups", fmt.Sprintf(`{"user_id":%d,"group_id":%d}`, userID, groupID), adminToken); got.Code != http.StatusOK {
		t.Fatalf("member assignment: %d %s", got.Code, got.Body.String())
	}
	login := request(http.MethodPost, "/api/auth/login", `{"identifier":"alice","password":"password123"}`, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login: %d %s", login.Code, login.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range login.Result().Cookies() {
		if c.Name == "gocms_user" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("missing user cookie")
	}
	var payload struct {
		User siteUser `json:"user"`
	}
	if json.Unmarshal(login.Body.Bytes(), &payload) != nil || payload.User.Username != "alice" {
		t.Fatal("unexpected login payload")
	}
	if got := request(http.MethodGet, "/api/auth/session", "", cookie); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"alice"`) {
		t.Fatalf("session: %d %s", got.Code, got.Body.String())
	}
	request(http.MethodPost, "/api/auth/logout", "", cookie)
	if got := request(http.MethodGet, "/api/auth/session", "", cookie); strings.Contains(got.Body.String(), `"alice"`) {
		t.Fatal("session survived logout")
	}
}

func requestWithCookie(s *Server, method, path, body, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: "gocms_admin", Value: token})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
