package site

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"gocms/internal/db"
)

func TestLogCleanupKeepsLoginProtection(t *testing.T) {
	s, database, token := newCategoryTestServer(t)
	for i := 0; i < 5; i++ {
		r := categoryRequest(t, s, "", "POST", "/api/admin/login", `{"username":"missing","password":"wrong"}`)
		if r.Code != 401 {
			t.Fatalf("login: %d", r.Code)
		}
	}
	before := `{"before":"` + time.Now().UTC().Format("2006-01-02") + `"}`
	r := categoryRequest(t, s, token, "POST", "/api/admin/logs/clear", before)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	r = categoryRequest(t, s, "", "POST", "/api/admin/login", `{"username":"missing","password":"wrong"}`)
	if r.Code != 429 {
		t.Fatalf("cleanup unlocked login: %d", r.Code)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM gocms_admin_login`).Scan(&count); err != nil || count != 5 {
		t.Fatalf("blocked request extended lock: %d %v", count, err)
	}
	if _, err := database.Exec(`INSERT INTO gocms_admin_group(name, permissions) VALUES ('viewer', '["logs"]'); INSERT INTO gocms_admin_user(username, group_id) SELECT 'viewer', id FROM gocms_admin_group WHERE name='viewer'`); err != nil {
		t.Fatal(err)
	}
	viewer, err := s.createSession("viewer")
	if err != nil {
		t.Fatal(err)
	}
	if r := categoryRequest(t, s, viewer, "POST", "/api/admin/logs/clear", before); r.Code != 403 {
		t.Fatalf("viewer deleted logs: %d", r.Code)
	}
	if _, err := database.Exec(`UPDATE gocms_admin_login SET created_at = datetime('now','-16 minutes')`); err != nil {
		t.Fatal(err)
	}
	if r := categoryRequest(t, s, "", "POST", "/api/admin/login", `{"username":"missing","password":"wrong"}`); r.Code != 401 {
		t.Fatalf("lock did not expire: %d", r.Code)
	}
}

func TestOperationLogs(t *testing.T) {
	s, database, token := newCategoryTestServer(t)
	r := categoryRequest(t, s, token, "POST", "/api/admin/groups?secret=private", `{"name":"Operators","permissions":[]}`)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	var username, path string
	var status int
	if err := database.QueryRow(`SELECT username, path, status FROM gocms_admin_operation`).Scan(&username, &path, &status); err != nil {
		t.Fatal(err)
	}
	if username != "gocms" || path != "/api/admin/groups" || status != 200 {
		t.Fatalf("incorrect log %q %q %d", username, path, status)
	}
	r = categoryRequest(t, s, token, "POST", "/api/admin/groups", `{"name":"","password":"private-password"}`)
	if r.Code != 400 {
		t.Fatal(r.Body.String())
	}
	r = categoryRequest(t, s, token, "GET", "/api/admin/logs?username=gocms", "")
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"total":2`) || strings.Contains(r.Body.String(), "private") {
		t.Fatalf("bad logs: %s", r.Body.String())
	}
	r = categoryRequest(t, s, token, "GET", "/api/admin/logs?username=missing", "")
	if !strings.Contains(r.Body.String(), `"total":0`) {
		t.Fatal(r.Body.String())
	}
	if _, err := database.Exec(`INSERT INTO gocms_admin_user (username) VALUES ('reader')`); err != nil {
		t.Fatal(err)
	}
	reader, err := s.createSession("reader")
	if err != nil {
		t.Fatal(err)
	}
	if r := categoryRequest(t, s, reader, "GET", "/api/admin/logs", ""); r.Code != 403 {
		t.Fatalf("unprivileged log access: %d", r.Code)
	}
	if _, err := database.Exec(`INSERT INTO gocms_admin_group (name, permissions) VALUES ('Auditors', '["logs"]'); UPDATE gocms_admin_user SET group_id = (SELECT id FROM gocms_admin_group WHERE name = 'Auditors') WHERE username = 'reader'`); err != nil {
		t.Fatal(err)
	}
	if r := categoryRequest(t, s, reader, "GET", "/api/admin/logs", ""); r.Code != 200 {
		t.Fatalf("auditor denied: %d", r.Code)
	}
}

