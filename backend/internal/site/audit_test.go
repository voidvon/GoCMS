package site

import (
	"strings"
	"testing"
	"time"
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
