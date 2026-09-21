package db

import (
	"context"
	"testing"
)

func TestEnsureSitesCreatesDefaultSite(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := CreateSchema(context.Background(), database); err != nil {
		t.Fatal(err)
	}

	site, err := GetDefaultSite(context.Background(), database)
	if err != nil {
		t.Fatalf("GetDefaultSite failed: %v", err)
	}
	if site.ID != 1 {
		t.Errorf("expected site ID 1, got %d", site.ID)
	}
	if site.Code != "default" {
		t.Errorf("expected code 'default', got %q", site.Code)
	}
	if !site.IsDefault {
		t.Errorf("expected is_default=true")
	}
	if site.Status != "active" {
		t.Errorf("expected status 'active', got %q", site.Status)
	}
}

func TestCreateSiteAndUniqueCode(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := CreateSchema(context.Background(), database); err != nil {
		t.Fatal(err)
	}

	// Invalid code test
	_, err = CreateSite(context.Background(), database, &Site{
		Name: "Test Site",
		Code: "Invalid_Code!",
	})
	if err == nil {
		t.Errorf("expected error for invalid code, got nil")
	}

	// Valid creation
	created, err := CreateSite(context.Background(), database, &Site{
		Name:    "技术博客",
		Code:    "tech-blog",
		Domain:  "blog.example.com",
		Aliases: []string{"tech.example.com", "m.blog.example.com"},
		ThemeID: "clean-blog",
	})
	if err != nil {
		t.Fatalf("CreateSite failed: %v", err)
	}
	if created.ID <= 1 {
		t.Errorf("expected site ID > 1, got %d", created.ID)
	}

	// Duplicate code should fail
	_, err = CreateSite(context.Background(), database, &Site{
		Name: "Another Blog",
		Code: "tech-blog",
	})
	if err == nil {
		t.Errorf("expected duplicate code error, got nil")
	}

	// Query by code
	found, err := GetSiteByCode(context.Background(), database, "tech-blog")
	if err != nil {
		t.Fatalf("GetSiteByCode failed: %v", err)
	}
	if found.Name != "技术博客" {
		t.Errorf("expected name '技术博客', got %q", found.Name)
	}
	if len(found.Aliases) != 2 {
		t.Errorf("expected 2 aliases, got %d", len(found.Aliases))
	}
}

func TestGetSiteByHostAndAliases(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := CreateSchema(context.Background(), database); err != nil {
		t.Fatal(err)
	}

	site1, err := CreateSite(context.Background(), database, &Site{
		Name:    "商城站",
		Code:    "shop",
		Domain:  "shop.mysite.com",
		Aliases: []string{"mall.mysite.com", "buy.mysite.com"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Direct domain match
	matched, err := GetSiteByHost(context.Background(), database, "shop.mysite.com")
	if err != nil || matched.ID != site1.ID {
		t.Errorf("expected match site1 by domain, got %v, err: %v", matched, err)
	}

	// 2. With port
	matched, err = GetSiteByHost(context.Background(), database, "shop.mysite.com:8080")
	if err != nil || matched.ID != site1.ID {
		t.Errorf("expected match site1 by domain with port, got %v, err: %v", matched, err)
	}

	// 3. Match alias
	matched, err = GetSiteByHost(context.Background(), database, "mall.mysite.com")
	if err != nil || matched.ID != site1.ID {
		t.Errorf("expected match site1 by alias, got %v, err: %v", matched, err)
	}

	// 4. Empty host falls back to default site
	matched, err = GetSiteByHost(context.Background(), database, "")
	if err != nil || matched.ID != 1 {
		t.Errorf("expected match default site 1 for empty host, got %v, err: %v", matched, err)
	}
}

func TestUpdateSiteAndDefaultSwitch(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := CreateSchema(context.Background(), database); err != nil {
		t.Fatal(err)
	}

	site2, err := CreateSite(context.Background(), database, &Site{
		Name: "Site Two",
		Code: "site-two",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Make site2 default
	site2.IsDefault = true
	site2.Name = "Site Two Updated"
	if err := UpdateSite(context.Background(), database, site2); err != nil {
		t.Fatalf("UpdateSite failed: %v", err)
	}

	def, err := GetDefaultSite(context.Background(), database)
	if err != nil {
		t.Fatal(err)
	}
	if def.ID != site2.ID {
		t.Errorf("expected default site ID %d, got %d", site2.ID, def.ID)
	}

	// Old default should now have is_default=0
	oldDef, err := GetSiteByID(context.Background(), database, 1)
	if err != nil {
		t.Fatal(err)
	}
	if oldDef.IsDefault {
		t.Errorf("expected old default site is_default=false")
	}
}

func TestDeleteSiteProtection(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := CreateSchema(context.Background(), database); err != nil {
		t.Fatal(err)
	}

	// Cannot delete site 1
	if err := DeleteSite(context.Background(), database, 1); err == nil {
		t.Errorf("expected error deleting site 1, got nil")
	}

	// Create site 2 and delete it
	site2, err := CreateSite(context.Background(), database, &Site{
		Name: "Temp Site",
		Code: "temp-site",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := DeleteSite(context.Background(), database, site2.ID); err != nil {
		t.Fatalf("failed to delete site2: %v", err)
	}

	_, err = GetSiteByID(context.Background(), database, site2.ID)
	if err == nil {
		t.Errorf("expected site2 to be deleted, found record")
	}
}

func TestSiteSettingsScoping(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := CreateSchema(context.Background(), database); err != nil {
		t.Fatal(err)
	}

	// Save settings for site 1
	if err := SaveSiteSettingsForSite(context.Background(), database, 1, map[string]string{
		"site_name": "站点1名称",
		"site_url":  "https://site1.com",
	}); err != nil {
		t.Fatal(err)
	}

	// Save settings for site 2
	if err := SaveSiteSettingsForSite(context.Background(), database, 2, map[string]string{
		"site_name": "站点2名称",
		"site_url":  "https://site2.com",
	}); err != nil {
		t.Fatal(err)
	}

	s1Settings, err := LoadSiteSettingsForSite(context.Background(), database, 1)
	if err != nil {
		t.Fatal(err)
	}
	if s1Settings["site_name"] != "站点1名称" {
		t.Errorf("expected site 1 site_name '站点1名称', got %q", s1Settings["site_name"])
	}

	s2Settings, err := LoadSiteSettingsForSite(context.Background(), database, 2)
	if err != nil {
		t.Fatal(err)
	}
	if s2Settings["site_name"] != "站点2名称" {
		t.Errorf("expected site 2 site_name '站点2名称', got %q", s2Settings["site_name"])
	}
}
