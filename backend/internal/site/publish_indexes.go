package site

import (
	"errors"
	"net/http"

	"gocms/internal/generator"
)

func (s *Server) adminPublishIndexes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	siteID, err := s.resolveSiteID(r, s.currentAdmin(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	pub := s.publicationForSite(r.Context(), siteID)
	if pub == nil {
		http.Error(w, "尚未配置网站发布", http.StatusServiceUnavailable)
		return
	}
	var targets []generator.IndexTarget
	if r.Method == http.MethodGet {
		targets, err = pub.publisher.IndexTargets(r.Context())
	} else {
		var payload struct {
			Target string `json:"target"`
			Format string `json:"format"`
		}
		if err := decodeRequest(r, &payload); err != nil || payload.Target == "" || payload.Format == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请指定发布目标和索引格式"})
			return
		}
		targets, err = pub.publisher.GenerateIndexes(r.Context(), payload.Target, payload.Format)
	}
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, generator.ErrPublishBusy), errors.Is(err, generator.ErrIndexNotPublished):
			status = http.StatusConflict
		case errors.Is(err, generator.ErrIndexTargetNotFound):
			status = http.StatusNotFound
		case errors.Is(err, generator.ErrIndexConfiguration), errors.Is(err, generator.ErrIndexFormat):
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "targets": targets})
}
