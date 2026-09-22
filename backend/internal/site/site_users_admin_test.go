package site

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gocms/internal/apikey"
	"gocms/internal/auth"
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

func TestMultiSiteSecurityAndIsolation(t *testing.T) {
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

	// 1. Test adminStats site scoping
	// Insert contents into site 1 and site 2
	_, _ = database.Exec(`INSERT INTO gocms_content(site_id, category_id, title, visible) VALUES(1, 0, 'S1 Content 1', 1)`)
	_, _ = database.Exec(`INSERT INTO gocms_content(site_id, category_id, title, visible) VALUES(1, 0, 'S1 Content 2', 0)`)
	_, _ = database.Exec(`INSERT INTO gocms_content(site_id, category_id, title, visible) VALUES(?, 0, 'S2 Content 1', 1)`, site2ID)

	// Insert messages into site 1 and site 2
	_, _ = database.Exec(`INSERT INTO gocms_message(site_id, title, content, state) VALUES(1, 'M1', 'msg', 0)`)
	_, _ = database.Exec(`INSERT INTO gocms_message(site_id, title, content, state) VALUES(?, 'M2', 'msg', 0)`, site2ID)
	_, _ = database.Exec(`INSERT INTO gocms_message(site_id, title, content, state) VALUES(?, 'M3', 'msg', 1)`, site2ID)

	var stats1 AdminStats
	wStats1 := categoryRequest(t, s, root, "GET", "/api/admin/stats?site_id=1", "")
	if wStats1.Code != 200 {
		t.Fatalf("stats site 1 status = %d: %s", wStats1.Code, wStats1.Body.String())
	}
	if err := json.Unmarshal(wStats1.Body.Bytes(), &stats1); err != nil {
		t.Fatal(err)
	}
	if stats1.Contents != 2 || stats1.VisibleContents != 1 || stats1.Messages != 1 || stats1.PendingMessages != 1 {
		t.Fatalf("unexpected stats for site 1: %+v", stats1)
	}

	var stats2 AdminStats
	wStats2 := categoryRequest(t, s, root, "GET", fmt.Sprintf("/api/admin/stats?site_id=%d", site2ID), "")
	if wStats2.Code != 200 {
		t.Fatalf("stats site 2 status = %d: %s", wStats2.Code, wStats2.Body.String())
	}
	if err := json.Unmarshal(wStats2.Body.Bytes(), &stats2); err != nil {
		t.Fatal(err)
	}
	if stats2.Contents != 1 || stats2.VisibleContents != 1 || stats2.Messages != 2 || stats2.PendingMessages != 1 {
		t.Fatalf("unexpected stats for site 2: %+v", stats2)
	}

	// 2. Test cross-site category parenting prevention
	resCat1, err := database.Exec(`INSERT INTO gocms_category(site_id, name, parent_id, order_id, list_page_size, page_type) VALUES(1, 'S1 Cat', 0, 1, 20, 'list')`)
	if err != nil {
		t.Fatal(err)
	}
	cat1ID, _ := resCat1.LastInsertId()

	wCatFail := categoryRequest(t, s, root, "POST", fmt.Sprintf("/api/admin/categories?site_id=%d", site2ID), fmt.Sprintf(`{"name":"S2 Child Cat","parent_id":%d,"order_id":1,"page_type":"list"}`, cat1ID))
	if wCatFail.Code != 400 || !strings.Contains(wCatFail.Body.String(), "父分类属于其他站点") {
		t.Fatalf("expected 400 rejection for cross-site category parent, got %d: %s", wCatFail.Code, wCatFail.Body.String())
	}

	// 3. Test cross-site content category prevention
	wContentFail := categoryRequest(t, s, root, "POST", fmt.Sprintf("/api/admin/content?site_id=%d", site2ID), fmt.Sprintf(`{"title":"Test Content","category_id":%d}`, cat1ID))
	if wContentFail.Code != 400 || !strings.Contains(wContentFail.Body.String(), "属于其他站点") {
		t.Fatalf("expected 400 rejection for cross-site content category, got %d: %s", wContentFail.Code, wContentFail.Body.String())
	}

	// 4. Test cross-site media deletion prevention
	resMedia1, err := database.Exec(`INSERT INTO gocms_media(site_id, kind, public_path, original_name, mime_type, storage_path, status) VALUES(1, 'image', '/assets/1/uploads/test.png', 'test.png', 'image/png', '1/uploads/test.png', 'active')`)
	if err != nil {
		t.Fatal(err)
	}
	media1ID, _ := resMedia1.LastInsertId()

	// Admin only on site 2
	_, _ = database.Exec(`INSERT INTO gocms_admin_group(name, permissions, site_permissions) VALUES('Site 2 Media Admin', '["media"]', '{"2":["media"]}')`)
	var grp2ID int64
	_ = database.QueryRow(`SELECT id FROM gocms_admin_group WHERE name='Site 2 Media Admin'`).Scan(&grp2ID)
	_, _ = database.Exec(`INSERT INTO gocms_admin_user(username, group_id) VALUES('site2_media_admin', ?)`, grp2ID)
	s2AdminToken, err := s.createSession("site2_media_admin")
	if err != nil {
		t.Fatal(err)
	}

	wDelMedia := categoryRequest(t, s, s2AdminToken, "DELETE", fmt.Sprintf("/api/admin/media/%d", media1ID), "")
	if wDelMedia.Code != 403 {
		t.Fatalf("expected 403 when deleting other site media, got %d: %s", wDelMedia.Code, wDelMedia.Body.String())
	}

	// 5. Test member login cross-site isolation
	hash, _ := auth.HashPassword("password123")
	_, err = database.Exec(`INSERT INTO gocms_user(site_id, username, password_hash, status) VALUES(1, 'member_site1', ?, 'active')`, hash)
	if err != nil {
		t.Fatal(err)
	}

	// Try login on site 2 with credentials of site 1 member -> should fail 401
	loginReqSite2 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/auth/login?site_id=%d", site2ID), strings.NewReader(`{"username":"member_site1","password":"password123"}`))
	loginReqSite2.Header.Set("Content-Type", "application/json")
	wLoginS2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(wLoginS2, loginReqSite2)
	if wLoginS2.Code != 401 {
		t.Fatalf("expected 401 when site 1 member logs into site 2, got %d: %s", wLoginS2.Code, wLoginS2.Body.String())
	}

	// Try login on site 1 -> should succeed 200
	loginReqSite1 := httptest.NewRequest(http.MethodPost, "/api/auth/login?site_id=1", strings.NewReader(`{"username":"member_site1","password":"password123"}`))
	loginReqSite1.Header.Set("Content-Type", "application/json")
	wLoginS1 := httptest.NewRecorder()
	s.Handler().ServeHTTP(wLoginS1, loginReqSite1)
	if wLoginS1.Code != 200 {
		t.Fatalf("expected 200 when site 1 member logs into site 1, got %d: %s", wLoginS1.Code, wLoginS1.Body.String())
	}
}

