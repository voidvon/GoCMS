package site

import (
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func (s *Server) serveAdminApp(w http.ResponseWriter, r *http.Request) {
	if s.frontendFS != nil {
		s.serveEmbeddedAdminApp(w, r)
		return
	}
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

func (s *Server) serveEmbeddedAdminApp(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/admin" {
		http.Redirect(w, r, "/admin/", http.StatusTemporaryRedirect)
		return
	}
	relative := strings.TrimPrefix(r.URL.Path, "/admin/")
	if relative == "" {
		relative = "index.html"
	}
	clean := path.Clean(relative)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(relative, "/") {
		http.NotFound(w, r)
		return
	}
	info, err := fs.Stat(s.frontendFS, clean)
	if err != nil || info.IsDir() {
		if filepath.Ext(clean) != "" {
			http.NotFound(w, r)
			return
		}
		clean = "index.html"
	}
	request := r.Clone(r.Context())
	request.URL.Path = "/" + clean
	if clean == "index.html" {
		request.URL.Path = "/"
	}
	request.URL.RawPath = ""
	http.FileServer(http.FS(s.frontendFS)).ServeHTTP(w, request)
}

func (s *Server) ConfigureEmbeddedFrontend(frontend fs.FS) {
	s.frontendFS = frontend
}
