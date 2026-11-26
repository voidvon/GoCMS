package site

import (
	"net/http"
	"strings"

	"gocms/internal/db"
)

const adminSettingsRoute = "/api/admin/settings"

func (s *Server) adminSettings(w http.ResponseWriter, r *http.Request) {
	var name string
	switch r.Method {
	case http.MethodGet:
		var err error
		name, err = db.LoadAdminName(r.Context(), s.database)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "读取后台名称失败"})
			return
		}
	case http.MethodPut:
		var input struct {
			AdminName *string `json:"admin_name"`
		}
		if err := decodeRequest(r, &input); err != nil || input.AdminName == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请提供后台名称"})
			return
		}
		name = strings.TrimSpace(*input.AdminName)
		if err := db.SaveAdminName(r.Context(), s.database, name); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "保存后台名称失败"})
			return
		}
	default:
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"admin_name": name})
}
