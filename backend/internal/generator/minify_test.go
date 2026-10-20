package generator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHTMLMinifyEnabled(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"", true},
		{"1", true},
		{"true", true},
		{"0", false},
		{"false", false},
		{"off", false},
		{" OFF ", false},
	}
	for _, tc := range cases {
		settings := map[string]string{}
		if tc.value != "" {
			settings["html_minify"] = tc.value
		}
		if got := htmlMinifyEnabled(settings); got != tc.want {
			t.Errorf("htmlMinifyEnabled(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

func TestMinifyHTMLDocumentPreservesRawBlocks(t *testing.T) {
	input := []byte(`<!DOCTYPE html>
<html>
<head>
  <!-- remove me -->
  <style>  .a { color: red; }  </style>
</head>
<body>
  <div class="note">  hello   world  </div>
  <pre>  line 1
    line 2  </pre>
  <textarea>  a
  b  </textarea>
  <script>  var  x = 1;  </script>
</body>
</html>`)

	output, err := minifyHTMLDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	body := string(output)
	if len(output) >= len(input) {
		t.Fatalf("minified size %d is not smaller than input %d", len(output), len(input))
	}
	if strings.Contains(body, "remove me") {
		t.Fatalf("comment was not removed: %s", body)
	}
	if !strings.Contains(body, "hello world") {
		t.Fatalf("text whitespace was not collapsed: %s", body)
	}
	for _, raw := range []string{
		"<pre>  line 1\n    line 2  </pre>",
		"<textarea>  a\n  b  </textarea>",
		"<script>  var  x = 1;  </script>",
		"<style>  .a { color: red; }  </style>",
	} {
		if !strings.Contains(body, raw) {
			t.Fatalf("raw block was modified, missing %q in %s", raw, body)
		}
	}
}

func TestPublishPagesMinifiesOnlyHTML(t *testing.T) {
	pages := map[string][]byte{
		"index.html": []byte("<!DOCTYPE html>\n<html>\n  <body>\n    <!-- comment -->\n    <p>  hello   world  </p>\n  </body>\n</html>"),
		"sitemap.xml": []byte("<?xml version=\"1.0\"?>\n<urlset>  <url>  </url>  </urlset>"),
	}

	minifiedDir := filepath.Join(t.TempDir(), "minified")
	if err := publishPagesToDirectory(context.Background(), minifiedDir, pages, true); err != nil {
		t.Fatal(err)
	}
	html, err := os.ReadFile(filepath.Join(minifiedDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(html), "comment") || !strings.Contains(string(html), "hello world") {
		t.Fatalf("HTML was not minified: %s", html)
	}
	xml, err := os.ReadFile(filepath.Join(minifiedDir, "sitemap.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(xml) != string(pages["sitemap.xml"]) {
		t.Fatalf("XML should not be minified: %s", xml)
	}

	plainDir := filepath.Join(t.TempDir(), "plain")
	if err := publishPagesToDirectory(context.Background(), plainDir, pages, false); err != nil {
		t.Fatal(err)
	}
	plain, err := os.ReadFile(filepath.Join(plainDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != string(pages["index.html"]) {
		t.Fatalf("HTML should be unchanged when minify is disabled: %s", plain)
	}
}
