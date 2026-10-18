package db

import (
	"context"
	"testing"
)

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
