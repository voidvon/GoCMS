package site

import (
	"errors"
	"net/http"
	"strings"

	"gocms/internal/commerce"
)

// Prices and sale switches remain protected even when submitted through CMS
// content/category endpoints instead of the payment management page.
func (s *Server) authorizeContentCommerce(w http.ResponseWriter, r *http.Request, site int64) bool {
	user, ok, session := s.authenticateRequest(r)
	if !ok || user == nil || !session || !user.CanManageSite(site) || !user.HasPermissionInSite(site, "payments") {
		writeJSON(w, 403, map[string]string{"error": "销售配置需要支付与交易管理权限和后台登录会话"})
		return false
	}
	return memberRequest(w, r)
}

func contentCommerceError(w http.ResponseWriter, err error) {
	if errors.Is(err, commerce.ErrInvalid) {
		writeJSON(w, 400, map[string]string{"error": strings.TrimSuffix(err.Error(), ": "+commerce.ErrInvalid.Error())})
		return
	}
	commerceError(w, err)
}
