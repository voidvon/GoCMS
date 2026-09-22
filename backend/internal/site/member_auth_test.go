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

func TestDefaultMemberGroupRegistration(t *testing.T) {
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
	if _, err := database.Exec(`INSERT INTO gocms_admin_user(username,is_super) VALUES('super',1)`); err != nil {
		t.Fatal(err)
	}
	adminToken, err := s.createSession("super")
	if err != nil {
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

	// 1. Without default group: bob registers
	reg1 := request(http.MethodPost, "/api/auth/register", `{"username":"bob","password":"password123"}`, nil)
	if reg1.Code != http.StatusCreated {
		t.Fatalf("bob register failed: %d %s", reg1.Code, reg1.Body.String())
	}
	var res1 struct {
		User siteUser `json:"user"`
	}
	if err := json.Unmarshal(reg1.Body.Bytes(), &res1); err != nil {
		t.Fatal(err)
	}
	if len(res1.User.Groups) != 0 {
		t.Fatalf("expected bob to have 0 groups, got: %v", res1.User.Groups)
	}

	// 2. Admin creates standard group with is_default = true
	g1 := requestWithCookie(s, http.MethodPost, "/api/admin/member-groups", `{"name":"Standard","slug":"standard","is_default":true}`, adminToken)
	if g1.Code != http.StatusCreated {
		t.Fatalf("create standard group failed: %d %s", g1.Code, g1.Body.String())
	}

	// 3. Admin creates vip group with is_default = false
	g2 := requestWithCookie(s, http.MethodPost, "/api/admin/member-groups", `{"name":"VIP","slug":"vip","is_default":false}`, adminToken)
	if g2.Code != http.StatusCreated {
		t.Fatalf("create vip group failed: %d %s", g2.Code, g2.Body.String())
	}

	// 4. Verify list groups has correct is_default flags
	listG := requestWithCookie(s, http.MethodGet, "/api/admin/member-groups", "", adminToken)
	if listG.Code != http.StatusOK {
		t.Fatalf("list groups failed: %d %s", listG.Code, listG.Body.String())
	}
	var groupList struct {
		Items []memberGroup `json:"items"`
	}
	if err := json.Unmarshal(listG.Body.Bytes(), &groupList); err != nil {
		t.Fatal(err)
	}
	var standardID, vipID int64
	for _, it := range groupList.Items {
		if it.Slug == "standard" {
			standardID = it.ID
			if !it.IsDefault {
				t.Fatalf("expected standard group to be default")
			}
		}
		if it.Slug == "vip" {
			vipID = it.ID
			if it.IsDefault {
				t.Fatalf("expected vip group NOT to be default")
			}
		}
	}

	// 5. Charlie registers -> should automatically get standard group
	reg2 := request(http.MethodPost, "/api/auth/register", `{"username":"charlie","password":"password123"}`, nil)
	if reg2.Code != http.StatusCreated {
		t.Fatalf("charlie register failed: %d %s", reg2.Code, reg2.Body.String())
	}
	var res2 struct {
		User siteUser `json:"user"`
	}
	if err := json.Unmarshal(reg2.Body.Bytes(), &res2); err != nil {
		t.Fatal(err)
	}
	if len(res2.User.Groups) != 1 || res2.User.Groups[0] != "standard" {
		t.Fatalf("expected charlie to have [standard], got: %v", res2.User.Groups)
	}

	// Login charlie -> user.groups contains standard
	login2 := request(http.MethodPost, "/api/auth/login", `{"identifier":"charlie","password":"password123"}`, nil)
	if login2.Code != http.StatusOK {
		t.Fatalf("charlie login failed: %d %s", login2.Code, login2.Body.String())
	}
	var loginRes2 struct {
		User siteUser `json:"user"`
	}
	_ = json.Unmarshal(login2.Body.Bytes(), &loginRes2)
	if len(loginRes2.User.Groups) != 1 || loginRes2.User.Groups[0] != "standard" {
		t.Fatalf("expected charlie login to have [standard], got: %v", loginRes2.User.Groups)
	}

	// 6. Admin updates vip group to is_default = true via PATCH
	patchVIP := requestWithCookie(s, http.MethodPatch, "/api/admin/member-groups", fmt.Sprintf(`{"id":%d,"name":"VIP","slug":"vip","is_default":true}`, vipID), adminToken)
	if patchVIP.Code != http.StatusCreated {
		t.Fatalf("patch vip failed: %d %s", patchVIP.Code, patchVIP.Body.String())
	}

	// Verify standard is no longer default, vip is now default
	listG2 := requestWithCookie(s, http.MethodGet, "/api/admin/member-groups", "", adminToken)
	var groupList2 struct {
		Items []memberGroup `json:"items"`
	}
	_ = json.Unmarshal(listG2.Body.Bytes(), &groupList2)
	for _, it := range groupList2.Items {
		if it.Slug == "standard" && it.IsDefault {
			t.Fatalf("standard group should no longer be default")
		}
		if it.Slug == "vip" && !it.IsDefault {
			t.Fatalf("vip group should now be default")
		}
	}

	// 7. David registers -> should get vip group
	reg3 := request(http.MethodPost, "/api/auth/register", `{"username":"david","password":"password123"}`, nil)
	if reg3.Code != http.StatusCreated {
		t.Fatalf("david register failed: %d %s", reg3.Code, reg3.Body.String())
	}
	var res3 struct {
		User siteUser `json:"user"`
	}
	_ = json.Unmarshal(reg3.Body.Bytes(), &res3)
	if len(res3.User.Groups) != 1 || res3.User.Groups[0] != "vip" {
		t.Fatalf("expected david to have [vip], got: %v", res3.User.Groups)
	}

	// 8. Admin disables vip group -> eve registers -> no default group assigned because it's disabled
	patchVIPDisable := requestWithCookie(s, http.MethodPatch, "/api/admin/member-groups", fmt.Sprintf(`{"id":%d,"name":"VIP","slug":"vip","status":"disabled","is_default":true}`, vipID), adminToken)
	if patchVIPDisable.Code != http.StatusCreated {
		t.Fatalf("patch vip disable failed: %d %s", patchVIPDisable.Code, patchVIPDisable.Body.String())
	}
	reg4 := request(http.MethodPost, "/api/auth/register", `{"username":"eve","password":"password123"}`, nil)
	if reg4.Code != http.StatusCreated {
		t.Fatalf("eve register failed: %d %s", reg4.Code, reg4.Body.String())
	}
	var res4 struct {
		User siteUser `json:"user"`
	}
	_ = json.Unmarshal(reg4.Body.Bytes(), &res4)
	if len(res4.User.Groups) != 0 {
		t.Fatalf("expected eve to have no groups since vip is disabled, got: %v", res4.User.Groups)
	}

	_ = standardID
}

