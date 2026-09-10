package site

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSeparateResources(t *testing.T) {
	dir := t.TempDir()
	assets := filepath.Join(dir, "assets")
	web := filepath.Join(dir, "web")
	theme := filepath.Join(assets, "theme", "blue", "assets")
	for _, path := range []string{
		filepath.Join(assets, "images"), filepath.Join(theme, "images"), filepath.Join(theme, "css"),
		filepath.Join(theme, "skin"), web,
	} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range map[string]string{
		filepath.Join(assets, "images/product.jpg"): "product",
		filepath.Join(theme, "images/logo.svg"):     "logo",
		filepath.Join(theme, "css/site.css"):        "css",
		filepath.Join(theme, "skin/css.css"):        "skin",
		filepath.Join(theme, "private.txt"):         "private",
		filepath.Join(web, "index.html"):            "home",
		filepath.Join(dir, "secret.txt"):            "secret",
	} {
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "secret.txt"), filepath.Join(assets, "escape.txt")); err != nil {
		t.Fatal(err)
	}

	server, err := New(nil, web)
	if err != nil {
		t.Fatal(err)
	}
	server.assetsRoot = assets
	server.themeRoot = theme
	for _, test := range []struct {
		path   string
		status int
		body   string
	}{
		{path: "/", status: 200, body: "home"},
		{path: "/images/logo.svg", status: 200, body: "logo"},
		{path: "/images/product.jpg", status: 200, body: "product"},
		{path: "/css/site.css", status: 200, body: "css"},
		{path: "/skin/css.css", status: 200, body: "skin"},
		{path: "/images/", status: 404},
		{path: "/assets/theme", status: 404},
		{path: "/assets/theme/blue/assets/css/site.css", status: 404},
		{path: "/assets/theme/blue/private.txt", status: 404},
		{path: "/unknown-resource/product.jpg", status: 404},
		{path: "/images/../secret.txt", status: 404},
		{path: "/escape.txt", status: 404},
		{path: "/data/site.db", status: 404},
	} {
		t.Run(test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, httptest.NewRequest("GET", test.path, nil))
			if response.Code != test.status || test.body != "" && response.Body.String() != test.body {
				t.Fatalf("got %d %q", response.Code, response.Body.String())
			}
		})
	}
}
