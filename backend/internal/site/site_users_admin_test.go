package site

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocms/internal/apikey"
	"gocms/internal/auth"
	"gocms/internal/db"
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

func TestMultiSiteRound2AuditFixes(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.CreateSchema(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	assetsDir := filepath.Join(root, "assets")
	_ = os.MkdirAll(assetsDir, 0755)

	s, err := New(database, root)
	if err != nil {
		t.Fatal(err)
	}
	s.assetsRoot = assetsDir
	dataDir := filepath.Join(root, "data")
	_ = os.MkdirAll(dataDir, 0755)
	s.ConfigurePublishing("", dataDir, "", assetsDir, "")

	// 1. Create Site 2 with domain "sub.example.com"
	resSite2, err := db.CreateSite(context.Background(), database, &db.Site{
		Name:      "分站二",
		Code:      "sub2",
		Domain:    "sub.example.com",
		Status:    "active",
		OutputDir: "sub2",
	})
	if err != nil {
		t.Fatalf("create site 2 failed: %v", err)
	}
	site2ID := resSite2.ID

	// Insert content into Site 1
	resC1, err := database.Exec(`INSERT INTO gocms_content(site_id, category_id, title, visible) VALUES(1, 0, 'Site 1 Article', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	c1ID, _ := resC1.LastInsertId()

	// Insert content into Site 2
	resC2, err := database.Exec(`INSERT INTO gocms_content(site_id, category_id, title, visible) VALUES(?, 0, 'Site 2 Article', 1)`, site2ID)
	if err != nil {
		t.Fatal(err)
	}
	c2ID, _ := resC2.LastInsertId()

	// Test 1: Public content item isolation
	// Requesting Site 1 content through Site 2's host -> should return 404
	reqC1FromS2 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/content/%d", c1ID), nil)
	reqC1FromS2.Host = "sub.example.com"
	wC1S2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(wC1S2, reqC1FromS2)
	if wC1S2.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when accessing site 1 content from site 2 domain, got %d: %s", wC1S2.Code, wC1S2.Body.String())
	}

	// Requesting Site 2 content through Site 2's host -> should return 200
	reqC2FromS2 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/content/%d", c2ID), nil)
	reqC2FromS2.Host = "sub.example.com"
	wC2S2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(wC2S2, reqC2FromS2)
	if wC2S2.Code != http.StatusOK {
		t.Fatalf("expected 200 when accessing site 2 content from site 2 domain, got %d: %s", wC2S2.Code, wC2S2.Body.String())
	}

	// Test 2: Admin content item GET IDOR
	// Admin with permission ONLY for Site 2
	_, _ = database.Exec(`INSERT INTO gocms_admin_group(name, permissions, site_permissions) VALUES('Site 2 Content Admin', '["content"]', ?)`, fmt.Sprintf(`{"%d":["content"]}`, site2ID))
	var grp2ID int64
	_ = database.QueryRow(`SELECT id FROM gocms_admin_group WHERE name='Site 2 Content Admin'`).Scan(&grp2ID)
	_, _ = database.Exec(`INSERT INTO gocms_admin_user(username, group_id) VALUES('site2_content_admin', ?)`, grp2ID)
	s2AdminToken, err := s.createSession("site2_content_admin")
	if err != nil {
		t.Fatal(err)
	}

	// Site 2 admin requests Site 1 content -> should return 404 (concealing out-of-scope content)
	wAdminC1 := categoryRequest(t, s, s2AdminToken, "GET", fmt.Sprintf("/api/admin/content/%d", c1ID), "")
	if wAdminC1.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when Site 2 admin reads Site 1 content, got %d: %s", wAdminC1.Code, wAdminC1.Body.String())
	}

	// Site 2 admin requests Site 2 content -> should return 200
	wAdminC2 := categoryRequest(t, s, s2AdminToken, "GET", fmt.Sprintf("/api/admin/content/%d", c2ID), "")
	if wAdminC2.Code != http.StatusOK {
		t.Fatalf("expected 200 when Site 2 admin reads Site 2 content, got %d: %s", wAdminC2.Code, wAdminC2.Body.String())
	}

	// Test 3: Shared cross-site members in site member list and member groups
	// User registered on Site 1
	hash, _ := auth.HashPassword("password123")
	resUser, err := database.Exec(`INSERT INTO gocms_user(site_id, username, password_hash, display_name, status) VALUES(1, 'shared_user', ?, 'Shared User', 'active')`, hash)
	if err != nil {
		t.Fatal(err)
	}
	sharedUserID, _ := resUser.LastInsertId()

	// Add user to Site 2 via gocms_site_member
	_, err = database.Exec(`INSERT INTO gocms_site_member(site_id, user_id, status) VALUES(?, ?, 'active')`, site2ID, sharedUserID)
	if err != nil {
		t.Fatal(err)
	}

	// Superadmin session
	seedTestAdmin(t, s)
	superToken, _ := s.createSession("gocms")

	// List site users for Site 2 -> shared user must appear!
	wListUsers := categoryRequest(t, s, superToken, "GET", fmt.Sprintf("/api/admin/site-users?site_id=%d", site2ID), "")
	if wListUsers.Code != 200 || !strings.Contains(wListUsers.Body.String(), "shared_user") {
		t.Fatalf("shared_user should appear in site 2 user list: %s", wListUsers.Body.String())
	}

	// Create a user group for Site 2
	resGrp, err := database.Exec(`INSERT INTO gocms_user_group(site_id, name, slug) VALUES(?, 'VIP Group', 'vip')`, site2ID)
	if err != nil {
		t.Fatal(err)
	}
	s2UserGroupID, _ := resGrp.LastInsertId()

	// Assign shared_user to Site 2's group
	wAssignGrp := categoryRequest(t, s, superToken, "POST", fmt.Sprintf("/api/admin/member-groups?site_id=%d", site2ID), fmt.Sprintf(`{"user_id":%d,"group_id":%d}`, sharedUserID, s2UserGroupID))
	if wAssignGrp.Code != 200 {
		t.Fatalf("failed to assign shared user to site 2 group: %d %s", wAssignGrp.Code, wAssignGrp.Body.String())
	}

	// List group members -> shared user must appear!
	wListGrpMembers := categoryRequest(t, s, superToken, "GET", fmt.Sprintf("/api/admin/member-groups?site_id=%d&group_id=%d", site2ID, s2UserGroupID), "")
	if wListGrpMembers.Code != 200 || !strings.Contains(wListGrpMembers.Body.String(), "shared_user") {
		t.Fatalf("shared user should appear in group members list: %s", wListGrpMembers.Body.String())
	}

	// Verify shared user deletion from Site 1 by non-superadmin only detaches from Site 1
	if _, err := database.Exec(`
		INSERT INTO gocms_admin_group (id, name, permissions, site_ids) VALUES (201, 'Site 1 Admin', '["members"]', '[1]');
		INSERT INTO gocms_admin_user (username, group_id, site_ids) VALUES ('s1_admin', 201, '[1]');
	`); err != nil {
		t.Fatal(err)
	}
	s1AdminToken, _ := s.createSession("s1_admin")
	wDelShared := categoryRequest(t, s, s1AdminToken, "DELETE", "/api/admin/site-users?site_id=1", fmt.Sprintf(`{"id":%d}`, sharedUserID))
	if wDelShared.Code != 200 {
		t.Fatalf("failed to detach shared user from site 1: %d %s", wDelShared.Code, wDelShared.Body.String())
	}
	// Check that shared user still exists in gocms_user and has site_id reassigned to Site 2
	var sUserSiteID int64
	if err := database.QueryRow(`SELECT site_id FROM gocms_user WHERE id = ?`, sharedUserID).Scan(&sUserSiteID); err != nil {
		t.Fatalf("shared user was erroneously deleted from gocms_user: %v", err)
	}
	if sUserSiteID != site2ID {
		t.Fatalf("expected origin site reassigned to %d, got %d", site2ID, sUserSiteID)
	}
	// Check that shared user is still active in gocms_site_member on Site 2
	var s2MemberCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM gocms_site_member WHERE user_id = ? AND site_id = ?`, sharedUserID, site2ID).Scan(&s2MemberCount); err != nil || s2MemberCount != 1 {
		t.Fatalf("shared user missing on site 2 after site 1 detachment")
	}

	// Test 4: DeleteSite Cascade Cleanup
	// Create Site 3 with all types of child records
	resSite3, err := db.CreateSite(context.Background(), database, &db.Site{
		Name:      "待删站点三",
		Code:      "site3",
		Status:    "active",
		OutputDir: "site3",
	})
	if err != nil {
		t.Fatal(err)
	}
	site3ID := resSite3.ID

	// Insert media and media ref for Site 3
	_ = db.EnsureMedia(context.Background(), database)
	resM3, err := database.Exec(`INSERT INTO gocms_media(site_id, kind, storage_path, public_path, status) VALUES(?, 'image', '3/img.png', '/assets/3/img.png', 'active')`, site3ID)
	if err != nil {
		t.Fatalf("insert media 3 failed: %v", err)
	}
	m3ID, _ := resM3.LastInsertId()
	resC3, _ := database.Exec(`INSERT INTO gocms_content(site_id, title) VALUES(?, 'Site 3 Content')`, site3ID)
	c3ID, _ := resC3.LastInsertId()
	_, _ = database.Exec(`INSERT INTO gocms_media_ref(media_id, content_id, field_name) VALUES(?, ?, 'cover')`, m3ID, c3ID)
	_, _ = database.Exec(`INSERT INTO gocms_content_translation(content_id, lang, title) VALUES(?, 'en', 'Site 3 En')`, c3ID)
	resU3, _ := database.Exec(`INSERT INTO gocms_user(site_id, username, password_hash) VALUES(?, 'user_site3', 'hash')`, site3ID)
	u3ID, _ := resU3.LastInsertId()
	_, _ = database.Exec(`INSERT INTO gocms_user_session(token_hash, user_id, expires_at) VALUES('token3', ?, 9999999999)`, u3ID)


	// Delete Site 3
	wDelSite := categoryRequest(t, s, superToken, "DELETE", fmt.Sprintf("/api/admin/sites/%d", site3ID), "")
	if wDelSite.Code != 200 {
		t.Fatalf("delete site 3 failed: %d %s", wDelSite.Code, wDelSite.Body.String())
	}

	// Verify all child tables were cleaned up
	var count int
	_ = database.QueryRow(`SELECT COUNT(*) FROM gocms_media WHERE site_id = ?`, site3ID).Scan(&count)
	if count != 0 {
		t.Fatalf("expected 0 media records after site deletion, got %d", count)
	}
	_ = database.QueryRow(`SELECT COUNT(*) FROM gocms_media_ref WHERE content_id = ?`, c3ID).Scan(&count)
	if count != 0 {
		t.Fatalf("expected 0 media_ref records after site deletion, got %d", count)
	}
	_ = database.QueryRow(`SELECT COUNT(*) FROM gocms_content_translation WHERE content_id = ?`, c3ID).Scan(&count)
	if count != 0 {
		t.Fatalf("expected 0 content_translation records after site deletion, got %d", count)
	}
	_ = database.QueryRow(`SELECT COUNT(*) FROM gocms_user_session WHERE user_id = ?`, u3ID).Scan(&count)
	if count != 0 {
		t.Fatalf("expected 0 user_session records after site deletion, got %d", count)
	}
	_ = database.QueryRow(`SELECT COUNT(*) FROM gocms_user WHERE id = ?`, u3ID).Scan(&count)
	if count != 0 {
		t.Fatalf("expected 0 user records after site deletion, got %d", count)
	}

	// Test 5: Theme Copy-on-Write isolation
	// Site 1 has a theme "mytheme"
	s1ThemeDir := filepath.Join(assetsDir, "1", "themes", "mytheme")
	_ = os.MkdirAll(filepath.Join(s1ThemeDir, "templates"), 0755)
	_ = os.MkdirAll(filepath.Join(s1ThemeDir, "assets", "css"), 0755)
	_ = os.WriteFile(filepath.Join(s1ThemeDir, "theme.json"), []byte(`{"id":"mytheme","name":"My Theme"}`), 0644)
	s1Index := filepath.Join(s1ThemeDir, "templates", "index.html")
	_ = os.WriteFile(s1Index, []byte("<h1>Site 1 Theme Original</h1>"), 0644)

	// Set Site 2 theme to "mytheme"
	_, _ = database.Exec(`UPDATE gocms_site SET theme_id = 'mytheme' WHERE id = ?`, site2ID)

	// Site 2 admin updates index.html
	wSaveTheme := categoryRequest(t, s, superToken, "PUT", fmt.Sprintf("/api/admin/theme/files?site_id=%d", site2ID), `{"kind":"template","path":"index.html","content":"<h1>Site 2 Customized Theme</h1>"}`)
	if wSaveTheme.Code != 200 {
		t.Fatalf("save theme file for site 2 failed: %d %s", wSaveTheme.Code, wSaveTheme.Body.String())
	}

	// Verify Site 1 theme file is untouched
	bS1, _ := os.ReadFile(s1Index)
	if string(bS1) != "<h1>Site 1 Theme Original</h1>" {
		t.Fatalf("Site 1 theme was modified by Site 2 COW! Content: %s", string(bS1))
	}

	// Verify Site 2 has its own isolated theme file
	s2Index := filepath.Join(assetsDir, fmt.Sprint(site2ID), "themes", "mytheme", "templates", "index.html")
	bS2, err := os.ReadFile(s2Index)
	if err != nil || string(bS2) != "<h1>Site 2 Customized Theme</h1>" {
		t.Fatalf("Site 2 theme file was not created via COW: %v, content: %s", err, string(bS2))
	}

	// Test 6: Publication Cache Invalidation on Site Update
	pubS2 := s.publicationForSite(context.Background(), site2ID)
	if pubS2 == nil || pubS2.publisher.OutputDir != "sub2" {
		t.Fatalf("expected initial pub output dir sub2, got: %+v", pubS2)
	}

	// Update Site 2 OutputDir to "new-sub2-dir" via admin API
	wUpdateSite := categoryRequest(t, s, superToken, "PUT", fmt.Sprintf("/api/admin/sites/%d", site2ID), `{"name":"分站二","code":"sub2","output_dir":"new-sub2-dir","domain":"sub.example.com","status":"active"}`)
	if wUpdateSite.Code != 200 {
		t.Fatalf("update site 2 failed: %d %s", wUpdateSite.Code, wUpdateSite.Body.String())
	}

	// Verify publication was invalidated and newly fetched publication reflects "new-sub2-dir"
	pubS2New := s.publicationForSite(context.Background(), site2ID)
	if pubS2New == nil || pubS2New.publisher.OutputDir != "new-sub2-dir" {
		t.Fatalf("expected updated publication output dir 'new-sub2-dir', got: %s", pubS2New.publisher.OutputDir)
	}
}

func TestMultiSiteRound3AuditFixes(t *testing.T) {
	s, database, token := newCategoryTestServer(t)
	root := t.TempDir()
	assetsDir := filepath.Join(root, "assets")
	dataDir := filepath.Join(root, "data")
	_ = os.MkdirAll(dataDir, 0755)
	_ = os.MkdirAll(assetsDir, 0755)
	s.ConfigurePublishing("", dataDir, "", assetsDir, "")

	// Create Site 2
	site2, err := db.CreateSite(context.Background(), database, &db.Site{
		Name:      "分站二",
		Code:      "sub2",
		Domain:    "*.example.org",
		Aliases:   []string{"*.custom.net"},
		Status:    "active",
		OutputDir: "sub2",
	})
	if err != nil {
		t.Fatal(err)
	}
	site2ID := site2.ID

	// 1. Wildcard Host and X-Forwarded-Host Matching
	sMatch, err := db.GetSiteByHost(context.Background(), database, "blog.example.org")
	if err != nil || sMatch == nil || sMatch.ID != site2ID {
		t.Fatalf("expected blog.example.org to match site 2, got: %v, err: %v", sMatch, err)
	}
	sMatch2, err := db.GetSiteByHost(context.Background(), database, "shop.custom.net")
	if err != nil || sMatch2 == nil || sMatch2.ID != site2ID {
		t.Fatalf("expected shop.custom.net to match site 2, got: %v, err: %v", sMatch2, err)
	}
	_, errNonMatch := db.GetSiteByHost(context.Background(), database, "example.org")
	if errNonMatch == nil {
		t.Fatalf("exact root example.org should not match *.example.org without domain alias")
	}

	reqForwarded := httptest.NewRequest("GET", "/api/admin/site-users", nil)
	reqForwarded.Header.Set("X-Forwarded-Host", "blog.example.org, 10.0.0.1")
	resolvedSiteID, err := s.resolveSiteID(reqForwarded, nil)
	if err != nil || resolvedSiteID != site2ID {
		t.Fatalf("expected X-Forwarded-Host to resolve to site2ID (%d), got: %d, err: %v", site2ID, resolvedSiteID, err)
	}

	// 2. Member Status & Deletion Isolation
	pwdHash, _ := auth.HashPassword("pass123")
	resUser, err := database.Exec(`INSERT INTO gocms_user(site_id, username, password_hash, display_name, status) VALUES(1, 'origin_user', ?, 'Origin User', 'active')`, pwdHash)
	if err != nil {
		t.Fatal(err)
	}
	userID, _ := resUser.LastInsertId()

	_, err = database.Exec(`INSERT INTO gocms_site_member(site_id, user_id, status) VALUES(?, ?, 'active')`, site2ID, userID)
	if err != nil {
		t.Fatal(err)
	}

	// Disable user in Site 2
	wPatchS2 := categoryRequest(t, s, token, "PATCH", fmt.Sprintf("/api/admin/site-users?site_id=%d", site2ID), fmt.Sprintf(`{"id":%d,"status":"disabled"}`, userID))
	if wPatchS2.Code != 200 {
		t.Fatalf("patch user in site 2 failed: %d %s", wPatchS2.Code, wPatchS2.Body.String())
	}

	// Verify Site 2 user list shows status="disabled"
	wListS2 := categoryRequest(t, s, token, "GET", fmt.Sprintf("/api/admin/site-users?site_id=%d", site2ID), "")
	if !strings.Contains(wListS2.Body.String(), `"status":"disabled"`) {
		t.Fatalf("expected site 2 user status disabled, got: %s", wListS2.Body.String())
	}

	// Verify Site 1 user list still shows status="active"
	wListS1 := categoryRequest(t, s, token, "GET", "/api/admin/site-users?site_id=1", "")
	if !strings.Contains(wListS1.Body.String(), `"status":"active"`) {
		t.Fatalf("expected site 1 user status active, got: %s", wListS1.Body.String())
	}

	// Verify gocms_user.status in DB is still active!
	var globalStatus string
	_ = database.QueryRow(`SELECT status FROM gocms_user WHERE id = ?`, userID).Scan(&globalStatus)
	if globalStatus != "active" {
		t.Fatalf("expected gocms_user global status to remain active, got: %s", globalStatus)
	}

	// Subsite DELETE should only detach user from Site 2
	wDelS2 := categoryRequest(t, s, token, "DELETE", fmt.Sprintf("/api/admin/site-users?site_id=%d", site2ID), fmt.Sprintf(`{"id":%d}`, userID))
	if wDelS2.Code != 200 {
		t.Fatalf("delete user from site 2 failed: %d %s", wDelS2.Code, wDelS2.Body.String())
	}

	// User should no longer be in Site 2
	wListS2After := categoryRequest(t, s, token, "GET", fmt.Sprintf("/api/admin/site-users?site_id=%d", site2ID), "")
	if strings.Contains(wListS2After.Body.String(), "origin_user") {
		t.Fatalf("user should not appear in site 2 after detach")
	}

	// User must still exist in Site 1 and gocms_user
	var userStillExists int
	_ = database.QueryRow(`SELECT COUNT(*) FROM gocms_user WHERE id = ?`, userID).Scan(&userStillExists)
	if userStillExists != 1 {
		t.Fatalf("user should still exist in gocms_user after detach from subsite")
	}

	// Origin site DELETE should completely delete user
	wDelS1 := categoryRequest(t, s, token, "DELETE", "/api/admin/site-users?site_id=1", fmt.Sprintf(`{"id":%d}`, userID))
	if wDelS1.Code != 200 {
		t.Fatalf("delete user from origin site failed: %d %s", wDelS1.Code, wDelS1.Body.String())
	}
	_ = database.QueryRow(`SELECT COUNT(*) FROM gocms_user WHERE id = ?`, userID).Scan(&userStillExists)
	if userStillExists != 0 {
		t.Fatalf("user should be completely deleted after deletion from origin site")
	}

	// 3. Feedback Class Item Count Site Scope
	_ = db.EnsureMessages(context.Background(), database)
	var classID int64
	_ = database.QueryRow(`SELECT id FROM "`+db.FeedbackClassTable+`" LIMIT 1`).Scan(&classID)
	if classID > 0 {
		_, _ = database.Exec(`INSERT INTO "gocms_message"(site_id, class_id, title, content) VALUES(1, ?, 'S1 msg', 'test')`, classID)
		_, _ = database.Exec(`INSERT INTO "gocms_message"(site_id, class_id, title, content) VALUES(?, ?, 'S2 msg 1', 'test')`, site2ID, classID)
		_, _ = database.Exec(`INSERT INTO "gocms_message"(site_id, class_id, title, content) VALUES(?, ?, 'S2 msg 2', 'test')`, site2ID, classID)

		wFC1 := categoryRequest(t, s, token, "GET", "/api/admin/feedback-classes?site_id=1", "")
		if !strings.Contains(wFC1.Body.String(), `"item_count":1`) {
			t.Fatalf("expected site 1 feedback class item_count=1, got: %s", wFC1.Body.String())
		}
		wFC2 := categoryRequest(t, s, token, "GET", fmt.Sprintf("/api/admin/feedback-classes?site_id=%d", site2ID), "")
		if !strings.Contains(wFC2.Body.String(), `"item_count":2`) {
			t.Fatalf("expected site 2 feedback class item_count=2, got: %s", wFC2.Body.String())
		}
	}

	// 4. Site Settings Publication Cache Invalidation
	pubS2Init := s.publicationForSite(context.Background(), site2ID)
	if pubS2Init == nil {
		t.Fatalf("expected publicationForSite to return site 2 pub")
	}
	wSettings := categoryRequest(t, s, token, "POST", fmt.Sprintf("/api/admin/site-settings?site_id=%d", site2ID), `{"site_name":"New Site 2 Title"}`)
	if wSettings.Code != 200 {
		t.Fatalf("save site settings failed: %d %s", wSettings.Code, wSettings.Body.String())
	}
	s.sitePubMu.Lock()
	_, stillCached := s.sitePublications[site2ID]
	s.sitePubMu.Unlock()
	if stillCached {
		t.Fatalf("publication cache for site 2 should be invalidated after saving site settings")
	}

	// 5. Global Template Assignment Site Isolation
	s2ThemeDir := filepath.Join(assetsDir, fmt.Sprint(site2ID), "themes", "site2_theme")
	_ = os.MkdirAll(filepath.Join(s2ThemeDir, "templates"), 0755)
	_ = os.WriteFile(filepath.Join(s2ThemeDir, "theme.json"), []byte(`{"id":"site2_theme","name":"Site 2 Theme"}`), 0644)
	_ = os.WriteFile(filepath.Join(s2ThemeDir, "templates", "search_site2.html"), []byte(`<h1>Site 2 Search</h1>`), 0644)
	_ = os.WriteFile(filepath.Join(s2ThemeDir, "templates", "list_site2.html"), []byte(`<h1>Site 2 List</h1>`), 0644)
	_ = os.WriteFile(filepath.Join(s2ThemeDir, "templates", "detail_site2.html"), []byte(`<h1>Site 2 Detail</h1>`), 0644)
	_, _ = database.Exec(`UPDATE gocms_site SET theme_id = 'site2_theme' WHERE id = ?`, site2ID)

	// Attempt assignment with nonexistent template -> should fail
	wAssignFail := categoryRequest(t, s, token, "PUT", fmt.Sprintf("/api/admin/theme/assignments/search?site_id=%d", site2ID), `{"template_path":"nonexistent.html"}`)
	if wAssignFail.Code != 400 {
		t.Fatalf("expected 400 for nonexistent template in theme assignment, got %d: %s", wAssignFail.Code, wAssignFail.Body.String())
	}

	// Assignment with valid template in Site 2's theme -> should succeed
	wAssignOK := categoryRequest(t, s, token, "PUT", fmt.Sprintf("/api/admin/theme/assignments/search?site_id=%d", site2ID), `{"template_path":"search_site2.html"}`)
	if wAssignOK.Code != 200 {
		t.Fatalf("expected 200 for valid site 2 template assignment, got %d: %s", wAssignOK.Code, wAssignOK.Body.String())
	}

	// 6. Category Route Validation with Target Site Theme
	s1ThemeDir := filepath.Join(assetsDir, "1", "themes", "site1_theme")
	_ = os.MkdirAll(filepath.Join(s1ThemeDir, "templates"), 0755)
	_ = os.WriteFile(filepath.Join(s1ThemeDir, "theme.json"), []byte(`{"id":"site1_theme","name":"Site 1 Theme"}`), 0644)
	_ = os.WriteFile(filepath.Join(s1ThemeDir, "templates", "list_site1.html"), []byte(`<h1>Site 1 List</h1>`), 0644)
	_ = os.WriteFile(filepath.Join(s1ThemeDir, "templates", "detail_site1.html"), []byte(`<h1>Site 1 Detail</h1>`), 0644)
	_, _ = database.Exec(`UPDATE gocms_site SET theme_id = 'site1_theme' WHERE id = 1`)

	// Site 2 category with list_site2.html and detail_site2.html -> should succeed
	wCatS2 := categoryRequest(t, s, token, "POST", fmt.Sprintf("/api/admin/categories?site_id=%d", site2ID), `{"name":"S2 Cat","list_template":"list_site2.html","detail_template":"detail_site2.html"}`)
	if wCatS2.Code != 200 {
		t.Fatalf("expected 200 creating category on site 2 with site 2 template, got %d: %s", wCatS2.Code, wCatS2.Body.String())
	}

	// Site 1 category with list_site2.html -> should fail with 400 because Site 1 theme does not have list_site2.html
	wCatS1 := categoryRequest(t, s, token, "POST", "/api/admin/categories?site_id=1", `{"name":"S1 Cat","list_template":"list_site2.html","detail_template":"detail_site2.html"}`)
	if wCatS1.Code != 400 {
		t.Fatalf("expected 400 creating category on site 1 with site 2 template, got %d: %s", wCatS1.Code, wCatS1.Body.String())
	}

	// Site 1 category with list_site1.html and detail_site1.html -> should succeed
	wCatS1Valid := categoryRequest(t, s, token, "POST", "/api/admin/categories?site_id=1", `{"name":"S1 Cat","list_template":"list_site1.html","detail_template":"detail_site1.html"}`)
	if wCatS1Valid.Code != 200 {
		t.Fatalf("expected 200 creating category on site 1 with site 1 template, got %d: %s", wCatS1Valid.Code, wCatS1Valid.Body.String())
	}

	// 7. Site-Specific content.review Permission & Subsite site-settings authorization
	if _, err := database.Exec(`
		INSERT INTO gocms_admin_group (id, name, permissions, site_ids, site_permissions) VALUES (301, 'S2 Editor Group', '["content", "content.add", "content.edit"]', '[2]', '{"2":["content","content.add","content.edit","content.review"]}');
		INSERT INTO gocms_admin_user (username, group_id, site_ids) VALUES ('s2_reviewer', 301, '[2]');
	`); err != nil {
		t.Fatal(err)
	}
	s2ReviewerToken, _ := s.createSession("s2_reviewer")

	// Verify s2_reviewer can edit site-settings for site 2
	wS2Settings := categoryRequest(t, s, s2ReviewerToken, "POST", fmt.Sprintf("/api/admin/site-settings?site_id=%d", site2ID), `{"site_name":"S2 Custom Title"}`)
	if wS2Settings.Code != 200 {
		t.Fatalf("subsite admin cannot edit site-settings: %d %s", wS2Settings.Code, wS2Settings.Body.String())
	}

	// Verify s2_reviewer cannot edit site-settings for site 1 (403)
	wS1SettingsForbidden := categoryRequest(t, s, s2ReviewerToken, "POST", "/api/admin/site-settings?site_id=1", `{"site_name":"Hacked"}`)
	if wS1SettingsForbidden.Code != 403 {
		t.Fatalf("subsite admin should not be able to edit site 1 settings, got %d", wS1SettingsForbidden.Code)
	}

	// Create visible content on Site 2 using reviewer token
	var s2CatID int64
	_ = database.QueryRow(`SELECT id FROM gocms_category WHERE site_id = ? LIMIT 1`, site2ID).Scan(&s2CatID)
	wCreateContent := categoryRequest(t, s, s2ReviewerToken, "POST", fmt.Sprintf("/api/admin/content?site_id=%d", site2ID), fmt.Sprintf(`{"category":%d,"title":"Reviewed Content","visible":1}`, s2CatID))
	if wCreateContent.Code != 200 && wCreateContent.Code != 201 {
		t.Fatalf("expected s2_reviewer to be able to create and review visible content on site 2, got %d %s", wCreateContent.Code, wCreateContent.Body.String())
	}
}

func TestDisabledSiteAndSubsiteIsolation(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.CreateSchema(context.Background(), database); err != nil {
		t.Fatal(err)
	}

	siteRoot := t.TempDir()
	s, err := New(database, siteRoot)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Prepare files in siteRoot
	if err := os.WriteFile(filepath.Join(siteRoot, "index.html"), []byte("Site 1 Index"), 0644); err != nil {
		t.Fatal(err)
	}
	subDir := filepath.Join(siteRoot, "sub2")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "index.html"), []byte("Site 2 Index"), 0644); err != nil {
		t.Fatal(err)
	}

	// 2. Create subsite in DB
	site2, err := db.CreateSite(context.Background(), database, &db.Site{
		Name:      "分站二",
		Code:      "sub2",
		Domain:    "sub2.test.local",
		OutputDir: "sub2",
		Status:    "active",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 3. Test active subsite static serving
	reqS2 := httptest.NewRequest(http.MethodGet, "/", nil)
	reqS2.Host = "sub2.test.local"
	wS2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(wS2, reqS2)
	if wS2.Code != 200 || !strings.Contains(wS2.Body.String(), "Site 2 Index") {
		t.Fatalf("expected sub2 active to serve Site 2 Index, got %d: %s", wS2.Code, wS2.Body.String())
	}

	// 4. Test missing file on subsite returns 404 (does NOT leak site 1 index)
	reqMissing := httptest.NewRequest(http.MethodGet, "/missing.html", nil)
	reqMissing.Host = "sub2.test.local"
	wMissing := httptest.NewRecorder()
	s.Handler().ServeHTTP(wMissing, reqMissing)
	if wMissing.Code != 404 {
		t.Fatalf("expected sub2 missing file to return 404, got %d: %s", wMissing.Code, wMissing.Body.String())
	}

	// 5. Disable subsite in DB
	site2.Status = "disabled"
	if err := db.UpdateSite(context.Background(), database, site2); err != nil {
		t.Fatal(err)
	}

	// 6. Request to disabled subsite static root must return 403
	reqDisabled := httptest.NewRequest(http.MethodGet, "/", nil)
	reqDisabled.Host = "sub2.test.local"
	wDisabled := httptest.NewRecorder()
	s.Handler().ServeHTTP(wDisabled, reqDisabled)
	if wDisabled.Code != 403 || !strings.Contains(wDisabled.Body.String(), "站点已停用") {
		t.Fatalf("expected disabled site to return 403, got %d: %s", wDisabled.Code, wDisabled.Body.String())
	}

	// 7. Public API to disabled subsite must return 403
	reqPubAPI := httptest.NewRequest(http.MethodGet, "/api/content", nil)
	reqPubAPI.Host = "sub2.test.local"
	wPubAPI := httptest.NewRecorder()
	s.Handler().ServeHTTP(wPubAPI, reqPubAPI)
	if wPubAPI.Code != 403 {
		t.Fatalf("expected public API on disabled site to return 403, got %d: %s", wPubAPI.Code, wPubAPI.Body.String())
	}

	// 8. Admin access to disabled site still succeeds for management
	seedTestAdmin(t, s)
	adminToken, _ := s.createSession("gocms")
	wAdminSettings := categoryRequest(t, s, adminToken, "GET", fmt.Sprintf("/api/admin/site-settings?site_id=%d", site2.ID), "")
	if wAdminSettings.Code != 200 {
		t.Fatalf("expected admin to be able to manage disabled site settings, got %d: %s", wAdminSettings.Code, wAdminSettings.Body.String())
	}
}




