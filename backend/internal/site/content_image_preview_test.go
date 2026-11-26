package site

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminContentImagePreview(t *testing.T) {
	server, database, root, _ := newSiteAdminTestServer(t)
	if _, err := database.Exec(`UPDATE gocms_site SET domain='www.example.test' WHERE id=1;
		INSERT INTO gocms_site (id, name, code, is_default, status) VALUES (2, 'Site 2', 'site2', 0, 'active');
		INSERT INTO gocms_language (site_id, code, name, is_enabled, site_mode, domain)
		VALUES (1, 'en-US', 'English', 1, 'standalone', 'https://en.example.test:8443')`); err != nil {
		t.Fatal(err)
	}
	server.assetsRoot = t.TempDir()
	for _, siteID := range []int{1, 2} {
		folder := filepath.Join(server.assetsRoot, fmt.Sprint(siteID), "images")
		if err := os.MkdirAll(folder, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(folder, "shared.svg"), []byte(previewTestSVG(siteID)), 0644); err != nil {
			t.Fatal(err)
		}
	}
	group := siteAdminRequest(server, root, http.MethodPost, "/api/admin/groups", "", `{"name":"Editors","permissions":["content"],"site_ids":[2]}`)
	if group.Code != http.StatusOK {
		t.Fatalf("create editor group: %d %s", group.Code, group.Body.String())
	}
	var groupID int64
	if err := database.QueryRow(`SELECT id FROM gocms_admin_group WHERE name='Editors'`).Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	editor := siteAdminRequest(server, root, http.MethodPost, "/api/admin/users", "", fmt.Sprintf(`{"username":"preview-editor","password":"test-password","group_id":%d}`, groupID))
	if editor.Code != http.StatusOK {
		t.Fatalf("create editor: %d %s", editor.Code, editor.Body.String())
	}
	token, err := server.createSession("preview-editor")
	if err != nil {
		t.Fatal(err)
	}

	const route = "/api/admin/content/image-preview"
	preview := func(site int, path string) string {
		return fmt.Sprintf("%s?site_id=%d&path=%s", route, site, url.QueryEscape(path))
	}
	for _, siteID := range []int{1, 2} {
		response := siteAdminRequest(server, root, http.MethodGet, preview(siteID, "/images/shared.svg"), "1", "")
		if response.Code != http.StatusOK || response.Body.String() != previewTestSVG(siteID) {
			t.Fatalf("site %d preview: %d %s", siteID, response.Code, response.Body.String())
		}
		if typ := response.Header().Get("Content-Type"); typ != "image/svg+xml" {
			t.Errorf("unexpected preview type: %q", typ)
		}
		if !strings.Contains(response.Header().Get("Cache-Control"), "no-store") {
			t.Errorf("preview must not be publicly cached")
		}
	}
	for _, tc := range []struct {
		name, token, method, target string
		status                      int
	}{
		{"authorized site", token, http.MethodGet, preview(2, "/images/shared.svg"), http.StatusOK},
		{"head", token, http.MethodHead, preview(2, "/images/shared.svg"), http.StatusOK},
		{"another site", token, http.MethodGet, preview(1, "/images/shared.svg"), http.StatusForbidden},
		{"anonymous", "", http.MethodGet, preview(2, "/images/shared.svg"), http.StatusUnauthorized},
		{"no explicit site", root, http.MethodGet, route + "?path=/images/shared.svg", http.StatusNotFound},
		{"public assets only", root, http.MethodGet, preview(2, "/assets/1/images/shared.svg"), http.StatusNotFound},
		{"non-image", root, http.MethodGet, preview(2, "/images/page.html"), http.StatusNotFound},
		{"external address", root, http.MethodGet, preview(2, "https://example.test/images/shared.svg"), http.StatusNotFound},
		{"missing image", root, http.MethodGet, preview(2, "/images/missing.svg"), http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := siteAdminRequest(server, tc.token, tc.method, tc.target, "", "")
			if response.Code != tc.status {
				t.Fatalf("got %d, want %d: %s", response.Code, tc.status, response.Body.String())
			}
		})
	}

	var sites []adminSiteView
	siteResponse := siteAdminRequest(server, root, http.MethodGet, "/api/admin/sites", "", "")
	if err := json.Unmarshal(siteResponse.Body.Bytes(), &sites); err != nil || len(sites) != 2 || sites[0].PublicOrigin != "https://www.example.test" || sites[1].PublicOrigin != "" {
		t.Fatalf("site origins: %s (%v)", siteResponse.Body.String(), err)
	}
	var languages []LanguageItem
	languageResponse := siteAdminRequest(server, root, http.MethodGet, "/api/admin/languages", "1", "")
	if err := json.Unmarshal(languageResponse.Body.Bytes(), &languages); err != nil {
		t.Fatal(err)
	}
	for _, language := range languages {
		if language.Code == "en-US" && language.PublicOrigin == "https://en.example.test:8443" {
			return
		}
	}
	t.Fatalf("standalone language origin missing: %s", languageResponse.Body.String())
}

func previewTestSVG(siteID int) string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><title>Site %d</title></svg>`, siteID)
}
