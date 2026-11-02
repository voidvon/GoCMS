package generator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocms/internal/db"
)

func indexTargetsFixture(t *testing.T) (Publisher, string) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	ctx := context.Background()
	if err := db.CreateSchema(ctx, database); err != nil {
		t.Fatal(err)
	}
	for _, site := range []db.Site{
		{Name: "任意甲站", Code: "alpha", Domain: "alpha.example.test"},
		{Name: "任意乙站", Code: "beta", Domain: "beta.example.test"},
	} {
		if _, err := db.CreateSite(ctx, database, &site); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec(`INSERT INTO gocms_language (site_id, code, name, path_prefix, site_mode, domain, output_dir, is_enabled) VALUES
		(2, 'en', 'English', 'international', 'subdir', '', '', 1),
		(2, 'fr', 'Français', '', 'standalone', 'fr.alpha.example.test', 'isolated-french', 1),
		(2, 'de', 'Disabled', '', 'standalone', 'disabled.example.test', 'disabled-output', 0),
		(3, 'fr', 'Other French', '', 'standalone', 'fr.beta.example.test', 'other-french', 1)`); err != nil {
		t.Fatal(err)
	}
	var frenchID int64
	if err := database.QueryRow(`SELECT id FROM gocms_language WHERE site_id = 2 AND code = 'fr'`).Scan(&frenchID); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	p := Publisher{DB: database, SiteID: 2, OutputDir: "tenant-output", Web: filepath.Join(root, "web"), Data: filepath.Join(root, "data")}
	for _, page := range []struct{ dir, path, title string }{
		{p.TargetWeb(), "index.html", "Alpha home"},
		{p.TargetWeb(), "international/catalog/p-7.html", "English published"},
		{p.TargetWeb(), "404.html", "Error"},
		{filepath.Join(p.Web, "isolated-french"), "index.html", "Accueil"},
		{filepath.Join(p.Web, "isolated-french"), "arbitrary/entry.htm", "Article français"},
		{filepath.Join(p.Web, "isolated-french"), "404.html", "Erreur"},
		{filepath.Join(p.Web, "other-french"), "index.html", "Other site's private output"},
	} {
		writeTemplate(t, page.dir, page.path, "<html><head><title>"+page.title+"</title><meta name=\"description\" content=\"Published metadata\"></head><body>Published</body></html>")
	}
	return p, fmt.Sprintf("language:%d", frenchID)
}

func TestIndexTargetsGroupLanguagesAndReportActualFiles(t *testing.T) {
	p, french := indexTargetsFixture(t)
	targets, err := p.IndexTargets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || targets[0].ID != "main" || targets[1].ID != french {
		t.Fatalf("unexpected groups: %+v", targets)
	}
	if len(targets[0].Languages) != 2 || targets[0].Languages[1].Path != "/international/" || len(targets[1].Languages) != 1 {
		t.Fatalf("subdirectory language was not merged: %+v", targets)
	}
	for _, target := range targets {
		if !target.Published || len(target.Files) != 3 || target.Issue != "" {
			t.Fatalf("unexpected initial status: %+v", target)
		}
		for _, file := range target.Files {
			if file.Exists || file.UpdatedAt != nil || !strings.HasPrefix(file.URL, target.URL+"/") {
				t.Fatalf("incorrect initial file status: %+v", file)
			}
		}
	}
	if _, err := p.GenerateIndexes(context.Background(), french, "xml"); err != nil {
		t.Fatal(err)
	}
	targets, err = p.IndexTargets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if targets[0].Files[1].Exists || !targets[1].Files[1].Exists || targets[1].Files[1].UpdatedAt == nil || !targets[1].Files[1].Aliases[0].Exists || targets[1].Files[0].Exists {
		t.Fatalf("single file generation was not isolated: %+v", targets)
	}
}

