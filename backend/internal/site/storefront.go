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

// ConfigureStorefront installs the independently built customer application.
// Theme pages remain static; only the /shop/ application has an SPA fallback.
func (s *Server) ConfigureStorefront(root, developmentURL string, bundled fs.FS) error {
	s.storefrontRoot = root
	s.storefrontFS = bundled
	if developmentURL == "" {
		return nil
	}
	target, err := url.Parse(developmentURL)
	if err != nil {
		return err
	}
	if (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		return &url.Error{Op: "configure storefront", URL: developmentURL, Err: os.ErrInvalid}
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, _ error) { s.serveStorefrontFiles(w, r) }
	s.storefrontDevProxy = proxy
	return nil
}

func (s *Server) serveStorefront(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w)
		return
	}
	if r.URL.Path == "/shop" {
		http.Redirect(w, r, "/shop/", http.StatusTemporaryRedirect)
		return
	}
	w.Header().Set("X-Robots-Tag", "noindex, follow")
	w.Header().Set("Cache-Control", "no-cache")
	if s.storefrontDevProxy != nil {
		s.storefrontDevProxy.ServeHTTP(w, r)
		return
	}
	s.serveStorefrontFiles(w, r)
}

func (s *Server) serveStorefrontFiles(w http.ResponseWriter, r *http.Request) {
	root := s.storefrontFS
	if s.storefrontRoot != "" {
		root = os.DirFS(s.storefrontRoot)
	}
	if root == nil {
		http.Error(w, "购物前台尚未准备好，请启动 make storefront 或构建购物前台资源", http.StatusServiceUnavailable)
		return
	}
	relative := strings.TrimPrefix(r.URL.Path, "/shop/")
	if relative == "" {
		relative = "index.html"
	}
	clean := path.Clean(relative)
	if !fs.ValidPath(clean) || strings.HasPrefix(relative, "/") || strings.Contains(relative, `\`) {
		http.NotFound(w, r)
		return
	}
	info, err := fs.Stat(root, clean)
	if err != nil || info.IsDir() {
		if filepath.Ext(clean) != "" {
			http.NotFound(w, r)
			return
		}
		clean = "index.html"
	}
	if _, err := fs.Stat(root, clean); err != nil {
		http.Error(w, "购物前台尚未构建或开发服务未启动，请运行 make storefront", http.StatusServiceUnavailable)
		return
	}
	request := r.Clone(r.Context())
	request.URL.Path = "/" + clean
	request.URL.RawPath = ""
	if clean == "index.html" {
		request.URL.Path = "/"
	}
	http.FileServer(http.FS(root)).ServeHTTP(w, request)
}
