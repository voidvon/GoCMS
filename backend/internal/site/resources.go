package site

import (
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func canonicalImageURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	u, err := url.Parse(value)
	if err != nil || u.Path == "" {
		return value
	}
	if u.Host != "" && !strings.EqualFold(u.Hostname(), "www.bilvie.com") {
		return value
	}
	normalized := strings.ReplaceAll(u.Path, `\`, "/")
	trimmed := strings.TrimPrefix(normalized, "/")
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "uploadfile/") || strings.HasPrefix(lower, "produppic/") {
		filename := path.Base(trimmed)
		if filename == "." || filename == "/" || filename == "" {
			return value
		}
		u.Scheme, u.Host, u.Opaque = "", "", ""
		u.User = nil
		u.RawPath = ""
		u.Path = "/images/" + filename
		return u.String()
	}
	return value
}

func (s *Server) resourceRoots(requestPath string) ([]string, string, bool) {
	themeRoots := func(directory string) []string {
		if s.themeRoot == "" {
			return nil
		}
		return []string{filepath.Join(s.themeRoot, directory)}
	}
	for _, route := range []struct {
		prefix string
		roots  func() []string
	}{
		{
			prefix: "/images/",
			roots: func() []string {
				roots := make([]string, 0, 2)
				if s.assetsRoot != "" {
					roots = append(roots, filepath.Join(s.assetsRoot, "images"))
				}
				if s.themeRoot != "" {
					roots = append(roots, filepath.Join(s.themeRoot, "images"))
				}
				return roots
			},
		},
		{prefix: "/css/", roots: func() []string { return themeRoots("css") }},
		{prefix: "/js/", roots: func() []string { return themeRoots("js") }},
		{prefix: "/skin/", roots: func() []string { return themeRoots("skin") }},
	} {
		lower := strings.ToLower(requestPath)
		base := strings.TrimSuffix(route.prefix, "/")
		if lower == base {
			return route.roots(), "", true
		}
		if strings.HasPrefix(lower, route.prefix) {
			return route.roots(), requestPath[len(route.prefix):], true
		}
	}
	return nil, "", false
}

// Files are opened inside the configured resource roots, without allowing symlink escapes.
func (s *Server) serveResource(w http.ResponseWriter, r *http.Request) bool {
	roots, rel, ok := s.resourceRoots(r.URL.Path)
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
			http.ServeContent(w, r, info.Name(), info.ModTime(), f)
			_ = f.Close()
			return true
		}
		_ = f.Close()
	}
	http.NotFound(w, r)
	return true
}