func TestMultiSiteAuditLogIsolation(t *testing.T) {
	s, database, root := newCategoryTestServer(t)
	seedTestAdmin(t, s)
	superToken, err := s.createSession("gocms")
	if err != nil {
		t.Fatal(err)
	}

	// 1. Create Site 2
	resS2, err := db.CreateSite(context.Background(), database, &db.Site{
		Name:      "分站二",
		Code:      "site2",
		Status:    "active",
		OutputDir: "site2",
	})
	if err != nil {
		t.Fatal(err)
	}
	site2ID := resS2.ID

	// 2. Perform operations on Site 1 and Site 2
	r1 := categoryRequest(t, s, superToken, "POST", "/api/admin/site-settings?site_id=1", `{"site_name":"Site 1"}`)
	if r1.Code != 200 {
		t.Fatalf("site 1 op: %d %s", r1.Code, r1.Body.String())
	}
	r2 := categoryRequest(t, s, superToken, "POST", fmt.Sprintf("/api/admin/site-settings?site_id=%d", site2ID), `{"site_name":"Site 2"}`)
	if r2.Code != 200 {
		t.Fatalf("site 2 op: %d %s", r2.Code, r2.Body.String())
	}

	// 3. Create an auditor restricted to Site 1
	if _, err := database.Exec(`
		INSERT INTO gocms_admin_group (id, name, permissions, site_ids) VALUES (101, 'Site 1 Auditor', '["logs", "login_logs"]', '[1]');
		INSERT INTO gocms_admin_user (username, group_id, site_ids) VALUES ('auditor_s1', 101, '[1]');
	`); err != nil {
		t.Fatal(err)
	}
	s1AuditorToken, err := s.createSession("auditor_s1")
	if err != nil {
		t.Fatal(err)
	}

	// 4. Auditor 1 queries logs on Site 1 -> must only see site 1 ops
	rLogsS1 := categoryRequest(t, s, s1AuditorToken, "GET", "/api/admin/logs?site_id=1", "")
	if rLogsS1.Code != 200 {
		t.Fatalf("auditor s1 logs failed: %d %s", rLogsS1.Code, rLogsS1.Body.String())
	}
	if !strings.Contains(rLogsS1.Body.String(), `"site_id":1`) {
		t.Fatalf("expected site 1 logs, got: %s", rLogsS1.Body.String())
	}
	if strings.Contains(rLogsS1.Body.String(), fmt.Sprintf(`"site_id":%d`, site2ID)) {
		t.Fatalf("site 2 logs leaked to site 1 auditor: %s", rLogsS1.Body.String())
	}

	// 5. Auditor 1 tries to query logs for Site 2 -> must be 403 Forbidden
	rLogsS2Forbidden := categoryRequest(t, s, s1AuditorToken, "GET", fmt.Sprintf("/api/admin/logs?site_id=%d", site2ID), "")
	if rLogsS2Forbidden.Code != 403 {
		t.Fatalf("expected 403 for unauthorized site logs, got: %d", rLogsS2Forbidden.Code)
	}

	// 6. Auditor 1 queries login logs -> can only see their own logins, not superadmin's
	rLoginLogs := categoryRequest(t, s, s1AuditorToken, "GET", "/api/admin/logins", "")
	if rLoginLogs.Code != 200 {
		t.Fatalf("auditor logins: %d", rLoginLogs.Code)
	}
	if strings.Contains(rLoginLogs.Body.String(), "gocms") {
		t.Fatalf("superadmin logins leaked to subsite auditor: %s", rLoginLogs.Body.String())
	}

	// 7. Superadmin can filter logs by site_id=2
	rSuperS2 := categoryRequest(t, s, superToken, "GET", fmt.Sprintf("/api/admin/logs?site_id=%d", site2ID), "")
	if rSuperS2.Code != 200 || !strings.Contains(rSuperS2.Body.String(), fmt.Sprintf(`"site_id":%d`, site2ID)) {
		t.Fatalf("superadmin cannot view site 2 logs: %s", rSuperS2.Body.String())
	}

	// 8. Delete Site 2 and verify cascade deletion of audit records
	_ = root
	rDel := categoryRequest(t, s, superToken, "DELETE", fmt.Sprintf("/api/admin/sites/%d", site2ID), "")
	if rDel.Code != 200 {
		t.Fatalf("delete site 2 failed: %d %s", rDel.Code, rDel.Body.String())
	}
	var s2Count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM gocms_admin_operation WHERE site_id = ?`, site2ID).Scan(&s2Count); err != nil || s2Count != 0 {
		t.Fatalf("expected 0 site 2 ops remaining, got %d, err: %v", s2Count, err)
	}
}
