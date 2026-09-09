package site

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalImageURL(t *testing.T) {
	for _, test := range []struct {
		input, expected string
	}{
		{"/UploadFile/produppic/a.jpg", "/images/a.jpg"},
		{"http://www.example.com/UploadFile/a.jpg?size=small", "/images/a.jpg?size=small"},
		{"https://img05.jdzj.com/oledit/UploadFile/news2015a/a.jpg", "https://img05.jdzj.com/oledit/UploadFile/news2015a/a.jpg"},
		{"/images/a.jpg", "/images/a.jpg"},
	} {
		if actual := canonicalImageURL(test.input, "www.example.com"); actual != test.expected {
			t.Errorf("canonicalImageURL(%q) = %q, want %q", test.input, actual, test.expected)
		}
	}
}

func TestSeparateResources(t *testing.T) {
	dir := t.TempDir()
	assets, web := filepath.Join(dir, "assets"), filepath.Join(dir, "web")
	theme := filepath.Join(assets, "theme", "blue")
	for _, p := range []string{filepath.Join(assets, "images"), filepath.Join(theme, "images"), filepath.Join(theme, "css"), filepath.Join(theme, "skin"), web} {
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for p, body := range map[string]string{filepath.Join(assets, "images/product.jpg"): "product", filepath.Join(theme, "images/logo.svg"): "logo", filepath.Join(theme, "css/site.css"): "css", filepath.Join(theme, "skin/css.css"): "skin", filepath.Join(web, "index.html"): "home", filepath.Join(dir, "secret.txt"): "secret"} {
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "secret.txt"), filepath.Join(assets, "escape.txt")); err != nil {
		t.Fatal(err)
	}
	s, err := New(nil, web)
	if err != nil {
		t.Fatal(err)
	}
	s.assetsRoot = assets
	s.themeRoot = theme
	for _, tc := range []struct {
		path   string
		status int
		body   string
	}{
		{"/", 200, "home"}, {"/images/logo.svg", 200, "logo"}, {"/images/product.jpg", 200, "product"}, {"/css/site.css", 200, "css"}, {"/skin/css.css", 200, "skin"},
		{"/images/", 404, ""}, {"/uploadfile/product.jpg", 404, ""}, {"/UploadFile/product.jpg", 404, ""},
		{"/images/../secret.txt", 404, ""}, {"/escape.txt", 404, ""}, {"/data/site.db", 404, ""},
	} {
		t.Run(tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
			if w.Code != tc.status || tc.body != "" && w.Body.String() != tc.body {
				t.Fatalf("got %d %q", w.Code, w.Body.String())
			}
		})
	}
}
