package site

import (
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func (s *Server) serveAdminApp(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/admin" {
		http.Redirect(w, r, "/admin/", http.StatusTemporaryRedirect)
		return
	}
	if s.frontendDevProxy != nil {
		s.frontendDevProxy.ServeHTTP(w, r)
		return
	}
	s.serveStaticAdminApp(w, r)
}

func (s *Server) serveStaticAdminApp(w http.ResponseWriter, r *http.Request) {
	if s.frontendFS != nil {
		s.serveEmbeddedAdminApp(w, r)
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

func (s *Server) ConfigureFrontendDev(targetURL string) error {
	trimmed := strings.TrimSpace(targetURL)
	if trimmed == "" {
		s.frontendDevProxy = nil
		return nil
	}
	target, err := url.Parse(trimmed)
	if err != nil {
		return err
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		s.serveStaticAdminApp(w, req)
	}
	s.frontendDevProxy = proxy
	return nil
}

