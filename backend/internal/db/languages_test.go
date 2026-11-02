package db

import (
	"context"
	"fmt"
	"testing"
)

func TestEnsureLanguagesMigratesSharedConfigurationPerSite(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	if err := EnsureSites(ctx, database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		UPDATE gocms_site SET is_default = 0;
		INSERT INTO gocms_site (id, name, code, is_default) VALUES (2, '任意甲站', 'alpha', 1), (3, '任意乙站', 'beta', 0);
		CREATE TABLE gocms_language (
			id INTEGER PRIMARY KEY AUTOINCREMENT, code TEXT NOT NULL UNIQUE, name TEXT NOT NULL,
			is_default INTEGER NOT NULL DEFAULT 0, is_fallback INTEGER NOT NULL DEFAULT 0,
			is_enabled INTEGER NOT NULL DEFAULT 1, sort_order INTEGER NOT NULL DEFAULT 0,
			path_prefix TEXT NOT NULL DEFAULT '', site_mode TEXT NOT NULL DEFAULT 'subdir',
			domain TEXT NOT NULL DEFAULT '', output_dir TEXT NOT NULL DEFAULT '');
		INSERT INTO gocms_language (id, code, name, is_default, is_fallback, sort_order, path_prefix, site_mode, domain, output_dir)
		VALUES (17, 'en', 'English', 1, 1, 5, '', 'subdir', '', ''),
		       (29, 'fr', 'Français', 0, 0, 40, 'french', 'standalone', 'fr.example.test', 'french-output')`); err != nil {
		t.Fatal(err)
	}
	if err := EnsureLanguages(ctx, database); err != nil {
		t.Fatal(err)
	}
	for _, siteID := range []int64{1, 2, 3} {
		var count int
		if err := database.QueryRow(`SELECT COUNT(*) FROM gocms_language WHERE site_id = ?`, siteID).Scan(&count); err != nil || count != 2 {
			t.Fatalf("site %d languages = %d, err = %v", siteID, count, err)
		}
		var id int64
		var adminDefault int
		if err := database.QueryRow(`SELECT id, is_admin_default FROM gocms_language WHERE site_id = ? AND code = 'en'`, siteID).Scan(&id, &adminDefault); err != nil || adminDefault != 1 {
			t.Fatalf("site %d admin default = %d, err = %v", siteID, adminDefault, err)
		}
		if (siteID == 2) != (id == 17) {
			t.Fatalf("original ID belongs to the default site: site %d, id %d", siteID, id)
		}
		var name, prefix, domain, output string
		var sortOrder int
		if err := database.QueryRow(`SELECT name, path_prefix, sort_order, domain, output_dir FROM gocms_language WHERE site_id = ? AND code = 'fr'`, siteID).Scan(&name, &prefix, &sortOrder, &domain, &output); err != nil {
			t.Fatal(err)
		}
		if name != "Français" || prefix != "french" || sortOrder != 40 {
			t.Fatalf("site %d lost configuration: %q %q %d", siteID, name, prefix, sortOrder)
		}
		if siteID == 2 {
			if domain != "fr.example.test" || output != "french-output" {
				t.Fatalf("default site lost standalone route: %q %q", domain, output)
			}
		} else if domain != "" || output != "" {
			t.Fatalf("site %d inherited another site's standalone route: %q %q", siteID, domain, output)
		}
	}
	if _, err := database.Exec(`UPDATE gocms_language SET name = '独立修改' WHERE site_id = 3 AND code = 'fr';
		DELETE FROM gocms_language WHERE site_id = 3 AND code = 'en';
		UPDATE gocms_language SET is_default = 1, is_admin_default = 1 WHERE site_id = 3 AND code = 'fr'`); err != nil {
		t.Fatal(err)
	}
	if err := EnsureLanguages(ctx, database); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM gocms_language WHERE site_id = 3 AND name = '独立修改'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("restart overwrote independent configuration: count %d, err %v", count, err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM gocms_language WHERE site_id = 3`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("restart copied deleted language: count %d, err %v", count, err)
	}
	if _, err := database.Exec(`INSERT INTO gocms_language (site_id, code, name) VALUES (3, 'fr', 'duplicate')`); err == nil {
		t.Fatal("duplicate language code within a site was accepted")
	}
}

