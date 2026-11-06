package site

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"gocms/internal/commerce"
	"gocms/internal/db"
	"gocms/internal/payment"
	"gocms/internal/payment/providers/pingpong"
)

func (s *Server) configurePayments() error {
	cipher := &payment.Cipher{}
	if key := os.Getenv("GOCMS_PAYMENT_KEY"); key != "" {
		candidate, err := payment.NewCipher(key)
		if err != nil {
			s.paymentKeyEnvironmentError = err
		} else {
			cipher = candidate
			s.paymentKeySource = "environment"
		}
	}
	s.commerce = &commerce.Service{DB: s.database, Cipher: cipher, Providers: map[string]payment.Provider{"pingpong": pingpong.Provider{}}}
	return nil
}

func (s *Server) StartPaymentMaintenance(ctx context.Context) {
	if s.commerce == nil || s.commerce.DB == nil {
		return
	}
	go func() {
		run := func() {
			job, cancel := context.WithTimeout(ctx, 50*time.Second)
			defer cancel()
			if err := s.commerce.Reconcile(job); err != nil && ctx.Err() == nil {
				log.Printf("payment reconciliation: %v", err)
			}
		}
		run()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}

func commerceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, commerce.ErrPriceChanged):
		writeJSON(w, 409, map[string]string{"error": "商品价格已变化，请刷新购物车并重新确认金额", "code": "price_changed"})
	case errors.Is(err, commerce.ErrInvalid), errors.Is(err, payment.ErrInvalid):
		writeJSON(w, 400, map[string]string{"error": "输入或支付响应无效，请检查配置与交易信息", "code": "invalid_input"})
	case errors.Is(err, commerce.ErrNotFound):
		writeJSON(w, 404, map[string]string{"error": "记录不存在", "code": "not_found"})
	case errors.Is(err, commerce.ErrConflict):
		writeJSON(w, 409, map[string]string{"error": "订单状态已变化或存在待确认交易，请刷新并同步状态", "code": "conflict"})
	default:
		writeJSON(w, 503, map[string]string{"error": "暂时无法完成操作；支付或退款结果待确认时，请同步状态，勿重复下单", "code": "unavailable"})
	}
}

func paymentDecode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	if memberDecode(r, v) != nil {
		commerceError(w, commerce.ErrInvalid)
		return false
	}
	return true
}

func paymentAdminPath(r *http.Request) string {
	return strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/payments"), "/")
}

