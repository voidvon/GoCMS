package site

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
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

func TestPlatformAdminRequiresSuperAdministrator(t *testing.T) {
	s, database, root := newCategoryTestServer(t)
	if _, err := database.Exec(`
		INSERT INTO gocms_site (id, name, code) VALUES (2, 'Other site', 'other');
		INSERT INTO gocms_admin_group (name, permissions, site_permissions) VALUES
			('Legacy editor', '["content","models","logs","login_logs","security","security.manage"]',
				'{"1":["content","categories","models","logs","login_logs","security","security.manage"],"2":["content"]}'),
			('Wildcard editor', '["*"]', '{"1":["*"]}');
		INSERT INTO gocms_admin_user (username, group_id)
			SELECT name, id FROM gocms_admin_group WHERE name IN ('Legacy editor','Wildcard editor')`); err != nil {
		t.Fatal(err)
	}
	for _, username := range []string{"Legacy editor", "Wildcard editor"} {
		t.Run(username, func(t *testing.T) {
			token, err := s.createSession(username)
			if err != nil {
				t.Fatal(err)
			}
			key := apiKeyForTest(t, s, username)
			for _, tc := range []struct{ method, route string }{
				{"GET", "security/policy"}, {"GET", "SECURITY/nginx/config"}, {"POST", "security/entries"},
				{"GET", "models"}, {"POST", "models"}, {"GET", "model-fields"}, {"PUT", "model-fields/1"},
				{"GET", "model-tables"}, {"DELETE", "model-tables/1"}, {"GET", "logs"}, {"GET", "logins"},
				{"POST", "logs/clear"}, {"GET", "sites"}, {"GET", "sites/1"}, {"GET", "site-settings"},
				{"PUT", "site-settings"}, {"GET", "mail"}, {"GET", "member-login"}, {"GET", "geography"},
				{"GET", "database-backup/objects/download"}, {"GET", "users"}, {"GET", "groups"}, {"GET", "api-keys"},
			} {
				path := "/api/admin/" + tc.route
				for _, result := range []*httptest.ResponseRecorder{
					siteAdminRequest(s, token, tc.method, path, "1", "{}"),
					apiKeyRequest(t, s, key.Key, tc.method, path, "{}"),
				} {
					if result.Code != http.StatusForbidden {
						t.Errorf("%s %s: got %d: %s", tc.method, path, result.Code, result.Body)
					}
				}
			}
		})
	}
	token, err := s.createSession("Legacy editor")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		route    string
		siteID   string
		want     int
		contains string
	}{
		{"site-options", "999", 200, `"name":"Other site"`},
		{"content/editor-schema", "1", 200, `"entry_fields"`},
		{"content/editor-schema", "2", 200, `"field_name"`},
		{"categories/editor-options", "1", 200, `"template_files"`},
		{"categories/editor-options", "2", 403, ""},
	} {
		result := siteAdminRequest(s, token, "GET", "/api/admin/"+tc.route, tc.siteID, "")
		if result.Code != tc.want || !strings.Contains(result.Body.String(), tc.contains) {
			t.Errorf("%s for site %s: got %d: %s", tc.route, tc.siteID, result.Code, result.Body)
		}
	}

	session := categoryRequest(t, s, token, "GET", "/api/admin/session", "")
	var identity struct{ User AdminUser }
	if err := json.Unmarshal(session.Body.Bytes(), &identity); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(identity.User.Permissions, []string{"content"}) ||
		!slices.Equal(identity.User.SitePermissions[1], []string{"content", "categories"}) {
		t.Fatalf("unexpected session permissions: %+v", identity.User)
	}
	groups := categoryRequest(t, s, root, "GET", "/api/admin/groups", "")
	for _, key := range []string{"models", "logs", "login_logs", "security", "security.manage"} {
		if strings.Contains(groups.Body.String(), `"`+key+`"`) {
			t.Errorf("retired permission %q returned: %s", key, groups.Body)
		}
	}
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

