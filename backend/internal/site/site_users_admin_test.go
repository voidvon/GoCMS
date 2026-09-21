package site

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gocms/internal/apikey"
)

func TestSiteUserManagementBoundaryAndRevocation(t *testing.T) {
	s, database, root := newCategoryTestServer(t)
	result, err := database.Exec("INSERT INTO gocms_user(username,password_hash) VALUES('member','unused')")
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO gocms_user_session(token_hash,user_id,expires_at) VALUES(?,?,?)", hashToken("member-token"), id, time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	call := func(token, method, body string, want int) string {
		t.Helper()
		w := categoryRequest(t, s, token, method, "/api/admin/site-users", body)
		if w.Code != want {
			t.Fatalf("%s: got %d want %d: %s", method, w.Code, want, w.Body.String())
		}
		return w.Body.String()
	}
	call("", "GET", "", 401)
	if _, err := database.Exec("INSERT INTO gocms_admin_user(username) VALUES('editor')"); err != nil {
		t.Fatal(err)
	}
	editor, err := s.createSession("editor")
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "PATCH", "DELETE"} {
		call(editor, method, fmt.Sprintf(`{"id":%d,"status":"disabled"}`, id), 403)
	}
	var adminID int64
	if err := database.QueryRow("SELECT id FROM gocms_admin_user WHERE username='gocms'").Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	key, err := apikey.Create(context.Background(), database, adminID, adminID, apikey.CreateInput{Name: "root-key"}, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "PATCH", "DELETE"} {
		r := httptest.NewRequest(method, "/api/admin/site-users", strings.NewReader(fmt.Sprintf(`{"id":%d,"status":"disabled"}`, id)))
		r.Header.Set("X-API-Key", key.Key)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("API key %s: %d", method, w.Code)
		}
	}
	// A member cookie cannot authenticate an administrator request.
	r := httptest.NewRequest("GET", "/api/admin/site-users", nil)
	r.AddCookie(&http.Cookie{Name: "gocms_user", Value: "member-token"})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("member gained admin access: %d", w.Code)
	}
	body := call(root, "GET", "", 200)
	if !strings.Contains(body, "member") || strings.Contains(body, "password_hash") {
		t.Fatal("incorrect public account fields")
	}
	call(root, "PATCH", fmt.Sprintf(`{"id":%d,"status":"disabled"}`, id), 200)
	call(root, "PATCH", fmt.Sprintf(`{"id":%d,"status":"active"}`, id), 200)
	r = httptest.NewRequest("GET", "/api/auth/session", nil)
	r.AddCookie(&http.Cookie{Name: "gocms_user", Value: "member-token"})
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), `"user":null`) {
		t.Fatal("old session revived")
	}
	call(root, "DELETE", fmt.Sprintf(`{"id":%d}`, id), 200)
	call(root, "DELETE", fmt.Sprintf(`{"id":%d}`, id), 404)
}