func TestGenerateIndexesSeparatesOriginsAndUsesPublishedPages(t *testing.T) {
	p, _ := indexTargetsFixture(t)
	ctx := context.Background()
	if err := db.SaveSiteSettingsForSite(ctx, p.DB, 2, map[string]string{"consent_config": `{"enabled":false,"languages":{"fr":{"mode":"on"}}}`}); err != nil {
		t.Fatal(err)
	}
	targets, err := p.GenerateIndexes(ctx, "all", "all")
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		for _, file := range target.Files {
			if !file.Exists || file.UpdatedAt == nil {
				t.Fatalf("generated file status missing: %+v", file)
			}
		}
	}
	frenchWeb := filepath.Join(p.Web, "isolated-french")
	for _, test := range []struct{ web, domain, wanted, forbidden string }{
		{p.TargetWeb(), "https://alpha.example.test", "international/catalog/p-7.html", "arbitrary/entry.htm"},
		{frenchWeb, "https://fr.alpha.example.test", "arbitrary/entry.htm", "international/catalog/p-7.html"},
	} {
		for _, filename := range []string{"sitemap.html", "sitemap.xml", "Sitemap.xml", "llms.txt"} {
			body := readGenerated(t, test.web, filename)
			if !strings.Contains(body, test.wanted) || strings.Contains(body, test.forbidden) || strings.Contains(body, "404.html") || strings.Contains(body, "beta.example.test") {
				t.Fatalf("%s/%s leaked another origin or omitted a page: %s", test.web, filename, body)
			}
			if filename != "sitemap.html" && !strings.Contains(body, test.domain+"/"+test.wanted) {
				t.Fatalf("%s uses the wrong origin: %s", filename, body)
			}
		}
		if readGenerated(t, test.web, "sitemap.xml") != readGenerated(t, test.web, "Sitemap.xml") {
			t.Fatal("XML aliases diverged")
		}
	}
	if body := readGenerated(t, frenchWeb, "sitemap.html"); !strings.Contains(body, `"language":"fr"`) || !strings.Contains(body, `"enabled":true`) {
		t.Fatal("standalone HTML index lost its consent policy")
	}
	if body := readGenerated(t, frenchWeb, "llms.txt"); !strings.Contains(body, "# Accueil") || !strings.Contains(body, "Article français") {
		t.Fatal("llms did not use the language's published metadata")
	}
	if _, err := os.Stat(filepath.Join(p.Web, "other-french", "llms.txt")); !os.IsNotExist(err) {
		t.Fatal("another site's indexes were generated")
	}
}

func TestGenerateIndexesFailuresPreserveExistingFiles(t *testing.T) {
	p, french := indexTargetsFixture(t)
	ctx := context.Background()
	if _, err := p.GenerateIndexes(ctx, "all", "all"); err != nil {
		t.Fatal(err)
	}
	before := readGenerated(t, p.TargetWeb(), "sitemap.html")
	writeTemplate(t, p.TargetWeb(), "new.html", "<title>New page</title>")
	if _, err := p.DB.Exec(`UPDATE gocms_language SET domain = '' WHERE site_id = 2 AND code = 'fr'`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.GenerateIndexes(ctx, "all", "all"); !errors.Is(err, ErrIndexConfiguration) {
		t.Fatalf("missing standalone domain accepted: %v", err)
	}
	if got := readGenerated(t, p.TargetWeb(), "sitemap.html"); got != before {
		t.Fatal("validation failure partially replaced indexes")
	}
	if _, err := p.GenerateIndexes(ctx, "language:999999", "all"); !errors.Is(err, ErrIndexTargetNotFound) {
		t.Fatalf("foreign or unknown target accepted: %v", err)
	}
	if _, err := p.GenerateIndexes(ctx, french, "unsupported"); !errors.Is(err, ErrIndexFormat) {
		t.Fatalf("invalid format accepted: %v", err)
	}
	if _, err := p.GenerateIndexes(ctx, "main", "xml"); err != nil {
		t.Fatalf("a pending standalone domain blocked the valid main target: %v", err)
	}
	lock, err := os.OpenFile(filepath.Join(p.Data, p.lockPath()), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	unlock, err := acquirePublishLock(lock)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err := p.GenerateIndexes(ctx, "main", "all"); !errors.Is(err, ErrPublishBusy) {
		t.Fatalf("publication lock bypassed: %v", err)
	}
}

func TestGenerateIndexesExcludesNestedStandaloneDirectory(t *testing.T) {
	p, _ := indexTargetsFixture(t)
	if _, err := p.DB.Exec(`UPDATE gocms_language SET output_dir = 'tenant-output/nested-french' WHERE site_id = 2 AND code = 'fr'`); err != nil {
		t.Fatal(err)
	}
	writeTemplate(t, p.TargetWeb(), "nested-french/index.html", "<title>Nested French</title>")
	writeTemplate(t, p.TargetWeb(), "nested-french/french-only.html", "<title>French</title>")
	if _, err := p.GenerateIndexes(context.Background(), "main", "all"); err != nil {
		t.Fatal(err)
	}
	for _, filename := range []string{"sitemap.html", "sitemap.xml", "llms.txt"} {
		if body := readGenerated(t, p.TargetWeb(), filename); strings.Contains(body, "french-only") || strings.Contains(body, "nested-french") {
			t.Fatalf("nested standalone pages leaked into main index: %s", body)
		}
	}
}

func TestGenerateIndexesRequiresPublishedPages(t *testing.T) {
	p, french := indexTargetsFixture(t)
	if err := os.RemoveAll(filepath.Join(p.Web, "isolated-french")); err != nil {
		t.Fatal(err)
	}
	if _, err := p.GenerateIndexes(context.Background(), french, "all"); !errors.Is(err, ErrIndexNotPublished) {
		t.Fatalf("unpublished target accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(p.Web, "isolated-french")); !os.IsNotExist(err) {
		t.Fatal("unpublished target gained empty indexes")
	}
}