func TestAdminAccountSitePermissions(t *testing.T) {
	s, database, root := newCategoryTestServer(t)

	// Create Site 2
	if _, err := database.Exec(`INSERT INTO gocms_site (id, name, code, domain) VALUES (2, 'Site 2', 'site2', 'site2.test')`); err != nil {
		t.Fatal(err)
	}

	call := func(token, method, path, body string, want int) string {
		t.Helper()
		r := categoryRequest(t, s, token, method, path, body)
		if r.Code != want {
			t.Fatalf("%s %s: got %d want %d: %s", method, path, r.Code, want, r.Body.String())
		}
		return r.Body.String()
	}

	// Create editor group
	call(root, "POST", "/api/admin/groups", `{"name":"Site2Only","permissions":["content","categories"]}`, 200)
	var groupID int64
	if err := database.QueryRow(`SELECT id FROM gocms_admin_group WHERE name = 'Site2Only'`).Scan(&groupID); err != nil {
		t.Fatal(err)
	}

	// Create user with site_ids: [2]
	call(root, "POST", "/api/admin/users", fmt.Sprintf(`{"username":"site2editor","password":"test-password","group_id":%d,"site_ids":[2]}`, groupID), 200)

	editorToken, err := s.createSession("site2editor")
	if err != nil {
		t.Fatal(err)
	}

	// 1. The site switcher should only show Site 2.
	sitesResp := call(editorToken, "GET", "/api/admin/site-options", "", 200)
	var siteList []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(sitesResp), &siteList); err != nil {
		t.Fatal(err)
	}
	if len(siteList) != 1 || siteList[0].ID != 2 {
		t.Fatalf("expected only site 2, got: %s", sitesResp)
	}

	// 2. Accessing Site 2 categories should succeed
	call(editorToken, "GET", "/api/admin/categories?site_id=2", "", 200)

	// 3. Accessing Site 1 categories should fail with 403 Forbidden
	call(editorToken, "GET", "/api/admin/categories?site_id=1", "", 403)

	// 4. Update user to have site_ids: null (all sites)
	var userID int64
	if err := database.QueryRow(`SELECT id FROM gocms_admin_user WHERE username = 'site2editor'`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	call(root, "PUT", "/api/admin/users", fmt.Sprintf(`{"id":%d,"username":"site2editor","group_id":%d,"site_ids":null}`, userID, groupID), 200)

	// In GoCMS, updating an account revokes its existing sessions.
	editorToken, err = s.createSession("site2editor")
	if err != nil {
		t.Fatal(err)
	}

	// Now editor should see both sites
	sitesRespAll := call(editorToken, "GET", "/api/admin/site-options", "", 200)
	var allSitesList []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(sitesRespAll), &allSitesList); err != nil {
		t.Fatal(err)
	}
	if len(allSitesList) != 2 {
		t.Fatalf("expected 2 sites for all-sites admin, got: %s", sitesRespAll)
	}
	call(editorToken, "GET", "/api/admin/categories?site_id=1", "", 200)
	call(editorToken, "GET", "/api/admin/categories?site_id=2", "", 200)
}

