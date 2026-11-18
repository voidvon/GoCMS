package generator

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"gocms/internal/db"
	"gocms/internal/sitecontacts"
)

func TestSiteContactsUsesSelectedOrderAndConfiguredFallback(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.CreateSchema(ctx, database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		UPDATE gocms_language SET is_fallback = 0, contact_card_ids = '["north","south"]' WHERE code = 'zh-CN';
		INSERT INTO gocms_language (code, name, path_prefix, is_fallback, contact_card_ids) VALUES
			('fr', 'Français', 'equipe', 1, '["south","north"]'),
			('es', 'Español', 'es', 0, '[]'),
			('de', 'Deutsch', 'de', 0, '["north"]');
		INSERT INTO gocms_category (name, page_type, list_path, list_file_pattern, cover_template) VALUES
			('Über uns', 'cover', 'locations', 'index.html', 'offices.html')`); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveSiteSettingsForSite(ctx, database, 1, map[string]string{
		sitecontacts.SettingKey: `[{"id":"north","name":"North","email":"north@example.test"},{"id":"south","name":"South & partners","email":"south@example.test"}]`,
	}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	templates := filepath.Join(root, "templates")
	writeTemplate(t, templates, "offices.html", `<html><head><title>Offices</title></head><body>{{range siteContacts}}<a href="mailto:{{html .Email}}">{{html .Name}}</a>{{end}}</body></html>`)
	for _, name := range []string{"index.html", "404.html", "msg.html", "search.html"} {
		writeTemplate(t, templates, name, "<html><body>Page</body></html>")
	}
	p := Publisher{DB: database, Web: filepath.Join(root, "web"), Templates: templates, Data: filepath.Join(root, "data"), SiteID: 1}
	if _, err := p.Generate(ctx); err != nil {
		t.Fatal(err)
	}
	for route, order := range map[string][]string{
		"locations/index.html":        {"north", "south"},
		"equipe/locations/index.html": {"south", "north"},
		"es/locations/index.html":     {"south", "north"},
		"de/locations/index.html":     {"north"},
	} {
		body := readGenerated(t, p.TargetWeb(), route)
		last := -1
		for _, id := range order {
			if index := strings.Index(body, "mailto:"+id+"@example.test"); index <= last {
				t.Fatalf("%s: missing or out of order %s in %s", route, id, body)
			} else {
				last = index
			}
		}
		if strings.Count(body, "mailto:") != len(order) {
			t.Fatalf("%s: unexpected cards in %s", route, body)
		}
	}
}
