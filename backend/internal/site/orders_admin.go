package site

import (
	"errors"
	"net/http"
	"strings"

	"gocms/internal/commerce"
)

func (s *Server) authorizeOrders(w http.ResponseWriter, r *http.Request, u *AdminUser) bool {
	if r.Method != http.MethodGet && !memberRequest(w, r) {
		return false
	}
	key := "orders"
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/orders"), "/")
	parts := strings.Split(path, "/")
	if r.Method != http.MethodGet {
		switch {
		case path == "" || strings.HasSuffix(path, "/payment-link"):
			key = "orders.create"
		case strings.HasSuffix(path, "/shipments"):
			key = "orders.ship"
		case len(parts) == 3 && parts[1] == "shipments":
			key = "orders.shipping.edit"
		case strings.HasSuffix(path, "/recipient"):
			key = "orders.recipient.edit"
		case strings.HasSuffix(path, "/complete"):
			key = "orders.complete"
		case strings.HasSuffix(path, "/refund") || strings.HasSuffix(path, "/refund-sync"):
			key = "orders.refund"
		}
	}
	site, err := s.resolveSiteID(r, u)
	if err != nil || !u.HasPermissionInSite(site, "orders") || !u.HasPermissionInSite(site, key) {
		writeJSON(w, 403, map[string]string{"error": "当前用户组没有此订单操作权限"})
		return false
	}
	return true
}
func (s *Server) adminOrderDetail(w http.ResponseWriter, r *http.Request, site int64, id string) {
	o, err := s.commerce.Order(r.Context(), site, id)
	if err != nil {
		orderError(w, err)
		return
	}
	type adminShipment struct {
		commerce.Shipment
		InternalNote string `json:"internal_note"`
		NoticeStatus string `json:"notice_status"`
	}
	packages := []adminShipment{}
	for _, p := range o.Shipments {
		packages = append(packages, adminShipment{p, p.InternalNote, p.NoticeStatus})
	}
	events, err := s.commerce.OrderEvents(r.Context(), site, id)
	if err != nil {
		orderError(w, err)
		return
	}
	writeJSON(w, 200, struct {
		commerce.Order
		Shipments []adminShipment       `json:"shipments"`
		Events    []commerce.OrderEvent `json:"events"`
	}{o, packages, events})
}
func (s *Server) adminOrders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	site, err := s.resolveSiteID(r, s.currentAdmin(r))
	if err != nil {
		writeJSON(w, 403, map[string]string{"error": "无权访问站点订单"})
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/orders"), "/")
	p := strings.Split(path, "/")
	ctx := r.Context()
	if path == "" && r.Method == http.MethodPost {
		var in commerce.ManualOrderInput
		if !paymentDecode(w, r, &in) {
			return
		}
		o, err := s.commerce.CreateManualOrder(ctx, site, s.currentAdmin(r).Username, r.Header.Get("Idempotency-Key"), in)
		if err != nil {
			orderError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, o)
		return
	}
	if (path == "creation-options" || path == "catalog") && r.Method == http.MethodGet {
		if !s.currentAdmin(r).HasPermissionInSite(site, "orders.create") {
			writeJSON(w, 403, map[string]string{"error": "当前用户组没有创建订单权限"})
			return
		}
		if path == "catalog" {
			items, more, err := s.commerce.OrderVariants(ctx, site, parseIntOrZero(r.URL.Query().Get("account_id")), positiveInt(r.URL.Query().Get("page"), 1), r.URL.Query().Get("q"))
			if err != nil {
				orderError(w, err)
				return
			}
			writeJSON(w, 200, map[string]any{"items": items, "has_more": more})
			return
		}
		accounts, err := s.commerce.Accounts(ctx, site)
		if err != nil {
			orderError(w, err)
			return
		}
		items := []map[string]any{}
		for _, a := range accounts {
			if a.Enabled {
				items = append(items, map[string]any{"id": a.ID, "name": a.Name, "currency": a.Currency, "public_url": a.PublicURL, "sandbox": a.Environment == "sandbox"})
			}
		}
		writeJSON(w, 200, map[string]any{"items": items})
		return
	}
	if path == "" && r.Method == http.MethodGet {
		page := positiveInt(r.URL.Query().Get("page"), 1)
		if page > 100000 {
			page = 100000
		}
		items, err := s.commerce.FilterOrders(ctx, site, page, r.URL.Query().Get("status"), r.URL.Query().Get("q"))
		if err != nil {
			orderError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"items": items, "page": page})
		return
	}
	if path == "carriers" && r.Method == http.MethodGet {
		items, err := s.commerce.ShipmentCarriers(ctx, site)
		if err != nil {
			orderError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
		return
	}
	if len(p) == 1 && r.Method == http.MethodGet {
		s.adminOrderDetail(w, r, site, p[0])
		return
	}
	if len(p) < 2 || len(p) > 3 {
		http.NotFound(w, r)
		return
	}
	actor := s.currentAdmin(r).Username
	if len(p) == 2 && p[1] == "payment-link" && r.Method == http.MethodPost {
		var in struct {
			Revision int64 `json:"revision"`
		}
		if !paymentDecode(w, r, &in) {
			return
		}
		link, err := s.commerce.IssueOrderLink(ctx, site, p[0], actor, in.Revision)
		if err != nil {
			orderError(w, err)
			return
		}
		writeJSON(w, 200, map[string]string{"payment_url": link})
		return
	}
	switch {
	case p[1] == "shipments" && (r.Method == http.MethodPost && len(p) == 2 || r.Method == http.MethodPut && len(p) == 3):
		var in commerce.ShipmentInput
		if !paymentDecode(w, r, &in) {
			return
		}
		if len(p) == 2 {
			_, _, in.Notify = s.recoveryMail(r)
			_, err = s.commerce.CreateShipment(ctx, site, p[0], actor, in)
		} else {
			err = s.commerce.EditShipment(ctx, site, p[0], p[2], actor, in)
		}
	case len(p) == 2 && p[1] == "complete" && r.Method == http.MethodPost:
		var in struct {
			Revision int64 `json:"revision"`
		}
		if !paymentDecode(w, r, &in) {
			return
		}
		err = s.commerce.CompleteOrder(ctx, site, p[0], actor, in.Revision)
	case len(p) == 2 && p[1] == "recipient" && r.Method == http.MethodPut:
		var in struct {
			Revision int64            `json:"revision"`
			Name     string           `json:"customer_name"`
			Address  commerce.Address `json:"address"`
			Reason   string           `json:"reason"`
		}
		if !paymentDecode(w, r, &in) {
			return
		}
		err = s.commerce.UpdateRecipient(ctx, site, p[0], actor, in.Revision, in.Name, in.Address, in.Reason)
	case len(p) == 2 && r.Method == http.MethodPost && (p[1] == "sync" || p[1] == "refund" || p[1] == "refund-sync"):
		var in struct {
			ID string `json:"id"`
		}
		if !paymentDecode(w, r, &in) {
			return
		}
		o, e := s.commerce.Order(ctx, site, p[0])
		if e != nil {
			orderError(w, e)
			return
		}
		found := false
		if p[1] == "refund-sync" {
			for _, v := range o.Refunds {
				if v.ID == in.ID {
					found = true
				}
			}
		} else {
			for _, v := range o.Attempts {
				if v.ID == in.ID {
					found = true
				}
			}
		}
		if !found {
			commerceError(w, commerce.ErrNotFound)
			return
		}
		switch p[1] {
		case "sync":
			err = s.commerce.Sync(ctx, site, in.ID)
		case "refund":
			_, err = s.commerce.RefundAttempt(ctx, site, in.ID)
		case "refund-sync":
			err = s.commerce.SyncRefund(ctx, site, in.ID)
		}
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		orderError(w, err)
		return
	}
	s.adminOrderDetail(w, r, site, p[0])
}

func orderError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, commerce.ErrInvalid):
		writeJSON(w, 400, map[string]string{"error": "订单信息无效，请检查商品金额、数量、图片、客户及收货信息、物流单号、网址和交付时间", "code": "invalid_input"})
	case errors.Is(err, commerce.ErrConflict):
		writeJSON(w, 409, map[string]string{"error": "订单已变化，或当前付款、退款及交付状态不允许此操作，请刷新订单后重试", "code": "conflict"})
	default:
		commerceError(w, err)
	}
}