func TestAdminGroupSitePermissions(t *testing.T) {
	s, database, root := newCategoryTestServer(t)

	// Create Site 2
	if _, err := database.Exec(`INSERT INTO gocms_site (id, name, code, is_default, status) VALUES (2, 'Site 2', 'site2', 0, 'active')`); err != nil {
		t.Fatal(err)
	}

	call := func(token, method, path, body string, want int) string {
		t.Helper()
		r := categoryRequest(t, s, token, method, path, body)
		if r.Code != want {
			t.Fatalf("%s %s: got %d want %d: %s", method, path, r.Code, want, r.Body.String())
		}
		return r.Body.String()
	}

	// 1. Create a user group configured with site_ids: [2]
	call(root, "POST", "/api/admin/groups", `{"name":"Site2Group","permissions":["content","categories"],"site_ids":[2]}`, 200)
	var groupID int64
	if err := database.QueryRow(`SELECT id FROM gocms_admin_group WHERE name = 'Site2Group'`).Scan(&groupID); err != nil {
		t.Fatal(err)
	}

	// 2. Create account belonging to Site2Group without setting account-level site_ids
	call(root, "POST", "/api/admin/users", fmt.Sprintf(`{"username":"groupeditor","password":"test-password","group_id":%d}`, groupID), 200)

	editorToken, err := s.createSession("groupeditor")
	if err != nil {
		t.Fatal(err)
	}

	// Editor should only see Site 2
	sitesResp := call(editorToken, "GET", "/api/admin/site-options", "", 200)
	var siteList []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(sitesResp), &siteList); err != nil {
		t.Fatal(err)
	}
	if len(siteList) != 1 || siteList[0].ID != 2 {
		t.Fatalf("expected only site 2 via group, got: %s", sitesResp)
	}

	call(editorToken, "GET", "/api/admin/categories?site_id=2", "", 200)
	call(editorToken, "GET", "/api/admin/categories?site_id=1", "", 403)

	// 3. Update the group to site_ids: [1, 2]
	call(root, "PUT", "/api/admin/groups", fmt.Sprintf(`{"id":%d,"name":"Site2Group","permissions":["content","categories"],"site_ids":[1,2]}`, groupID), 200)

	// Editor now has access to both sites
	sitesRespAll := call(editorToken, "GET", "/api/admin/site-options", "", 200)
	var allSitesList []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(sitesRespAll), &allSitesList); err != nil {
		t.Fatal(err)
	}
	if len(allSitesList) != 2 {
		t.Fatalf("expected 2 sites after group update, got: %s", sitesRespAll)
	}
	call(editorToken, "GET", "/api/admin/categories?site_id=1", "", 200)
	call(editorToken, "GET", "/api/admin/categories?site_id=2", "", 200)
}

func TestAdminGroupPerSiteModulePermissions(t *testing.T) {
	s, database, root := newCategoryTestServer(t)

	// Create Site 2 and Site 3
	if _, err := database.Exec(`INSERT INTO gocms_site (id, name, code, is_default, status) VALUES (2, 'Site 2', 'site2', 0, 'active'), (3, 'Site 3', 'site3', 0, 'active')`); err != nil {
		t.Fatal(err)
	}

	call := func(token, method, path, body string, want int) string {
		t.Helper()
		r := categoryRequest(t, s, token, method, path, body)
		if r.Code != want {
			t.Fatalf("%s %s: got %d want %d: %s", method, path, r.Code, want, r.Body.String())
		}
		return r.Body.String()
	}

	// 1. Create a user group with per-site permissions:
	// Site 1 has "content", "theme"
	// Site 2 has only "content"
	// Site 3 is omitted (no access)
	call(root, "POST", "/api/admin/groups", `{
		"name": "MatrixGroup",
		"site_permissions": {
			"1": ["content", "theme"],
			"2": ["content"]
		}
	}`, 200)
	var groupID int64
	if err := database.QueryRow(`SELECT id FROM gocms_admin_group WHERE name = 'MatrixGroup'`).Scan(&groupID); err != nil {
		t.Fatal(err)
	}

	// 2. Create account belonging to MatrixGroup
	call(root, "POST", "/api/admin/users", fmt.Sprintf(`{"username":"matrixuser","password":"test-password","group_id":%d}`, groupID), 200)

	userToken, err := s.createSession("matrixuser")
	if err != nil {
		t.Fatal(err)
	}

	// 3. Check sites: user should only see Site 1 and Site 2 (not Site 3)
	sitesResp := call(userToken, "GET", "/api/admin/site-options", "", 200)
	var siteList []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(sitesResp), &siteList); err != nil {
		t.Fatal(err)
	}
	if len(siteList) != 2 {
		t.Fatalf("expected 2 sites (1 and 2), got: %s", sitesResp)
	}

	// 4. Site 1 has "theme" permission -> 200 OK
	call(userToken, "GET", "/api/admin/theme?site_id=1", "", 200)

	// 5. Site 2 does NOT have "theme" permission -> 403 Forbidden!
	call(userToken, "GET", "/api/admin/theme?site_id=2", "", 403)

	// 6. Site 2 DOES have "content" permission -> 200 OK
	call(userToken, "GET", "/api/admin/content?site_id=2", "", 200)

	// 7. Site 3 is completely inaccessible -> 403 Forbidden
	call(userToken, "GET", "/api/admin/content?site_id=3", "", 403)
}
