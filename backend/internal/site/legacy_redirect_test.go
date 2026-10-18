package site

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"gocms/internal/db"
)

// newLegacyRedirectTestServer 准备一个默认站点（example.test）+ 多语言站点目录，
// 用于验证旧语言前缀重定向。
func newLegacyRedirectTestServer(t *testing.T) (*Server, *sql.DB, string) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSchema(context.Background(), database); err != nil {
		database.Close()
		t.Fatal(err)
	}
	root := t.TempDir()
	server, err := New(database, root)
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	// 默认语言改为 en（根路径服务），并补一个子目录语言与两个独立域名语言。
	statements := []string{
		`UPDATE "gocms_site" SET "domain" = 'example.test' WHERE "is_default" = 1`,
		`UPDATE "gocms_language" SET "code" = 'en', "name" = 'English', "is_default" = 1, "is_fallback" = 1, "path_prefix" = '', "site_mode" = 'subdir' WHERE "code" = 'zh-CN'`,
		`INSERT INTO "gocms_language" ("code", "name", "is_default", "is_fallback", "is_enabled", "sort_order", "path_prefix", "site_mode", "domain", "output_dir") VALUES ('es', 'Español', 0, 0, 1, 20, 'es', 'subdir', '', '')`,
		`INSERT INTO "gocms_language" ("code", "name", "is_default", "is_fallback", "is_enabled", "sort_order", "path_prefix", "site_mode", "domain", "output_dir") VALUES ('zh-CN', '简体中文', 0, 0, 1, 30, '', 'standalone', 'cn.example.test', 'cn-out')`,
		`INSERT INTO "gocms_language" ("code", "name", "is_default", "is_fallback", "is_enabled", "sort_order", "path_prefix", "site_mode", "domain", "output_dir") VALUES ('ru', 'Русский', 0, 0, 1, 40, 'ru', 'standalone', 'ru.example.test', 'ru')`,
		`INSERT INTO "gocms_language" ("code", "name", "is_default", "is_fallback", "is_enabled", "sort_order", "path_prefix", "site_mode", "domain", "output_dir") VALUES ('de', 'Deutsch', 0, 0, 0, 50, 'de', 'subdir', '', '')`,
	}
	for _, statement := range statements {
		if _, err := database.Exec(statement); err != nil {
			t.Fatalf("seed %q: %v", statement, err)
		}
	}

	files := []string{
		"index.html",
		"products/a/index.html",
		"es/products/a/index.html",
		"cn-out/products/a/index.html",
		"en/real/index.html",
	}
	for _, name := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("<html>"+name+"</html>"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return server, database, root
}

func legacyRedirectRequest(t *testing.T, server *Server, method, host, target string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, nil)
	request.Host = host
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

func TestLegacyLanguagePrefixRedirect(t *testing.T) {
	server, _, _ := newLegacyRedirectTestServer(t)

	cases := []struct {
		name     string
		method   string
		host     string
		target   string
		status   int
		location string
	}{
		{name: "根语言前缀重定向", method: http.MethodGet, host: "example.test", target: "/en/products/a/", status: http.StatusMovedPermanently, location: "/products/a/"},
		{name: "保留查询串", method: http.MethodGet, host: "example.test", target: "/en/products/a/?from=old&x=1", status: http.StatusMovedPermanently, location: "/products/a/?from=old&x=1"},
		{name: "无尾斜杠也重定向", method: http.MethodGet, host: "example.test", target: "/en/products/a", status: http.StatusMovedPermanently, location: "/products/a"},
		{name: "前缀指向根路径", method: http.MethodGet, host: "example.test", target: "/en/", status: http.StatusMovedPermanently, location: "/"},
		{name: "目标不存在时保持 404", method: http.MethodGet, host: "example.test", target: "/en/missing/", status: http.StatusNotFound},
		{name: "正常页面不受影响", method: http.MethodGet, host: "example.test", target: "/products/a/", status: http.StatusOK},
		{name: "真实存在的前缀目录优先", method: http.MethodGet, host: "example.test", target: "/en/real/", status: http.StatusOK},
		{name: "子目录语言前缀不改写", method: http.MethodGet, host: "example.test", target: "/es/products/a/", status: http.StatusOK},
		{name: "未启用的语言前缀不参与", method: http.MethodGet, host: "example.test", target: "/de/products/a/", status: http.StatusNotFound},
		{name: "独立域名语言代码可重定向", method: http.MethodGet, host: "example.test", target: "/zh-CN/products/a/", status: http.StatusMovedPermanently, location: "/products/a/"},
		{name: "独立域名语言前缀可重定向", method: http.MethodGet, host: "example.test", target: "/ru/products/a/", status: http.StatusMovedPermanently, location: "/products/a/"},
		{name: "独立域名站点自身前缀", method: http.MethodGet, host: "cn.example.test", target: "/en/products/a/", status: http.StatusMovedPermanently, location: "/products/a/"},
		{name: "非 GET 不重定向", method: http.MethodPost, host: "example.test", target: "/en/products/a/", status: http.StatusNotFound},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response := legacyRedirectRequest(t, server, testCase.method, testCase.host, testCase.target)
			if response.Code != testCase.status {
				t.Fatalf("%s %s -> %d, 期望 %d", testCase.method, testCase.target, response.Code, testCase.status)
			}
			if testCase.location != "" {
				if location := response.Header().Get("Location"); location != testCase.location {
					t.Fatalf("Location = %q, 期望 %q", location, testCase.location)
				}
			}
		})
	}
}

func TestLegacyLanguagePrefixesSkipsSubdirectoryLanguages(t *testing.T) {
	server, _, _ := newLegacyRedirectTestServer(t)
	prefixes := server.legacyLanguagePrefixes(context.Background())
	contains := func(value string) bool {
		for _, prefix := range prefixes {
			if prefix == value {
				return true
			}
		}
		return false
	}
	for _, want := range []string{"en", "zh-CN", "ru"} {
		if !contains(want) {
			t.Fatalf("前缀列表缺少 %q：%v", want, prefixes)
		}
	}
	for _, unwanted := range []string{"es", "de"} {
		if contains(unwanted) {
			t.Fatalf("子目录/停用语言 %q 不应出现在前缀列表：%v", unwanted, prefixes)
		}
	}
}

func TestLegacyLanguagePrefixRedirectWithoutDatabase(t *testing.T) {
	server, err := New(nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	response := legacyRedirectRequest(t, server, http.MethodGet, "example.test", "/en/products/a/")
	if response.Code != http.StatusNotFound {
		t.Fatalf("无数据库时应保持 404，得到 %d", response.Code)
	}
}
