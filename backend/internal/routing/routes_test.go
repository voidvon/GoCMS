package routing

import "testing"

func TestCategoryRouteDefaults(t *testing.T) {
	if got := DefaultListPath(); got != "category" {
		t.Fatalf("root list path = %q", got)
	}
	filename, err := RenderDetailFilename(DefaultDetailPattern, 185)
	if err != nil || filename != "185.html" {
		t.Fatalf("detail filename = %q, err=%v", filename, err)
	}
	base, err := RenderListFilename(DefaultListPattern, 39, 1)
	if err != nil || base != "39.html" {
		t.Fatalf("list filename = %q, err=%v", base, err)
	}
	page, err := RenderListPageFilename(DefaultListPattern, 39, 2)
	if err != nil || page != "39-2.html" {
		t.Fatalf("paged list filename = %q, err=%v", page, err)
	}
}

func TestRouteValidation(t *testing.T) {
	if _, err := NormalizeDirectory("../outside"); err == nil {
		t.Fatal("parent directory accepted")
	}
	if _, err := NormalizeFilePattern("fixed.html", false); err == nil {
		t.Fatal("pattern without id accepted")
	}
	if _, err := NormalizeFilePattern("{id}-{page}.html", false); err == nil {
		t.Fatal("detail pattern accepted page placeholder")
	}
	if got, err := NormalizeDirectory("/catalog/"); err != nil || got != "catalog" {
		t.Fatalf("normalized directory = %q, err=%v", got, err)
	}
	if got, err := NormalizeOptionalDirectory("/"); err != nil || got != "" {
		t.Fatalf("optional root directory = %q, err=%v", got, err)
	}
	if got, err := NormalizeCoverFilePattern("landing.html"); err != nil || got != "landing.html" {
		t.Fatalf("cover filename = %q, err=%v", got, err)
	}
	if got, err := NormalizeListFilePattern("index.html"); err != nil || got != "index.html" {
		t.Fatalf("list index filename = %q, err=%v", got, err)
	}
	if got, err := NormalizeListFilePattern("{id}.html"); err != nil || got != "{id}.html" {
		t.Fatalf("list id filename = %q, err=%v", got, err)
	}
	if _, err := NormalizeListFilePattern(""); err == nil {
		t.Fatal("empty list filename accepted")
	}
	if _, err := NormalizeListFilePattern("contact.html"); err == nil {
		t.Fatal("fixed non-index list filename accepted")
	}
	if got, err := NormalizeListFilePattern("INDEX.HTML"); err != nil || got != "INDEX.HTML" {
		t.Fatalf("upper case index filename = %q, err=%v", got, err)
	}
	if _, err := NormalizeListFilePattern("index-{page}.html"); err == nil {
		t.Fatal("list filename accepted page placeholder")
	}
	if got, err := RenderListFilename("index.html", 131, 1); err != nil || got != "index.html" {
		t.Fatalf("index list page 1 = %q, err=%v", got, err)
	}
	if got, err := RenderListPageFilename("index.html", 131, 2); err != nil || got != "index-2.html" {
		t.Fatalf("index list page 2 = %q, err=%v", got, err)
	}
	if got, err := RenderCoverFilename("landing.html", 25); err != nil || got != "landing.html" {
		t.Fatalf("cover route filename = %q, err=%v", got, err)
	}
	if got, err := RenderCoverFilename("{id}.html", 25); err != nil || got != "25.html" {
		t.Fatalf("cover id filename = %q, err=%v", got, err)
	}
}
