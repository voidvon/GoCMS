package site

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gocms/internal/assetmin"
	"gocms/internal/db"
)

func (s *Server) resourceRoots(r *http.Request) ([]string, string, int64, bool) {
	themeRoot, _ := s.themePaths()
	siteID, _ := s.resolvePublicSiteID(r)
	var themeID string
	if s.database != nil && r != nil {
		if targetSite, err := db.GetSiteByID(r.Context(), s.database, siteID); err == nil && targetSite != nil {
			themeID = targetSite.ThemeID
		}
	}
	siteIDStr := strconv.FormatInt(siteID, 10)

	if themeID != "" && s.assetsRoot != "" {
		siteThemeAssets := filepath.Join(s.assetsRoot, siteIDStr, "themes", themeID, "assets")
		if stat, err := os.Stat(siteThemeAssets); err == nil && stat.IsDir() {
			themeRoot = siteThemeAssets
		} else {
			fallbackThemeAssets := filepath.Join(s.assetsRoot, "1", "themes", themeID, "assets")
			if stat, err := os.Stat(fallbackThemeAssets); err == nil && stat.IsDir() {
				themeRoot = fallbackThemeAssets
			}
		}
	} else if s.themeBase != "" && themeID != "" {
		siteThemeAssets := filepath.Join(s.themeBase, themeID, "assets")
		if stat, err := os.Stat(siteThemeAssets); err == nil && stat.IsDir() {
			themeRoot = siteThemeAssets
		}
	}

	// Standard web-root assets (favicon, touch icon, logo) live directly in the
	// theme asset root and are served at "/" rather than under /images/.
	rootName := strings.TrimPrefix(strings.ToLower(r.URL.Path), "/")
	if rootName != "" && !strings.Contains(rootName, "/") && isPublicRootImage(rootName) {
		if themeRoot == "" {
			return nil, "", siteID, false
		}
		return []string{themeRoot}, rootName, siteID, true
	}

	themeRoots := func(directory string) []string {
		if themeRoot == "" {
			return nil
		}
		return []string{filepath.Join(themeRoot, directory)}
	}
	for _, route := range []struct {
		prefix string
		roots  func() []string
	}{
		{
			prefix: "/assets/",
			roots: func() []string {
				if s.assetsRoot != "" {
					return []string{s.assetsRoot}
				}
				return nil
			},
		},
		{
			prefix: "/uploads/",
			roots: func() []string {
				roots := make([]string, 0, 2)
				if s.assetsRoot != "" {
					roots = append(roots, filepath.Join(s.assetsRoot, siteIDStr, "uploads"))
					if siteID != 1 {
						roots = append(roots, filepath.Join(s.assetsRoot, "1", "uploads"))
					}
				}
				return roots
			},
		},
		{
			prefix: "/images/",
			roots: func() []string {
				roots := make([]string, 0, 4)
				if s.assetsRoot != "" {
					roots = append(roots, filepath.Join(s.assetsRoot, siteIDStr, "images"))
					roots = append(roots, filepath.Join(s.assetsRoot, "1", "images"))
					roots = append(roots, filepath.Join(s.assetsRoot, "images"))
				}
				if themeRoot != "" {
					roots = append(roots, filepath.Join(themeRoot, "images"))
				}
				return roots
			},
		},
		{prefix: "/css/", roots: func() []string { return themeRoots("css") }},
		{prefix: "/js/", roots: func() []string { return themeRoots("js") }},
		{prefix: "/skin/", roots: func() []string { return themeRoots("skin") }},
	} {
		requestPath := r.URL.Path
		lower := strings.ToLower(requestPath)
		base := strings.TrimSuffix(route.prefix, "/")
		if lower == base {
			return route.roots(), "", siteID, true
		}
		if strings.HasPrefix(lower, route.prefix) {
			rel := requestPath[len(route.prefix):]
			if route.prefix == "/assets/" {
				cleanRel := path.Clean("/" + strings.ReplaceAll(rel, `\`, "/"))
				lowerRel := strings.ToLower(cleanRel)
				if strings.HasPrefix(lowerRel, "/theme/") || strings.HasPrefix(lowerRel, "/themes/") ||
					strings.Contains(lowerRel, "/themes/") || strings.Contains(lowerRel, "/theme/") ||
					strings.HasSuffix(lowerRel, ".html") || strings.HasSuffix(lowerRel, ".json") {
					return nil, "", siteID, false
				}
			}
			return route.roots(), rel, siteID, true
		}
	}
	return nil, "", siteID, false
}

type cachedMinifiedAsset struct {
	modTime time.Time
	size    int64
	data    []byte
}

var minifiedAssetCache sync.Map

// Files are opened inside the configured resource roots, without allowing symlink escapes.
func (s *Server) serveResource(w http.ResponseWriter, r *http.Request) bool {
	roots, rel, siteID, ok := s.resourceRoots(r)
	if !ok {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w)
		return true
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		f, err := os.OpenInRoot(root, rel)
		if err != nil {
			continue
		}
		info, err := f.Stat()
		if err == nil && info.Mode().IsRegular() {
			switch strings.ToLower(filepath.Ext(rel)) {
			case ".svg":
				w.Header().Set("Content-Type", "image/svg+xml")
				w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
				w.Header().Set("X-Content-Type-Options", "nosniff")
			case ".ico":
				w.Header().Set("Content-Type", "image/vnd.microsoft.icon")
				w.Header().Set("X-Content-Type-Options", "nosniff")
			}
			if data, ok := s.minifiedAsset(r.Context(), siteID, root, rel, r.URL.Path, f, info); ok {
				http.ServeContent(w, r, info.Name(), info.ModTime(), bytes.NewReader(data))
			} else {
				_, _ = f.Seek(0, io.SeekStart)
				http.ServeContent(w, r, info.Name(), info.ModTime(), f)
			}
			_ = f.Close()
			return true
		}
		_ = f.Close()
	}
	http.NotFound(w, r)
	return true
}

func (s *Server) minifiedAsset(ctx context.Context, siteID int64, root, rel, requestPath string, file *os.File, info os.FileInfo) ([]byte, bool) {
	ext := strings.ToLower(filepath.Ext(rel))
	if ext != ".css" && ext != ".js" {
		return nil, false
	}
	requestPath = strings.ToLower(requestPath)
	if !strings.HasPrefix(requestPath, "/css/") && !strings.HasPrefix(requestPath, "/js/") && !strings.HasPrefix(requestPath, "/skin/") {
		return nil, false
	}
	if !s.assetMinifyEnabled(ctx, siteID) {
		return nil, false
	}
	cacheKey := root + "\x00" + rel
	if cached, ok := minifiedAssetCache.Load(cacheKey); ok {
		entry := cached.(cachedMinifiedAsset)
		if entry.size == info.Size() && entry.modTime.Equal(info.ModTime()) {
			return entry.data, true
		}
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, false
	}
	var output []byte
	if ext == ".css" {
		output, err = assetmin.CSS(data)
	} else {
		output, err = assetmin.JS(data)
	}
	if err != nil || len(output) == 0 {
		return nil, false
	}
	minifiedAssetCache.Store(cacheKey, cachedMinifiedAsset{modTime: info.ModTime(), size: info.Size(), data: output})
	return output, true
}

func (s *Server) assetMinifyEnabled(ctx context.Context, siteID int64) bool {
	if s.database == nil {
		return true
	}
	value, err := db.LoadSiteSetting(ctx, s.database, siteID, "asset_minify")
	if err != nil {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}
