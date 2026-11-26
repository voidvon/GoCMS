package site

import (
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

var previewImageTypes = map[string]string{
	".avif": "image/avif",
	".bmp":  "image/bmp",
	".gif":  "image/gif",
	".ico":  "image/vnd.microsoft.icon",
	".jpeg": "image/jpeg",
	".jpg":  "image/jpeg",
	".png":  "image/png",
	".svg":  "image/svg+xml",
	".webp": "image/webp",
}

// The content module authorizes this read. Native image requests identify the
// site in the URL because they cannot send the admin API's site header.
func (s *Server) adminContentImagePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		methodNotAllowed(w)
		return
	}
	query := r.URL.Query()
	requestedSite, err := strconv.ParseInt(query.Get("site_id"), 10, 64)
	if err != nil || requestedSite < 1 {
		http.NotFound(w, r)
		return
	}
	siteID, err := s.resolveSiteID(r, s.currentAdmin(r))
	if err != nil || siteID != requestedSite {
		http.NotFound(w, r)
		return
	}
	source, err := url.Parse(query.Get("path"))
	if err != nil || source.Scheme != "" || source.Host != "" || source.RawQuery != "" || source.ForceQuery || source.Fragment != "" ||
		!strings.HasPrefix(source.Path, "/") || path.Clean(source.Path) != source.Path || strings.ContainsAny(source.Path, "\\\x00") {
		http.NotFound(w, r)
		return
	}
	contentType := previewImageTypes[strings.ToLower(path.Ext(source.Path))]
	name := strings.TrimPrefix(source.Path, "/")
	lower := strings.ToLower(name)
	if contentType == "" || (!strings.HasPrefix(lower, "images/") && !strings.HasPrefix(lower, "uploads/") && !isPublicRootImage(name)) {
		http.NotFound(w, r)
		return
	}
	roots, relative, ok := s.resourceRoots(r.Context(), siteID, source.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	file, info, _ := openResource(roots, relative)
	if file == nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}
