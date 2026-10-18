package site

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestServeSiteNotFoundPage 验证未命中的路径会返回站点自己生成的 404 页面，
// 语言目录优先，API 路径与缺少 404 模板的站点保持纯文本响应。
func TestServeSiteNotFoundPage(t *testing.T) {
	server, _, root := newLegacyRedirectTestServer(t)
	writePage := func(name, body string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writePage("404.html", "<html>root-404-page</html>")
	writePage("es/404.html", "<html>es-404-page</html>")

	response := legacyRedirectRequest(t, server, http.MethodGet, "example.test", "/missing/page")
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing path status = %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), "root-404-page") {
		t.Fatalf("missing path body = %q", response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("missing path content type = %q", contentType)
	}

	// 语言目录下的未命中路径优先使用该语言的 404 页面。
	response = legacyRedirectRequest(t, server, http.MethodGet, "example.test", "/es/missing")
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "es-404-page") {
		t.Fatalf("language missing path = %d %q", response.Code, response.Body.String())
	}

	// 未配置 404 页面的语言目录回落到站点根 404 页面。
	response = legacyRedirectRequest(t, server, http.MethodGet, "example.test", "/ru/missing")
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "root-404-page") {
		t.Fatalf("fallback missing path = %d %q", response.Code, response.Body.String())
	}

	// API 路径不应返回 HTML 页面。
	response = legacyRedirectRequest(t, server, http.MethodGet, "example.test", "/api/no-such-endpoint")
	if response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "root-404-page") {
		t.Fatalf("api missing path = %d %q", response.Code, response.Body.String())
	}

	// 站点没有 404 页面时保持原有纯文本响应。
	if err := os.Remove(filepath.Join(root, "404.html")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "es", "404.html")); err != nil {
		t.Fatal(err)
	}
	response = legacyRedirectRequest(t, server, http.MethodGet, "example.test", "/missing/page")
	if response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "root-404-page") {
		t.Fatalf("plain missing path = %d %q", response.Code, response.Body.String())
	}
}
