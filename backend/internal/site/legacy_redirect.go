package site

import (
	"context"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gocms/internal/db"
)

// legacyPrefixCacheTTL 是语言前缀列表的缓存时长。语言配置很少变动，但改动后
// 需要在一个短周期内生效，因此不做主动失效。
const legacyPrefixCacheTTL = time.Minute

type legacyPrefixCache struct {
	mu       sync.Mutex
	expires  time.Time
	prefixes []string
}

func newLegacyPrefixCache() *legacyPrefixCache {
	return &legacyPrefixCache{}
}

// legacyLanguagePrefixes 返回“不应出现在生成 URL 中”的语言路径前缀：
//   - 根语言（path_prefix 为空）的语言代码，例如 en 站点根路径服务，/en/... 是旧地址；
//   - 独立域名语言（site_mode = standalone）的语言代码与配置前缀。
//
// 子目录语言（site_mode = subdir 且 path_prefix 非空）的前缀是真实路径，不参与重定向。
func (s *Server) legacyLanguagePrefixes(ctx context.Context) []string {
	if s.database == nil || s.legacyPrefixes == nil {
		return nil
	}
	cache := s.legacyPrefixes
	cache.mu.Lock()
	if time.Now().Before(cache.expires) {
		prefixes := cache.prefixes
		cache.mu.Unlock()
		return prefixes
	}
	cache.mu.Unlock()

	rows, err := s.database.QueryContext(ctx, `
		SELECT "code", "path_prefix", "site_mode"
		FROM "`+db.LanguageTable+`"
		WHERE "is_enabled" = 1`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	seen := make(map[string]bool)
	prefixes := make([]string, 0, 8)
	add := func(value string) {
		value = strings.Trim(strings.TrimSpace(value), "/")
		if value == "" {
			return
		}
		key := strings.ToLower(value)
		if seen[key] {
			return
		}
		seen[key] = true
		prefixes = append(prefixes, value)
	}
	for rows.Next() {
		var code, prefix, mode string
		if err := rows.Scan(&code, &prefix, &mode); err != nil {
			return nil
		}
		if mode == db.LanguageSiteModeSubdir && strings.Trim(strings.TrimSpace(prefix), "/") != "" {
			continue
		}
		add(code)
		add(prefix)
	}
	if err := rows.Err(); err != nil {
		return nil
	}

	cache.mu.Lock()
	cache.prefixes = prefixes
	cache.expires = time.Now().Add(legacyPrefixCacheTTL)
	cache.mu.Unlock()
	return prefixes
}

func (s *Server) hasLegacyLanguagePrefix(ctx context.Context, segment string) bool {
	for _, prefix := range s.legacyLanguagePrefixes(ctx) {
		if strings.EqualFold(prefix, segment) {
			return true
		}
	}
	return false
}

// siteServingDirs 列出该站点可能提供静态文件的目录，顺序与 staticFile 一致。
func (s *Server) siteServingDirs(site *db.Site) []string {
	if site == nil {
		return nil
	}
	dirs := make([]string, 0, 2)
	siteDir := site.OutputDir
	if siteDir == "" && site.ID > 0 {
		siteDir = strconv.FormatInt(site.ID, 10)
	}
	if siteDir != "" {
		if !filepath.IsAbs(siteDir) {
			primaryDir := filepath.Join(s.siteRoot, siteDir)
			if _, err := os.Stat(primaryDir); os.IsNotExist(err) && site.OutputDir == "" {
				legacySitesDir := filepath.Join(s.siteRoot, "sites", siteDir)
				if stat, err := os.Stat(legacySitesDir); err == nil && stat.IsDir() {
					primaryDir = legacySitesDir
				}
			}
			siteDir = primaryDir
		}
		dirs = append(dirs, siteDir)
	}
	// 默认站点在自身目录找不到文件时会回落到站点根目录，判断是否存在需要一并考虑。
	if site.IsDefault || site.ID <= 1 {
		dirs = append(dirs, s.siteRoot)
	}
	return dirs
}

// pathServed 判断某个 URL 路径在候选目录中是否存在可提供的文件，规则与
// staticFile 保持一致：文件直接命中，目录需要包含 index.html。
func (s *Server) pathServed(dirs []string, urlPath string) bool {
	relative := strings.Trim(path.Clean("/"+strings.ReplaceAll(urlPath, `\`, "/")), "/")
	if relative == "" || relative == "." {
		relative = "index.html"
	}
	for _, dir := range dirs {
		target := filepath.Join(dir, filepath.FromSlash(relative))
		stat, err := os.Stat(target)
		if err != nil {
			continue
		}
		if stat.IsDir() {
			if fileExistsInDir(target, "index.html") {
				return true
			}
			continue
		}
		return true
	}
	return false
}

// legacyLanguagePrefixRedirect 处理旧语言前缀地址：当请求路径不是该站点可提供的
// 页面，而“去掉语言前缀后的路径”可以命中时，永久重定向到后者。
// 只有在原地址本身不可用、且目标存在时才重定向，因此不会影响任何正常 URL。
func (s *Server) legacyLanguagePrefixRedirect(response http.ResponseWriter, request *http.Request, site *db.Site) bool {
	if s.database == nil || site == nil {
		return false
	}
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		return false
	}
	rawPath := request.URL.Path
	if rawPath == "" || rawPath == "/" {
		return false
	}
	dirs := s.siteServingDirs(site)
	if len(dirs) == 0 || s.pathServed(dirs, rawPath) {
		return false
	}
	segments := strings.SplitN(strings.TrimPrefix(rawPath, "/"), "/", 2)
	if segments[0] == "" || !s.hasLegacyLanguagePrefix(request.Context(), segments[0]) {
		return false
	}
	target := "/"
	if len(segments) == 2 {
		target = "/" + segments[1]
	}
	if !s.pathServed(dirs, target) {
		return false
	}
	if request.URL.RawQuery != "" {
		target += "?" + request.URL.RawQuery
	}
	http.Redirect(response, request, target, http.StatusMovedPermanently)
	return true
}
