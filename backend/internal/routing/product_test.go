package routing

import "testing"

func TestProductRouteDefaults(t *testing.T) {
	if got := DefaultListPath(0); got != "valve" {
		t.Fatalf("root list path = %q", got)
	}
	if got := DefaultListPath(25); got != "Products" {
		t.Fatalf("child list path = %q", got)
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
	if got, err := NormalizeDirectory("/Products/"); err != nil || got != "Products" {
		t.Fatalf("normalized directory = %q, err=%v", got, err)
	}
}