func (s *Server) adminPayments(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.commerce == nil || s.database == nil {
		commerceError(w, payment.ErrUnavailable)
		return
	}
	site, err := s.resolveSiteID(r, s.currentAdmin(r))
	if err != nil {
		writeJSON(w, 403, map[string]string{"error": "无权访问站点支付"})
		return
	}
	p := strings.Split(paymentAdminPath(r), "/")
	page := positiveInt(r.URL.Query().Get("page"), 1)
	if page > 100000 {
		page = 100000
	}
	ctx := r.Context()
	if len(p) == 1 && p[0] == "status" && r.Method == http.MethodGet {
		s.paymentStatus(w, r)
		return
	}
	if len(p) == 1 && p[0] == "key" && r.Method == http.MethodPost {
		s.initializePaymentKey(w, r)
		return
	}
	if len(p) == 2 && p[0] == "key" && r.Method == http.MethodPost {
		switch p[1] {
		case "export":
			s.exportPaymentKey(w, r)
		case "adopt":
			s.adoptPaymentKey(w, r)
		default:
			http.NotFound(w, r)
		}
		return
	}
	if len(p) == 1 && p[0] == "checkout-template" && r.Method == http.MethodPost {
		s.installPaymentTemplate(w, r, site)
		return
	}
	if len(p) == 1 && r.Method == http.MethodGet {
		var items any
		switch p[0] {
		case "accounts":
			items, err = s.commerce.Accounts(ctx, site)
		case "offers":
			items, err = s.commerce.Offers(ctx, site, false, page)
		case "orders":
			items, err = s.commerce.Orders(ctx, site, 0, page)
		case "events":
			items, err = s.commerce.Events(ctx, site, page)
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			commerceError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"items": items, "page": page})
		return
	}
	if len(p) == 1 && p[0] == "accounts" && r.Method == http.MethodPost {
		var in struct {
			Name        string `json:"name"`
			Provider    string `json:"provider"`
			Environment string `json:"environment"`
			ClientID    string `json:"client_id"`
			MerchantID  string `json:"merchant_id"`
			Secret      string `json:"secret"`
			Currency    string `json:"currency"`
			PublicURL   string `json:"public_url"`
			ReturnPath  string `json:"return_path"`
			Enabled     bool   `json:"enabled"`
		}
		if !paymentDecode(w, r, &in) {
			return
		}
		if !s.commerce.Cipher.Configured() {
			writeJSON(w, 503, map[string]string{"error": "请先由超级管理员初始化支付加密密钥，再添加支付账户"})
			return
		}
		var a commerce.Account
		a, err = s.commerce.CreateAccount(ctx, site, commerce.Account{Name: in.Name, Provider: in.Provider, Environment: in.Environment, ClientID: in.ClientID, MerchantID: in.MerchantID, Secret: in.Secret, Currency: in.Currency, PublicURL: in.PublicURL, ReturnPath: in.ReturnPath, Enabled: in.Enabled})
		if err != nil {
			commerceError(w, err)
			return
		}
		writeJSON(w, 201, a)
		return
	}
	if len(p) == 2 && p[0] == "accounts" && r.Method == http.MethodPut {
		id, _ := strconv.ParseInt(p[1], 10, 64)
		var in struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		}
		if !paymentDecode(w, r, &in) {
			return
		}
		err = s.commerce.UpdateAccount(ctx, site, id, in.Name, in.Enabled)
	} else if (len(p) == 1 || len(p) == 2) && p[0] == "offers" && (r.Method == http.MethodPost || r.Method == http.MethodPut) {
		writeJSON(w, 409, map[string]string{"error": "商品销售配置已并入内容编辑，请在分类开启在线销售后配置内容规格与价格"})
		return
	} else if len(p) == 2 && p[0] == "orders" && r.Method == http.MethodGet {
		var o commerce.Order
		o, err = s.commerce.Order(ctx, site, p[1])
		if err != nil {
			commerceError(w, err)
			return
		}
		writeJSON(w, 200, o)
		return
	} else if len(p) == 3 && p[0] == "orders" && p[2] == "fulfillment" && r.Method == http.MethodPut {
		var in struct {
			Status string `json:"status"`
		}
		if !paymentDecode(w, r, &in) {
			return
		}
		err = s.commerce.Fulfill(ctx, site, p[1], in.Status)
	} else if len(p) == 3 && p[0] == "refunds" && p[2] == "sync" && r.Method == http.MethodPost {
		err = s.commerce.SyncRefund(ctx, site, p[1])
	} else if len(p) == 3 && p[0] == "attempts" && r.Method == http.MethodPost {
		if p[2] == "sync" {
			err = s.commerce.Sync(ctx, site, p[1])
		} else if p[2] == "refund" {
			_, err = s.commerce.RefundAttempt(ctx, site, p[1])
		} else {
			http.NotFound(w, r)
			return
		}
	} else {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		commerceError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) paymentWebhook(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if s.commerce == nil || s.database == nil {
		http.Error(w, "FAIL", 503)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/payments/webhooks/")
	if len(id) != 37 || !strings.HasPrefix(id, "acct_") {
		http.Error(w, "FAIL", 400)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "FAIL", 413)
		return
	}
	if err := s.commerce.Notify(r.Context(), id, raw); err != nil {
		http.Error(w, "FAIL", 400)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("OK"))
}

