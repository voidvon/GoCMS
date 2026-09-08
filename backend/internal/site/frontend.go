package site

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) serveAdminApp(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/admin" {
		http.Redirect(w, r, "/admin/", http.StatusTemporaryRedirect)
		return
	}
	rel := strings.TrimPrefix(r.URL.Path, "/admin/")
	if rel == "" {
		rel = "index.html"
	}
	if strings.Contains(rel, "..") {
		http.NotFound(w, r)
		return
	}
	p := filepath.Join(s.frontendRoot, filepath.FromSlash(rel))
	if _, e := os.Stat(p); e != nil {
		if filepath.Ext(rel) != "" {
			http.NotFound(w, r)
			return
		}
		p = filepath.Join(s.frontendRoot, "index.html")
	}
	http.ServeFile(w, r, p)
}
