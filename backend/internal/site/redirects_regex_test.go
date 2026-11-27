package site

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"gocms/internal/db"
)

func TestRegexRedirectRoutingAndLifecycle(t *testing.T) {
	s, database, token, root := newSiteAdminTestServer(t)
	site, err := db.CreateSite(context.Background(), database, &db.Site{Name: "Example", Code: "example", Domain: "second.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	siteID := fmt.Sprint(site.ID)
	if _, err := database.Exec(`INSERT INTO gocms_language (site_id, code, name, is_enabled, site_mode, domain, output_dir)
		VALUES (?, 'fr', 'French', 1, 'standalone', 'fr.example.test', 'french')`, site.ID); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{
		siteID + "/news/a.html", siteID + "/news/kept.htmlnull", siteID + "/news/chain.html", siteID + "/docs/index.html",
		siteID + "/news/a?b#c.html", siteID + "/admin/example.html", siteID + "/404.html", siteID + "/search/index.html",
		"french/news/french.html", "news/a.html", "news/default-only.html",
	} {
		filename := filepath.Join(root, file)
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(file), 0600); err != nil {
			t.Fatal(err)
		}
	}
	admin := func(method, path, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		res := siteAdminRequest(s, token, method, path, siteID, body)
		if res.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, res.Code, res.Body.String())
		}
		return res
	}
	const adminPath = "/api/admin/redirects"
	const rule = `{"match_type":"regex","source_path":"^(/.+\\.html)null$","target_url":"${1}?campaign=target#section"}`
	admin("POST", adminPath, rule, 200)
	admin("POST", adminPath, `{"source_path":"/news/exact.htmlnull","target_url":"/replacement","status_code":302}`, 200)
	admin("POST", adminPath, `{"source_path":"/news/chain.html","target_url":"/news/chain.htmlnull"}`, 200)
	admin("POST", adminPath, `{"source_path":"/docs","target_url":"/docs/index.htmlnull"}`, 200)
	admin("POST", adminPath+"/import", `{"items":[
		{"match_type":"regex","source_path":"(/news/a\\.html)null","target_url":"${1}?campaign=second"},
		{"match_type":"regex","source_path":"/legacy(?P<page>/news/a\\.html)","target_url":"$page","status_code":302,"preserve_query":false},
		{"match_type":"regex","source_path":"/search\\.html","target_url":"/search/"},
		{"match_type":"regex","source_path":"/escape(/.+)","target_url":"/${1}"}]}`, 201)

	var listed struct {
		Items []redirectRule `json:"items"`
	}
	if err := json.Unmarshal(admin("GET", adminPath, "", 200).Body.Bytes(), &listed); err != nil || len(listed.Items) != 8 {
		t.Fatalf("list: %+v %v", listed, err)
	}
	firstRule := listed.Items[len(listed.Items)-1]
	if firstRule.MatchType != redirectMatchRegex || firstRule.SourcePath != `^(/.+\.html)null$` || firstRule.TargetURL != "${1}?campaign=target#section" || !firstRule.PreserveQuery {
		t.Fatalf("regex rule did not round trip: %+v", firstRule)
	}

	for _, tc := range []struct {
		method, host, source, destination string
		status                            int
	}{
		{"GET", site.Domain, "/news/a.htmlnull/?campaign=source&tag=one&tag=two", "/news/a.html?campaign=target&tag=one&tag=two#section", 301},
		{"HEAD", site.Domain, "/news/a.htmlnull", "/news/a.html?campaign=target#section", 301},
		{"GET", site.Domain, "/news/a%3Fb%23c.htmlnull", "/news/a%3Fb%23c.html?campaign=target#section", 301},
		{"GET", site.Domain, "/legacy/news/a.html?q=ignored", "/news/a.html", 302},
		{"GET", site.Domain, "/news/kept.htmlnull", "", 200},
		{"GET", site.Domain, "/news/missing.htmlnull", "", 404},
		{"GET", site.Domain, "/news/default-only.htmlnull", "", 404},
		{"GET", "localhost", "/news/a.htmlnull", "", 404},
		{"GET", "fr.example.test", "/news/french.htmlnull", "/news/french.html?campaign=target#section", 301},
		{"GET", "fr.example.test", "/news/a.htmlnull", "", 404},
		{"GET", site.Domain, "/news/a.htmlnull/more", "", 404},
		{"GET", site.Domain, "/news/exact.htmlnull", "/replacement", 302},
		{"GET", site.Domain, "/news/chain.htmlnull", "", 404},
		{"GET", site.Domain, "/docs/index.htmlnull", "", 404},
		{"GET", site.Domain, "/admin/example.htmlnull", "", 404},
		{"GET", site.Domain, "/404.htmlnull", "", 404},
		{"GET", site.Domain, "/search.html", "", 404},
		{"GET", site.Domain, "/escape/other.example.test", "", 404},
		{"POST", site.Domain, "/news/a.htmlnull", "", 404},
	} {
		t.Run(tc.method+" "+tc.host+tc.source, func(t *testing.T) {
			res := legacyRedirectRequest(t, s, tc.method, tc.host, tc.source)
			if res.Code != tc.status || res.Header().Get("Location") != tc.destination {
				t.Fatalf("got %d %v, want %d %q", res.Code, res.Header(), tc.status, tc.destination)
			}
			if tc.destination != "" && res.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("redirect is cacheable: %v", res.Header())
			}
			if tc.method == "HEAD" && res.Body.Len() != 0 {
				t.Fatal("HEAD redirect has a body")
			}
		})
	}

	firstRule.Enabled = false
	update, err := json.Marshal(firstRule)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := fmt.Sprintf("%s/%d", adminPath, firstRule.ID)
	admin("PUT", endpoint, string(update), 200)
	if res := legacyRedirectRequest(t, s, "GET", site.Domain, "/news/a.htmlnull"); res.Code != 301 || res.Header().Get("Location") != "/news/a.html?campaign=second" {
		t.Fatalf("disabling the first rule did not expose the next one: %d %v", res.Code, res.Header())
	}
	admin("DELETE", endpoint, "", 200)
}

func TestRegexRedirectValidation(t *testing.T) {
	s, _, token, _ := newSiteAdminTestServer(t)
	for _, body := range []string{
		`{"match_type":"regex","source_path":"(","target_url":"/new"}`,
		`{"match_type":"regex","source_path":"a)|b|(?:c","target_url":"/new"}`,
		`{"match_type":"regex","source_path":"","target_url":"/new"}`,
		`{"match_type":"regex","source_path":"/(.*)","target_url":"${2}"}`,
		`{"match_type":"regex","source_path":"/(.*)","target_url":"/${missing}"}`,
		`{"match_type":"regex","source_path":"/(.*)","target_url":"/${"}`,
		`{"match_type":"regex","source_path":"/(.*)","target_url":"https://example.test/${1}"}`,
		`{"match_type":"regex","source_path":"/(.*)","target_url":"//example.test/${1}"}`,
		`{"match_type":"regex","source_path":"/(.*)","target_url":"/%5c${1}"}`,
		`{"match_type":"other","source_path":"/old","target_url":"/new"}`,
	} {
		res := siteAdminRequest(s, token, "POST", "/api/admin/redirects", "1", body)
		if res.Code != 400 {
			t.Fatalf("accepted invalid rule %s: %d %s", body, res.Code, res.Body.String())
		}
	}
}
