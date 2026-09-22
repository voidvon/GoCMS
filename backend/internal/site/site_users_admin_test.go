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

func TestSiteUsersAndMemberGroupsMultiSiteIsolation(t *testing.T) {
	s, database, root := newCategoryTestServer(t)

	// Create Site 2
	res, err := database.Exec(`INSERT INTO gocms_site (name, code, domain, output_dir, is_default, status) VALUES ('Sub Site', 'sub', 'sub.example.com', 'web_sub', 0, 'active')`)
	if err != nil {
		t.Fatal(err)
	}
	site2ID, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}

	// Insert members: u1 on site 1, u2 on site 2
	res1, err := database.Exec(`INSERT INTO gocms_user(site_id, username, password_hash) VALUES(1, 'site1_member', 'unused')`)
	if err != nil {
		t.Fatal(err)
	}
	u1ID, _ := res1.LastInsertId()

	res2, err := database.Exec(`INSERT INTO gocms_user(site_id, username, password_hash) VALUES(?, 'site2_member', 'unused')`, site2ID)
	if err != nil {
		t.Fatal(err)
	}
	u2ID, _ := res2.LastInsertId()

	// Insert member groups: g1 on site 1, g2 on site 2
	_, err = database.Exec(`INSERT INTO gocms_user_group(site_id, name, slug) VALUES(1, 'VIP 1', 'vip1')`)
	if err != nil {
		t.Fatal(err)
	}

	resG2, err := database.Exec(`INSERT INTO gocms_user_group(site_id, name, slug) VALUES(?, 'VIP 2', 'vip2')`, site2ID)
	if err != nil {
		t.Fatal(err)
	}
	g2ID, _ := resG2.LastInsertId()

	// 1. Root checks: site 1 should only return site1_member and VIP 1
	w1 := categoryRequest(t, s, root, "GET", "/api/admin/site-users?site_id=1", "")
	if w1.Code != 200 || !strings.Contains(w1.Body.String(), "site1_member") || strings.Contains(w1.Body.String(), "site2_member") {
		t.Fatalf("site 1 users leak: %d %s", w1.Code, w1.Body.String())
	}
	w2 := categoryRequest(t, s, root, "GET", fmt.Sprintf("/api/admin/site-users?site_id=%d", site2ID), "")
	if w2.Code != 200 || !strings.Contains(w2.Body.String(), "site2_member") || strings.Contains(w2.Body.String(), "site1_member") {
		t.Fatalf("site 2 users leak: %d %s", w2.Code, w2.Body.String())
	}

	wg1 := categoryRequest(t, s, root, "GET", "/api/admin/member-groups?site_id=1", "")
	if wg1.Code != 200 || !strings.Contains(wg1.Body.String(), "VIP 1") || strings.Contains(wg1.Body.String(), "VIP 2") {
		t.Fatalf("site 1 groups leak: %d %s", wg1.Code, wg1.Body.String())
	}
	wg2 := categoryRequest(t, s, root, "GET", fmt.Sprintf("/api/admin/member-groups?site_id=%d", site2ID), "")
	if wg2.Code != 200 || !strings.Contains(wg2.Body.String(), "VIP 2") || strings.Contains(wg2.Body.String(), "VIP 1") {
		t.Fatalf("site 2 groups leak: %d %s", wg2.Code, wg2.Body.String())
	}

	// 2. Cross-site mutation restriction for Root:
	// Trying to delete u2 while scoped to site 1 should 404
	wDelCross := categoryRequest(t, s, root, "DELETE", "/api/admin/site-users?site_id=1", fmt.Sprintf(`{"id":%d}`, u2ID))
	if wDelCross.Code != 404 {
		t.Fatalf("expected 404 deleting cross-site user, got %d", wDelCross.Code)
	}

	// Trying to delete g2 while scoped to site 1 should 404
	wDelGCross := categoryRequest(t, s, root, "DELETE", "/api/admin/member-groups?site_id=1", fmt.Sprintf(`{"id":%d}`, g2ID))
	if wDelGCross.Code != 404 {
		t.Fatalf("expected 404 deleting cross-site group, got %d", wDelGCross.Code)
	}

	// 3. Admin user with permissions only on site 1
	if _, err := database.Exec(`INSERT INTO gocms_admin_group(name, permissions, site_permissions) VALUES('Site 1 Admin', '[]', '{"1":["members"]}')`); err != nil {
		t.Fatal(err)
	}
	var grpID int64
	_ = database.QueryRow(`SELECT id FROM gocms_admin_group WHERE name='Site 1 Admin'`).Scan(&grpID)
	if _, err := database.Exec(`INSERT INTO gocms_admin_user(username, group_id) VALUES('site1_manager', ?)`, grpID); err != nil {
		t.Fatal(err)
	}
	mgrToken, err := s.createSession("site1_manager")
	if err != nil {
		t.Fatal(err)
	}

	// Allowed on site 1
	if res := categoryRequest(t, s, mgrToken, "GET", "/api/admin/site-users?site_id=1", ""); res.Code != 200 {
		t.Fatalf("expected 200 for site 1 manager, got %d", res.Code)
	}
	if res := categoryRequest(t, s, mgrToken, "GET", "/api/admin/member-groups?site_id=1", ""); res.Code != 200 {
		t.Fatalf("expected 200 for site 1 manager groups, got %d", res.Code)
	}

	// Forbidden on site 2 (cannot tamper with site_id to access site 2)
	if res := categoryRequest(t, s, mgrToken, "GET", fmt.Sprintf("/api/admin/site-users?site_id=%d", site2ID), ""); res.Code != 403 {
		t.Fatalf("expected 403 for unauthorized site 2, got %d", res.Code)
	}
	if res := categoryRequest(t, s, mgrToken, "GET", fmt.Sprintf("/api/admin/member-groups?site_id=%d", site2ID), ""); res.Code != 403 {
		t.Fatalf("expected 403 for unauthorized site 2 member groups, got %d", res.Code)
	}
	if res := categoryRequest(t, s, mgrToken, "PATCH", fmt.Sprintf("/api/admin/site-users?site_id=%d", site2ID), fmt.Sprintf(`{"id":%d,"status":"disabled"}`, u2ID)); res.Code != 403 {
		t.Fatalf("expected 403 for unauthorized site 2 patch, got %d", res.Code)
	}
	if res := categoryRequest(t, s, mgrToken, "DELETE", fmt.Sprintf("/api/admin/site-users?site_id=%d", site2ID), fmt.Sprintf(`{"id":%d}`, u2ID)); res.Code != 403 {
		t.Fatalf("expected 403 for unauthorized site 2 delete, got %d", res.Code)
	}

	// Admin with no "members" permission at all
	if _, err := database.Exec(`INSERT INTO gocms_admin_group(name, permissions, site_permissions) VALUES('No Member Perm', '[]', '{"1":["content"]}')`); err != nil {
		t.Fatal(err)
	}
	var noPermGrpID int64
	_ = database.QueryRow(`SELECT id FROM gocms_admin_group WHERE name='No Member Perm'`).Scan(&noPermGrpID)
	if _, err := database.Exec(`INSERT INTO gocms_admin_user(username, group_id) VALUES('no_member_user', ?)`, noPermGrpID); err != nil {
		t.Fatal(err)
	}
	noMemberToken, err := s.createSession("no_member_user")
	if err != nil {
		t.Fatal(err)
	}
	if res := categoryRequest(t, s, noMemberToken, "GET", "/api/admin/site-users?site_id=1", ""); res.Code != 403 {
		t.Fatalf("expected 403 when missing members permission, got %d", res.Code)
	}
	if res := categoryRequest(t, s, noMemberToken, "GET", "/api/admin/member-groups?site_id=1", ""); res.Code != 403 {
		t.Fatalf("expected 403 when missing members permission, got %d", res.Code)
	}

	// Clean up verification: delete u1 on site 1
	if res := categoryRequest(t, s, mgrToken, "DELETE", "/api/admin/site-users?site_id=1", fmt.Sprintf(`{"id":%d}`, u1ID)); res.Code != 200 {
		t.Fatalf("expected 200 deleting u1 on site 1, got %d", res.Code)
	}
	// u2 is still intact on site 2
	var u2Exists int
	_ = database.QueryRow(`SELECT COUNT(*) FROM gocms_user WHERE id=?`, u2ID).Scan(&u2Exists)
	if u2Exists != 1 {
		t.Fatal("u2 should still exist on site 2")
	}
}
