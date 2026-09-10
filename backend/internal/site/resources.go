package site

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) resourceRoots(requestPath string) ([]string, string, bool) {
	themeRoot, _ := s.themePaths()
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
			prefix: "/images/",
			roots: func() []string {
				roots := make([]string, 0, 2)
				if s.assetsRoot != "" {
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
