package site

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gocms/internal/db"
)

func TestFirstRunAdminBootstrap(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := db.CreateSchema(context.Background(), database); err != nil {
		t.Fatal(err)
	}

	server, err := New(database, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	request := func(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, req)
		return recorder
	}

	// 1. Initial status: needs_setup must be true
	statusRes := request(http.MethodGet, "/api/admin/setup-status", "", nil)
	if statusRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", statusRes.Code, statusRes.Body.String())
	}
	var statusData map[string]any
	if err := json.Unmarshal(statusRes.Body.Bytes(), &statusData); err != nil {
		t.Fatal(err)
	}
	if statusData["needs_setup"] != true {
		t.Fatalf("expected needs_setup true, got %v", statusData["needs_setup"])
	}

	// 2. Empty username rejected
	emptyUserRes := request(http.MethodPost, "/api/admin/login", `{"username":"  ","password":"password123"}`, nil)
	if emptyUserRes.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty username, got %d: %s", emptyUserRes.Code, emptyUserRes.Body.String())
	}

	// 3. Short password rejected
	shortPassRes := request(http.MethodPost, "/api/admin/login", `{"username":"admin","password":"123"}`, nil)
	if shortPassRes.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for short password, got %d: %s", shortPassRes.Code, shortPassRes.Body.String())
	}

	// 4. Valid credentials initialize super administrator
	initRes := request(http.MethodPost, "/api/admin/login", `{"username":"superadmin","password":"password123"}`, nil)
	if initRes.Code != http.StatusOK {
		t.Fatalf("expected 200 for bootstrap login, got %d: %s", initRes.Code, initRes.Body.String())
	}

	var initData struct {
		User AdminUser `json:"user"`
	}
	if err := json.Unmarshal(initRes.Body.Bytes(), &initData); err != nil {
		t.Fatal(err)
	}
	if initData.User.Username != "superadmin" {
		t.Fatalf("expected username superadmin, got %s", initData.User.Username)
	}
	if !initData.User.IsSuper {
		t.Fatal("expected user to be super administrator")
	}

	var sessionCookie *http.Cookie
	for _, c := range initRes.Result().Cookies() {
		if c.Name == "gocms_admin" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatal("expected session cookie to be set")
	}

	// 5. Setup status now returns needs_setup: false
	statusAfterRes := request(http.MethodGet, "/api/admin/setup-status", "", nil)
	if statusAfterRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", statusAfterRes.Code, statusAfterRes.Body.String())
	}
	var statusAfterData map[string]any
	if err := json.Unmarshal(statusAfterRes.Body.Bytes(), &statusAfterData); err != nil {
		t.Fatal(err)
	}
	if statusAfterData["needs_setup"] != false {
		t.Fatalf("expected needs_setup false, got %v", statusAfterData["needs_setup"])
	}

	// 6. Session request verifies authenticated user
	sessionRes := request(http.MethodGet, "/api/admin/session", "", sessionCookie)
	if sessionRes.Code != http.StatusOK {
		t.Fatalf("expected 200 for session, got %d: %s", sessionRes.Code, sessionRes.Body.String())
	}

	// 7. Subsequent login with nonexistent account returns 401 (bootstrap channel closed)
	unknownRes := request(http.MethodPost, "/api/admin/login", `{"username":"another","password":"password123"}`, nil)
	if unknownRes.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unknown user after bootstrap, got %d: %s", unknownRes.Code, unknownRes.Body.String())
	}

	// 8. Subsequent login with superadmin succeeds
	reLoginRes := request(http.MethodPost, "/api/admin/login", `{"username":"superadmin","password":"password123"}`, nil)
	if reLoginRes.Code != http.StatusOK {
		t.Fatalf("expected 200 for subsequent login, got %d: %s", reLoginRes.Code, reLoginRes.Body.String())
	}

	// 9. Verify database state directly
	var isSuper, disabled int
	var categoryIDs string
	if err := database.QueryRow(`SELECT is_super, disabled, category_ids FROM gocms_admin_user WHERE username = 'superadmin'`).Scan(&isSuper, &disabled, &categoryIDs); err != nil {
		t.Fatal(err)
	}
	if isSuper != 1 {
		t.Fatalf("expected is_super = 1 in database, got %d", isSuper)
	}
	if disabled != 0 {
		t.Fatalf("expected disabled = 0 in database, got %d", disabled)
	}
	if categoryIDs != "null" {
		t.Fatalf("expected category_ids = 'null', got %s", categoryIDs)
	}
}
