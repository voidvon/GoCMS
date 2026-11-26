package site

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAdminNameSharedAcrossSites(t *testing.T) {
	server, database, root, _ := newSiteAdminTestServer(t)
	if _, err := database.Exec(`INSERT INTO gocms_site (id, name, code) VALUES (2, '分站', 'other');
		INSERT INTO gocms_admin_group (name, permissions, site_ids) VALUES ('Editors', '["content"]', '[2]');
		INSERT INTO gocms_admin_user (username, group_id) SELECT 'editor', id FROM gocms_admin_group WHERE name='Editors'`); err != nil {
		t.Fatal(err)
	}
	editor, err := server.createSession("editor")
	if err != nil {
		t.Fatal(err)
	}
	call := func(token, method, siteID, payload string, status int) string {
		t.Helper()
		res := siteAdminRequest(server, token, method, adminSettingsRoute, siteID, payload)
		if res.Code != status {
			t.Fatalf("%s for site %s returned %d, want %d: %s", method, siteID, res.Code, status, res.Body.String())
		}
		if status != http.StatusOK {
			return ""
		}
		var data map[string]string
		if err := json.Unmarshal(res.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		name, ok := data["admin_name"]
		if !ok {
			t.Fatalf("missing admin_name: %s", res.Body.String())
		}
		return name
	}
	const name = "统一管理台"
	const payload = `{"admin_name":"  统一管理台  "}`
	call("", http.MethodGet, "2", "", http.StatusUnauthorized)
	call(editor, http.MethodPut, "2", payload, http.StatusForbidden)
	call(root, http.MethodPut, "2", `{}`, http.StatusBadRequest)
	if got := call(root, http.MethodGet, "1", "", http.StatusOK); got != "" {
		t.Fatalf("initial admin name = %q", got)
	}
	if got := call(root, http.MethodPut, "2", payload, http.StatusOK); got != name {
		t.Fatalf("saved admin name = %q", got)
	}
	for _, siteID := range []string{"1", "2", "999"} {
		if got := call(editor, http.MethodGet, siteID, "", http.StatusOK); got != name {
			t.Fatalf("admin name from site selector %s = %q", siteID, got)
		}
	}
	var auditSiteID int64
	if err := database.QueryRow(`SELECT site_id FROM gocms_admin_operation WHERE path = ? AND status = 200`, adminSettingsRoute).Scan(&auditSiteID); err != nil || auditSiteID != 0 {
		t.Fatalf("global settings operation recorded against site %d: %v", auditSiteID, err)
	}
	call(root, http.MethodPut, "1", `{"admin_name":" "}`, http.StatusOK)
	if got := call(editor, http.MethodGet, "2", "", http.StatusOK); got != "" {
		t.Fatalf("cleared admin name = %q", got)
	}
}
