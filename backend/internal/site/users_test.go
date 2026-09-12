package site

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"gocms/internal/apikey"
)

func seedTestAdmin(t *testing.T, s *Server) {
	t.Helper()
	if _, err := s.database.Exec(`INSERT OR IGNORE INTO gocms_admin_user (username, is_super) VALUES ('gocms', 1)`); err != nil {
		t.Fatal(err)
	}
}

func TestAdminAccountLifecycleAndPermissions(t *testing.T) {
	s, database, root := newCategoryTestServer(t)
	call := func(token, method, path, body string, want int) string {
		t.Helper()
		r := categoryRequest(t, s, token, method, path, body)
		if r.Code != want {
			t.Fatalf("%s %s: got %d want %d: %s", method, path, r.Code, want, r.Body.String())
		}
		return r.Body.String()
	}
	call(root, "POST", "/api/admin/groups", `{"name":"Editors","permissions":["content"]}`, 200)
	var groupID int64
	if err := database.QueryRow(`SELECT id FROM gocms_admin_group WHERE name = 'Editors'`).Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	call(root, "POST", "/api/admin/users", fmt.Sprintf(`{"username":"editor","password":"test-password","group_id":%d}`, groupID), 200)
	login := call("", "POST", "/api/admin/login", `{"username":"editor","password":"test-password"}`, 200)
	var session struct {
		User AdminUser `json:"user"`
	}
	if err := json.Unmarshal([]byte(login), &session); err != nil {
		t.Fatal(err)
	}
	if session.User.IsSuper || len(session.User.Permissions) != 1 {
		t.Fatalf("unexpected privileges: %+v", session.User)
	}
	editor, err := s.createSession("editor")
	if err != nil {
		t.Fatal(err)
	}
	call(editor, "GET", "/api/admin/content", "", 200)
	for _, path := range []string{"/api/admin/users", "/api/admin/groups", "/api/admin/theme", "/api/admin/templates", "/api/admin/api-keys", "/api/admin/update/check"} {
		call(editor, "GET", path, "", 403)
	}
	call(editor, "POST", "/api/admin/categories", `{}`, 403)
	call(root, "DELETE", "/api/admin/groups", fmt.Sprintf(`{"id":%d}`, groupID), 400)
	call(root, "PUT", "/api/admin/groups", fmt.Sprintf(`{"id":%d,"name":"Editors","permissions":[]}`, groupID), 200)
	call(editor, "GET", "/api/admin/content", "", 403)
	key, err := apikey.Create(context.Background(), database, session.User.ID, session.User.ID, apikey.CreateInput{Name: "editor"}, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	_ = key
	call(root, "PUT", "/api/admin/users", fmt.Sprintf(`{"id":%d,"username":"editor","group_id":%d,"disabled":true}`, session.User.ID, groupID), 200)
	call(editor, "GET", "/api/admin/session", "", 401)
	call("", "POST", "/api/admin/login", `{"username":"editor","password":"test-password"}`, 401)
	var liveKeys int
	if err := database.QueryRow(`SELECT COUNT(*) FROM gocms_api_key WHERE admin_id = ? AND revoked_at IS NULL`, session.User.ID).Scan(&liveKeys); err != nil || liveKeys != 0 {
		t.Fatalf("keys not revoked: %d, %v", liveKeys, err)
	}
	call(root, "DELETE", "/api/admin/users", fmt.Sprintf(`{"id":%d}`, session.User.ID), 200)
	call(root, "DELETE", "/api/admin/groups", fmt.Sprintf(`{"id":%d}`, groupID), 200)
	var rootID int64
	if err := database.QueryRow(`SELECT id FROM gocms_admin_user WHERE username = 'gocms'`).Scan(&rootID); err != nil {
		t.Fatal(err)
	}
	call(root, "DELETE", "/api/admin/users", fmt.Sprintf(`{"id":%d}`, rootID), 400)
	call(root, "PUT", "/api/admin/users", fmt.Sprintf(`{"id":%d,"username":"gocms","is_super":true,"disabled":true}`, rootID), 400)
	call(root, "POST", "/api/admin/groups", `{"name":"Bad","permissions":["users"]}`, 400)
	call(root, "POST", "/api/admin/users", `{"username":"bad","password":"test-password","group_id":99999}`, 400)
}

func TestDeletedSessionUserCannotAuthenticate(t *testing.T) {
	s, database, token := newCategoryTestServer(t)
	if _, err := database.Exec(`DELETE FROM gocms_admin_user WHERE username = 'gocms'`); err != nil {
		t.Fatal(err)
	}
	if response := categoryRequest(t, s, token, http.MethodGet, "/api/admin/session", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("deleted user accepted: %d", response.Code)
	}
}

func TestContentActionPermissions(t *testing.T) {
	s, database, root := newCategoryTestServer(t)
	if _, err := database.Exec(`INSERT INTO gocms_admin_group (name, permissions) VALUES ('writer', '["content","content.add","content.edit"]');
	INSERT INTO gocms_admin_user (username, group_id) SELECT 'writer', id FROM gocms_admin_group WHERE name = 'writer'`); err != nil {
		t.Fatal(err)
	}
	token, err := s.createSession("writer")
	if err != nil {
		t.Fatal(err)
	}
	res := categoryRequest(t, s, root, "POST", "/api/admin/categories", `{"name":"任意栏目","list_template":"category_list.html","detail_template":"content_detail.html"}`)
	if res.Code != 200 {
		t.Fatalf("category setup: %d %s", res.Code, res.Body.String())
	}
	category := categoryID(t, database, "任意栏目")
	body := fmt.Sprintf(`{"title":"待确认文章","category_id":%d,"visible":0}`, category)
	res = categoryRequest(t, s, token, "POST", "/api/admin/content", body)
	if res.Code != 200 {
		t.Fatalf("save hidden: %d %s", res.Code, res.Body.String())
	}
	var id int64
	if err := database.QueryRow(`SELECT id FROM gocms_content WHERE title = '待确认文章'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/admin/content/%d", id)
	publicBody := fmt.Sprintf(`{"title":"待确认文章","category_id":%d,"visible":1}`, category)
	for _, method := range []string{"PUT", "PATCH"} {
		if res := categoryRequest(t, s, token, method, path, publicBody); res.Code != 403 {
			t.Fatalf("unreviewed public save: %d", res.Code)
		}
	}
	if res := categoryRequest(t, s, token, "DELETE", path, ""); res.Code != 403 {
		t.Fatalf("unprivileged delete: %d", res.Code)
	}
	if res := categoryRequest(t, s, root, "PUT", path, publicBody); res.Code != 200 {
		t.Fatalf("review: %d %s", res.Code, res.Body.String())
	}
	if res := categoryRequest(t, s, token, "PUT", path, body); res.Code != 403 {
		t.Fatalf("public content changed by writer: %d", res.Code)
	}
	if _, err := database.Exec(`UPDATE gocms_admin_group SET permissions = '["content"]' WHERE name = 'writer'`); err != nil {
		t.Fatal(err)
	}
	if res := categoryRequest(t, s, token, "POST", "/api/admin/content", body); res.Code != 403 {
		t.Fatalf("unprivileged create: %d", res.Code)
	}
	if res := categoryRequest(t, s, token, "GET", "/api/admin/content", ""); res.Code != 200 {
		t.Fatalf("read-only content: %d", res.Code)
	}
}

func TestContentCategoryScope(t *testing.T) {
	s, database, root := newCategoryTestServer(t)
	for _, name := range []string{"Allowed", "Other"} {
		r := categoryRequest(t, s, root, "POST", "/api/admin/categories", fmt.Sprintf(`{"name":%q,"list_template":"category_list.html","detail_template":"content_detail.html"}`, name))
		if r.Code != 200 {
			t.Fatalf("category: %s", r.Body.String())
		}
	}
	allowed, other := categoryID(t, database, "Allowed"), categoryID(t, database, "Other")
	for _, category := range []int64{allowed, other} {
		r := categoryRequest(t, s, root, "POST", "/api/admin/content", fmt.Sprintf(`{"title":"Article","category_id":%d,"visible":0}`, category))
		if r.Code != 200 {
			t.Fatalf("content: %s", r.Body.String())
		}
	}
	if _, err := database.Exec(`INSERT INTO gocms_admin_group (name, permissions) VALUES ('scoped', '["content","content.add","content.edit","content.delete","content.review"]');
	INSERT INTO gocms_admin_user (username, group_id) SELECT 'scoped', id FROM gocms_admin_group WHERE name = 'scoped'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE gocms_admin_user SET category_ids = ? WHERE username = 'scoped'`, fmt.Sprintf("[%d]", allowed)); err != nil {
		t.Fatal(err)
	}
	token, err := s.createSession("scoped")
	if err != nil {
		t.Fatal(err)
	}
	var deniedID, allowedID int64
	if err := database.QueryRow(`SELECT id FROM gocms_content WHERE category_id = ?`, other).Scan(&deniedID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT id FROM gocms_content WHERE category_id = ?`, allowed).Scan(&allowedID); err != nil {
		t.Fatal(err)
	}
	response := categoryRequest(t, s, token, "GET", "/api/admin/content", "")
	var page ContentPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].Category != allowed {
		t.Fatalf("scope leaked: %s (%v)", response.Body.String(), err)
	}
	path := fmt.Sprintf("/api/admin/content/%d", deniedID)
	if r := categoryRequest(t, s, token, "GET", path, ""); r.Code != 404 {
		t.Fatalf("detail leaked: %d", r.Code)
	}
	if r := categoryRequest(t, s, token, "DELETE", path, ""); r.Code != 403 {
		t.Fatalf("delete allowed: %d", r.Code)
	}
	for _, test := range []struct {
		path     string
		category int64
	}{{path, allowed}, {fmt.Sprintf("/api/admin/content/%d", allowedID), other}} {
		r := categoryRequest(t, s, token, "PUT", test.path, fmt.Sprintf(`{"title":"Moved","category_id":%d}`, test.category))
		if r.Code != 403 {
			t.Fatalf("cross-scope move allowed: %d", r.Code)
		}
	}
	if _, err := database.Exec(`UPDATE gocms_admin_user SET category_ids = '[]' WHERE username = 'scoped'`); err != nil {
		t.Fatal(err)
	}
	response = categoryRequest(t, s, token, "GET", "/api/admin/content", "")
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil || page.Total != 0 {
		t.Fatalf("empty scope leaked: %s", response.Body.String())
	}
}
