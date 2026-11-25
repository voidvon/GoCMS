package generator

import (
	"path/filepath"
	"strings"
	"testing"

	"gocms/internal/db"
)

func TestPublisherUsesPublicR2ImageLinksForConfiguredSite(t *testing.T) {
	ctx := t.Context()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.CreateSchema(ctx, database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO gocms_site (id, name, code, domain, aliases)
			VALUES (2, 'Media test', 'media-test', 'example.test', '["alias.example.test"]');
		INSERT INTO gocms_language (site_id, code, name, is_default, is_enabled, site_mode, domain, output_dir)
			VALUES (2, 'en', 'English', 1, 1, 'subdir', '', ''),
			       (2, 'de', 'Deutsch', 0, 1, 'standalone', 'de.example.test', 'site-de');
		INSERT INTO gocms_platform_media_storage (id, enabled, images, endpoint, bucket, public_url, access_key, secret_ciphertext)
			VALUES (1, 1, 1, '', '', 'https://media.example.test', '', '');
		INSERT INTO gocms_media (site_id, kind, storage_provider, shared_storage, storage_path, public_path, status) VALUES
			(2, 'image', 'r2',    1, 'sites/2/image/migrated/a.jpg', '/assets/2/uploads/old.jpg', 'active'),
			(2, 'image', 'r2',    1, 'sites/2/image/new.webp', '/api/files/media/2/new.webp', 'active'),
			(2, 'image', 'r2',    1, 'sites/2/image/deleting.jpg', '/assets/2/uploads/deleting.jpg', 'deleting'),
			(2, 'image', 'local', 0, '2/uploads/still-local.png', '/assets/2/uploads/still-local.png', 'active'),
			(1, 'image', 'r2',    1, 'sites/1/image/another.jpg', '/assets/1/uploads/another.jpg', 'active');`); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	publisher := Publisher{
		DB: database, SiteID: 2, Data: filepath.Join(root, "data"),
		Web: filepath.Join(root, "web"), Templates: filepath.Join(root, "templates"),
	}
	writeTemplate(t, publisher.Templates, "index.html", `<!doctype html>
		<meta property="og:image" content="https://alias.example.test/assets/2/uploads/old.jpg">
		<style> .hero { background:url('/assets/2/uploads/old.jpg') } </style>
		<script type="application/ld+json">{"image":"/assets/2/uploads/old.jpg"}</script>
		<script>const illustration = "/assets/2/uploads/old.jpg";</script>
		<div style="background-image:url(/uploads/old.jpg)">
			<picture><source srcset="/uploads/old.jpg 1x, /api/files/media/2/new.webp 2x">
			<img src="/assets/2/uploads/old.jpg?v=7#full" data-src="uploads/old.jpg"></picture>
			<a href="https://de.example.test/assets/2/uploads/old.jpg">Image</a>
			<img src="https://external.example.test/assets/2/uploads/old.jpg">
			<img src="/assets/1/uploads/another.jpg">
			<img src="/assets/2/uploads/still-local.png">
			<img src="/assets/2/uploads/deleting.jpg">
			<img src="/images/theme.png" alt="Arts &amp; crafts">
		</div>`)
	writeTemplate(t, publisher.Templates, "msg.html", "message")
	writeTemplate(t, publisher.Templates, "search.html", "search")
	const cdn = "https://media.example.test/"
	const migrated = cdn + "sites/2/image/migrated/a.jpg"
	if _, err := publisher.Generate(ctx); err != nil {
		t.Fatal(err)
	}
	if output := readGenerated(t, publisher.TargetWeb(), "index.html"); strings.Contains(output, migrated) {
		t.Fatal("R2 image rewriting was enabled by default")
	}
	if err := db.SaveSiteSettingsForSite(ctx, database, 2, map[string]string{publishR2ImageURLs: "true", "html_minify": "0"}); err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.Generate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{publisher.TargetWeb(), filepath.Join(publisher.Web, "site-de")} {
		output := readGenerated(t, directory, "index.html")
		for _, expected := range []string{
			`content="` + migrated + `"`,
			`url("` + migrated + `")`,
			`{"image":"` + migrated + `"}`,
			migrated + " 1x, " + cdn + "sites/2/image/new.webp 2x",
			migrated + "?v=7#full",
			`data-src="` + migrated + `"`,
			`href="` + migrated + `"`,
			"const illustration = \"/assets/2/uploads/old.jpg\"",
			"https://external.example.test/assets/2/uploads/old.jpg",
			"/assets/1/uploads/another.jpg",
			"/assets/2/uploads/still-local.png",
			"/assets/2/uploads/deleting.jpg",
			`src="/images/theme.png" alt="Arts &amp; crafts"`,
		} {
			if !strings.Contains(output, expected) {
				t.Errorf("published %s missing %q in %s", directory, expected, output)
			}
		}
	}
	if _, err := database.Exec(`UPDATE gocms_platform_media_storage SET public_url='' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.Generate(ctx); err != nil {
		t.Fatal(err)
	}
	output := readGenerated(t, publisher.TargetWeb(), "index.html")
	if strings.Contains(output, cdn) || !strings.Contains(output, "/api/files/media/2/new.webp") {
		t.Fatalf("private R2 bucket did not keep the GoCMS routes: %s", output)
	}
}
