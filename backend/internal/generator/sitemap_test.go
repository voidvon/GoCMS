package generator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocms/internal/db"
)

func TestGenerateSitemapFromPublishedPages(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.CreateSchema(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO "gocms_site_setting" ("key", "value") VALUES ('site_url', 'https://example.com/')`); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	web := filepath.Join(root, "web")
	for name, body := range map[string]string{
		"index.html":        "home",
		"catalog/item.htm":  "catalog",
		"sitemap.html":      "old html map",
		"Sitemap.xml":       "old xml map",
		"assets/ignored.js": "asset",
	} {
		path := filepath.Join(web, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}

	publisher := Publisher{DB: database, Web: web, Data: filepath.Join(root, "data")}
	if filename, err := publisher.GenerateSitemap(context.Background(), SitemapHTML); err != nil || filename != "sitemap.html" {
		t.Fatalf("generate html sitemap = %q, %v", filename, err)
	}
	html, err := os.ReadFile(filepath.Join(web, "sitemap.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), `href="/index.html"`) || !strings.Contains(string(html), `href="/catalog/item.htm"`) || strings.Contains(string(html), "old html map") || strings.Contains(string(html), "Sitemap.xml") {
		t.Fatalf("unexpected html sitemap: %s", html)
	}

	if filename, err := publisher.GenerateSitemap(context.Background(), SitemapXML); err != nil || filename != "Sitemap.xml" {
		t.Fatalf("generate xml sitemap = %q, %v", filename, err)
	}
	xml, err := os.ReadFile(filepath.Join(web, "Sitemap.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(xml), "https://example.com/catalog/item.htm") || strings.Contains(string(xml), "ignored.js") || strings.Contains(string(xml), "Sitemap.xml") {
		t.Fatalf("unexpected xml sitemap: %s", xml)
	}
}
