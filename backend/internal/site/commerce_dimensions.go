package site

import (
	"net/http"
	"strconv"

	"gocms/internal/commerce"
)

func (s *Server) adminCommerceDimensions(w http.ResponseWriter, r *http.Request, site int64, parts []string) {
	if len(parts) > 1 {
		http.NotFound(w, r)
		return
	}
	if len(parts) == 0 && r.Method == http.MethodGet {
		items, err := s.commerce.Dimensions(r.Context(), site)
		if err != nil {
			contentCommerceError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
		return
	}
	if (len(parts) == 0 && r.Method == http.MethodPost) || (len(parts) == 1 && r.Method == http.MethodPut) {
		var input commerce.DimensionRecord
		// Up to 5000 option values; paymentDecode's 16 KiB cap is
		// intended for channel credentials and is too small for this dictionary.
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		if err := memberDecode(r, &input); err != nil {
			writeJSON(w, 400, map[string]string{"error": "维度配置格式无效或超过大小限制"})
			return
		}
		key := ""
		if len(parts) == 1 {
			key = parts[0]
		}
		saved, err := s.commerce.SaveDimension(r.Context(), site, key, input)
		if err != nil {
			contentCommerceError(w, err)
			return
		}
		status := http.StatusOK
		if key == "" {
			status = http.StatusCreated
		}
		writeJSON(w, status, saved)
		return
	}
	if len(parts) == 1 && r.Method == http.MethodDelete {
		version, err := strconv.ParseInt(r.URL.Query().Get("version"), 10, 64)
		if err != nil || version <= 0 {
			writeJSON(w, 400, map[string]string{"error": "缺少有效的维度版本，请刷新后重试"})
			return
		}
		if err := s.commerce.DeleteDimension(r.Context(), site, parts[0], version); err != nil {
			contentCommerceError(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	methodNotAllowed(w)
}