func (s *Server) publicCommerce(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.commerce == nil || s.database == nil {
		commerceError(w, payment.ErrUnavailable)
		return
	}
	if !memberRequest(w, r) {
		return
	}
	site, err := s.resolvePublicSiteID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/commerce/"), "/"), "/")
	page := positiveInt(r.URL.Query().Get("page"), 1)
	if page > 100000 {
		page = 100000
	}

	if len(p) == 1 && p[0] == "storefront" && r.Method == http.MethodGet {
		current, err := db.GetSiteByID(r.Context(), s.database, site)
		if err != nil {
			commerceError(w, err)
			return
		}
		settings, err := db.LoadSiteSettingsForSite(r.Context(), s.database, site)
		if err != nil {
			commerceError(w, err)
			return
		}
		name := strings.TrimSpace(settings["site_name"])
		if name == "" {
			name = current.Name
		}
		languages, err := s.getLanguages(r.Context(), site)
		if err != nil {
			commerceError(w, err)
			return
		}
		defaultLanguage := ""
		for _, language := range languages {
			if language.IsDefault == 1 && language.IsEnabled == 1 {
				defaultLanguage = language.Code
				break
			}
		}
		writeJSON(w, 200, map[string]any{"site_id": site, "site_name": name, "path": "/shop/", "default_language": defaultLanguage})
		return
	}
	if len(p) == 1 && p[0] == "variants" && r.Method == http.MethodGet {
		raw := strings.Split(r.URL.Query().Get("ids"), ",")
		ids := make([]int64, 0, len(raw))
		for _, id := range raw {
			ids = append(ids, parseIntOrZero(id))
		}
		items, err := s.commerce.Variants(r.Context(), site, ids)
		if err != nil {
			commerceError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
		return
	}
	if len(p) == 1 && p[0] == "channels" && r.Method == http.MethodGet {
		channels, err := s.publicPaymentChannels(r.Context(), site)
		if err != nil {
			commerceError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"channels": channels})
		return
	}
	if len(p) == 1 && p[0] == "catalog" && r.Method == http.MethodGet {
		offers, err := s.commerce.Offers(r.Context(), site, true, page, parseIntOrZero(r.URL.Query().Get("content_id")), parseIntOrZero(r.URL.Query().Get("category_id")))
		if err != nil {
			commerceError(w, err)
			return
		}
		channels, err := s.publicPaymentChannels(r.Context(), site)
		if err != nil {
			commerceError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"offers": offers, "channels": channels})
		return
	}
	u, ok := s.currentSiteUser(r)
	if !ok {
		memberError(w, 401, "authentication_required", "请先登录会员账户")
		return
	}
	if len(p) == 1 && p[0] == "orders" {
		if r.Method == http.MethodGet {
			items, err := s.commerce.Orders(r.Context(), site, u.ID, page)
			if err != nil {
				commerceError(w, err)
				return
			}
			writeJSON(w, 200, map[string]any{"items": items})
			return
		}
		if r.Method == http.MethodPost {
			if !s.memberAttempt(w, r, "commerce-order", strconv.FormatInt(u.ID, 10), 20) {
				return
			}
			var in commerce.OrderInput
			if !paymentDecode(w, r, &in) {
				return
			}
			o, err := s.commerce.CreateOrder(r.Context(), site, u.ID, r.Header.Get("Idempotency-Key"), in)
			if err != nil {
				commerceError(w, err)
				return
			}
			writeJSON(w, 201, o)
			return
		}
	}
	if len(p) >= 2 && p[0] == "orders" {
		o, err := s.commerce.Order(r.Context(), site, p[1])
		if err != nil || o.BuyerID != u.ID {
			commerceError(w, commerce.ErrNotFound)
			return
		}
		if len(p) == 2 && r.Method == http.MethodGet {
			writeJSON(w, 200, o)
			return
		}
		if len(p) == 3 && r.Method == http.MethodPost {
			if !s.memberAttempt(w, r, "commerce-payment", strconv.FormatInt(u.ID, 10), 20) {
				return
			}
			if p[2] == "pay" {
				var in struct {
					AccountID int64 `json:"account_id"`
				}
				if !paymentDecode(w, r, &in) {
					return
				}
				// Forwarded addresses are not trusted implicitly for financial requests.
				ip, _, splitErr := net.SplitHostPort(r.RemoteAddr)
				if splitErr != nil {
					ip = r.RemoteAddr
				}
				a, err := s.commerce.Pay(r.Context(), site, o.ID, in.AccountID, ip)
				if err != nil {
					commerceError(w, err)
					return
				}
				writeJSON(w, 200, a)
				return
			}
			if p[2] == "sync" {
				for _, a := range o.Attempts {
					if a.Status != "succeeded" && a.Status != "closed" {
						err = s.commerce.Sync(r.Context(), site, a.ID)
						break
					}
				}
				if err != nil {
					commerceError(w, err)
					return
				}
				o, err = s.commerce.Order(r.Context(), site, o.ID)
				if err != nil {
					commerceError(w, err)
					return
				}
				writeJSON(w, 200, o)
				return
			}
		}
	}
	http.NotFound(w, r)
}

// Public channel metadata is also available without loading the product catalog.
func (s *Server) publicPaymentChannels(ctx context.Context, site int64) ([]map[string]any, error) {
	accounts, err := s.commerce.Accounts(ctx, site)
	if err != nil {
		return nil, err
	}
	channels := []map[string]any{}
	for _, account := range accounts {
		if account.Enabled {
			channels = append(channels, map[string]any{"id": account.ID, "name": account.Name, "provider": account.Provider, "currency": account.Currency, "sandbox": account.Environment == "sandbox"})
		}
	}
	return channels, nil
}

// Payment account mutation and refund initiation require an interactive admin
// session, same-origin validation and explicit operation-level permission.
func (s *Server) authorizePaymentOperation(w http.ResponseWriter, r *http.Request, u *AdminUser, session bool) bool {
	if r.Method != http.MethodGet {
		if !session {
			writeJSON(w, 403, map[string]string{"error": "支付操作需要后台登录会话"})
			return false
		}
		if !memberRequest(w, r) {
			return false
		}
	}
	key := "payments"
	path := paymentAdminPath(r)
	if path == "key" || strings.HasPrefix(path, "key/") {
		if !u.IsSuper {
			writeJSON(w, 403, map[string]string{"error": "只有超级管理员可以管理支付加密密钥"})
			return false
		}
		return true
	}
	if path == "checkout-template" {
		key = "theme"
	}
	if strings.HasPrefix(path, "accounts") {
		key = "payments.configure"
	}
	if strings.HasSuffix(path, "/refund") {
		key = "payments.refund"
	}
	if strings.HasSuffix(path, "/fulfillment") {
		key = "payments.fulfill"
	}
	site, err := s.resolveSiteID(r, u)
	if err != nil || !u.HasPermissionInSite(site, "payments") || (key != "payments" && !u.HasPermissionInSite(site, key)) {
		writeJSON(w, 403, map[string]string{"error": "当前用户组没有此支付操作权限"})
		return false
	}
	return true
}
