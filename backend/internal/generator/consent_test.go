package generator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocms/internal/db"
)

func TestConsentGeneratedAcrossConfiguredRoutesLanguagesAndFailures(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	if err := db.CreateSchema(ctx, database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO gocms_language (code, name, path_prefix, site_mode, domain, output_dir) VALUES
		('fr', 'Français', 'bonjour', 'subdir', '', ''), ('de', 'Deutsch', '', 'standalone', 'de.example.test', 'german');
		INSERT INTO gocms_category (id, name, page_type, route_id, list_path, list_file_pattern, list_template, detail_path, detail_file_pattern, detail_template, list_page_size)
		VALUES (1, '任意行业栏目', 'list', 1, 'arbitrary', 'page-{id}.html', 'custom/list.html', 'articles', '{id}.html', 'custom/detail.html', 1);
		INSERT INTO gocms_content (id, category_id, route_key, title, visible) VALUES (1,1,'first','First',1),(2,1,'second','Second',1)`); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveSiteSettingsForSite(ctx, database, 1, map[string]string{"consent_config": `{"enabled":false,"languages":{"fr":{"mode":"on","text":{"title":"Votre choix"}},"de":{"mode":"on"}}}`}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	templates := filepath.Join(root, "templates")
	page := `<html><head><title>Page</title></head><body>content {{currentLang}}<script data-consent="analytics" src="/tracker.js"></script></body></html>`
	for _, name := range []string{"index.html", "msg.html", "search.html", "404.html", "custom/list.html", "custom/detail.html"} {
		writeTemplate(t, templates, name, page)
	}
	p := Publisher{DB: database, Web: filepath.Join(root, "web"), Templates: templates, Data: filepath.Join(root, "data"), SiteID: 1}
	if _, err := p.Generate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.html", "bonjour/index.html", "bonjour/arbitrary/page-1-2.html", "bonjour/articles/first.html", "bonjour/404.html"} {
		body := readGenerated(t, p.TargetWeb(), name)
		if !strings.Contains(body, `data-consent-src=/tracker.js`) && !strings.Contains(body, `data-consent-src="/tracker.js"`) {
			t.Fatalf("resource not blocked on %s", name)
		}
		if strings.HasPrefix(name, "bonjour/") && !strings.Contains(body, "Votre choix") {
			t.Fatalf("missing localized policy on %s", name)
		}
		if strings.HasPrefix(name, "bonjour/") && !strings.Contains(body, `"enabled":true`) {
			t.Fatalf("language policy not enabled on %s", name)
		}
	}
	if body := readGenerated(t, filepath.Join(root, "web", "german"), "index.html"); !strings.Contains(body, `"language":"de"`) {
		t.Fatal("standalone domain missing its policy")
	}
	// A partial content refresh retains the same classification contract.
	if _, err := p.GenerateContent(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if body := readGenerated(t, p.TargetWeb(), "bonjour/articles/first.html"); !strings.Contains(body, `text/plain`) {
		t.Fatal("partial publication bypassed consent")
	}
	previous := readGenerated(t, p.TargetWeb(), "index.html")
	writeTemplate(t, templates, "custom/list.html", `<body><script data-consent="unknown" src="/tracker.js"></script></body>`)
	if _, err := p.Generate(ctx); err == nil {
		t.Fatal("invalid classification did not fail publication")
	}
	if body := readGenerated(t, p.TargetWeb(), "index.html"); body != previous {
		t.Fatal("failed publication changed live pages")
	}
	if err := db.SaveSiteSettingsForSite(ctx, database, 1, map[string]string{"consent_config": `{"max_age_days":-1}`}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Generate(ctx); err == nil {
		t.Fatal("invalid stored policy did not fail publication")
	}
	if _, err := os.Stat(filepath.Join(p.TargetWeb(), "bonjour/articles/first.html")); err != nil {
		t.Fatal("failed publication removed live language pages")
	}
}
