package site

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"gocms/internal/embedded"
)

// Installation is explicit and never replaces administrator-owned templates.
func (s *Server) installPaymentTemplate(w http.ResponseWriter, r *http.Request, site int64) {
	const name = "payment_checkout.html"
	content, err := embedded.ReadTemplate(name)
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": "程序未包含购买模板，请更新完整发行包或在主题管理中添加模板"})
		return
	}
	_, root, err := s.ensureSiteThemeLocal(r.Context(), site)
	if err != nil || root == "" {
		writeJSON(w, 503, map[string]string{"error": "无法准备当前站点的主题模板目录"})
		return
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		writeJSON(w, 503, map[string]string{"error": "主题模板目录不可写"})
		return
	}
	file, err := os.OpenFile(filepath.Join(root, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if errors.Is(err, os.ErrExist) {
		writeJSON(w, 409, map[string]string{"error": "当前主题已存在 payment_checkout.html，请在主题管理中编辑；安装不会覆盖已有模板"})
		return
	}
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": "购买模板保存失败"})
		return
	}
	_, writeErr := file.Write(content)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(filepath.Join(root, name))
		writeJSON(w, 503, map[string]string{"error": "购买模板保存失败"})
		return
	}
	s.invalidateSitePublication(site)
	writeJSON(w, 201, map[string]string{"template": name})
}