func TestSiteLanguageLifecycleAndStandaloneHostOwnership(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	if err := CreateSchema(ctx, database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO gocms_language (code, name) VALUES ('es', 'Español')`); err != nil {
		t.Fatal(err)
	}
	site, err := CreateSite(ctx, database, &Site{Name: "独立站点", Code: "independent", Domain: "owner.example.test", ThemeID: "custom-theme"})
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM gocms_language WHERE site_id = ? AND code = 'zh-CN' AND is_default = 1 AND is_fallback = 1 AND is_admin_default = 1`, site.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("new site's initial language: count %d, err %v", count, err)
	}
	if _, err := database.Exec(`INSERT INTO gocms_language (site_id, code, name, site_mode, domain) VALUES (?, 'es', 'Español independiente', 'standalone', 'es.owner.example.test')`, site.ID); err != nil {
		t.Fatal(err)
	}
	resolved, err := GetSiteByHost(ctx, database, "es.owner.example.test")
	if err != nil || resolved.ID != site.ID || resolved.ThemeID != site.ThemeID || resolved.OutputDir != fmt.Sprintf("%d_es", site.ID) {
		t.Fatalf("standalone host resolved to wrong owner: %+v, err %v", resolved, err)
	}
	if err := DeleteSite(ctx, database, site.ID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM gocms_language WHERE site_id = ?`, site.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("deleted site's languages retained: count %d, err %v", count, err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM gocms_language WHERE site_id = 1`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("deleting site changed other languages: count %d, err %v", count, err)
	}
}

func TestEnsureLanguagesMigratesLegacyTable(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	// Legacy install: no is_admin_default column at all.
	if _, err := database.Exec(`
		CREATE TABLE "gocms_language" (
			"id" INTEGER PRIMARY KEY AUTOINCREMENT,
			"code" TEXT NOT NULL UNIQUE,
			"name" TEXT NOT NULL,
			"is_default" INTEGER NOT NULL DEFAULT 0,
			"is_fallback" INTEGER NOT NULL DEFAULT 0,
			"is_enabled" INTEGER NOT NULL DEFAULT 1,
			"sort_order" INTEGER NOT NULL DEFAULT 0,
			"path_prefix" TEXT NOT NULL DEFAULT ''
		)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO "gocms_language" ("code", "name", "is_default", "is_fallback", "is_enabled", "sort_order", "path_prefix")
		VALUES ('zh-CN', '简体中文', 1, 1, 1, 10, '')`); err != nil {
		t.Fatal(err)
	}

	if err := EnsureLanguages(context.Background(), database); err != nil {
		t.Fatal(err)
	}

	var adminDefaultCode string
	if err := database.QueryRow(`SELECT "code" FROM "gocms_language" WHERE "is_admin_default" = 1`).Scan(&adminDefaultCode); err != nil {
		t.Fatalf("expected backfilled admin display language: %v", err)
	}
	if adminDefaultCode != "zh-CN" {
		t.Fatalf("expected zh-CN backfilled as admin display language, got %q", adminDefaultCode)
	}
}

func TestEnsureLanguagesBackfillsFromPrimarySiteLanguage(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := CreateSchema(context.Background(), database); err != nil {
		t.Fatal(err)
	}

	// Simulate a legacy install where only the primary site language was marked
	// and the admin display language was implicitly the same language.
	if _, err := database.Exec(`UPDATE "gocms_language" SET "is_admin_default" = 0`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE "gocms_language" SET "is_default" = 0`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO "gocms_language" ("code", "name", "is_default", "is_fallback", "is_enabled", "sort_order", "path_prefix")
		VALUES ('en', 'English', 1, 0, 1, 20, '')`); err != nil {
		t.Fatal(err)
	}

	if err := EnsureLanguages(context.Background(), database); err != nil {
		t.Fatal(err)
	}

	var adminDefaultCode string
	if err := database.QueryRow(`SELECT "code" FROM "gocms_language" WHERE "is_admin_default" = 1`).Scan(&adminDefaultCode); err != nil {
		t.Fatalf("expected backfilled admin display language: %v", err)
	}
	if adminDefaultCode != "en" {
		t.Fatalf("expected en backfilled as admin display language, got %q", adminDefaultCode)
	}
}

func TestEnsureLanguagesKeepsConfiguredAdminDisplayLanguage(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := CreateSchema(context.Background(), database); err != nil {
		t.Fatal(err)
	}

	// zh-CN stays the primary site language while English is the admin display language.
	if _, err := database.Exec(`INSERT INTO "gocms_language" ("code", "name", "is_default", "is_fallback", "is_enabled", "sort_order", "path_prefix", "is_admin_default")
		VALUES ('en', 'English', 0, 0, 1, 20, 'en', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE "gocms_language" SET "is_admin_default" = 0 WHERE "code" = 'zh-CN'`); err != nil {
		t.Fatal(err)
	}

	if err := EnsureLanguages(context.Background(), database); err != nil {
		t.Fatal(err)
	}

	var adminDefaultCode string
	if err := database.QueryRow(`SELECT "code" FROM "gocms_language" WHERE "is_admin_default" = 1`).Scan(&adminDefaultCode); err != nil {
		t.Fatalf("expected admin display language: %v", err)
	}
	if adminDefaultCode != "en" {
		t.Fatalf("expected en to stay the admin display language, got %q", adminDefaultCode)
	}
}
