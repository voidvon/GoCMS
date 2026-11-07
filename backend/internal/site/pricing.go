package site

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"gocms/internal/commerce"
)

func (s *Server) adminPricing(w http.ResponseWriter, r *http.Request, site int64) {
	switch r.Method {
	case http.MethodGet:
		settings, err := s.commerce.Pricing(r.Context(), site)
		if err != nil {
			commerceError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"settings": settings, "supported": commerce.SupportedCurrencies()})
	case http.MethodPut:
		var settings commerce.PricingSettings
		if !paymentDecode(w, r, &settings) {
			return
		}
		saved, err := s.commerce.SavePricing(r.Context(), site, settings)
		if errors.Is(err, commerce.ErrPriceChanged) {
			writeJSON(w, 409, map[string]string{"error": "币种配置已更新，请刷新后重新编辑", "code": "conflict"})
			return
		}
		if err != nil {
			contentCommerceError(w, err)
			return
		}
		writeJSON(w, 200, saved)
	default:
		methodNotAllowed(w)
	}
}

// Country hints from an edge proxy are optional and only accepted from CIDRs
// explicitly configured by an administrator. Never trust arbitrary forwarded IPs.
func pricingCountry(r *http.Request, p commerce.PricingSettings) string {
	if country := strings.ToUpper(r.URL.Query().Get("country")); len(country) == 2 && country[0] >= 'A' && country[0] <= 'Z' && country[1] >= 'A' && country[1] <= 'Z' {
		return country
	}
	if p.CountryHeader == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return ""
	}
	address = address.Unmap()
	trusted := false
	for _, raw := range p.TrustedProxies {
		if prefix, err := netip.ParsePrefix(raw); err == nil && prefix.Contains(address) {
			trusted = true
			break
		}
	}
	if !trusted {
		return ""
	}
	country := strings.ToUpper(strings.TrimSpace(r.Header.Get(p.CountryHeader)))
	if len(country) != 2 || country[0] < 'A' || country[0] > 'Z' || country[1] < 'A' || country[1] > 'Z' {
		return ""
	}
	return country
}
func (s *Server) publicPricing(w http.ResponseWriter, r *http.Request, site int64) {
	p, err := s.commerce.Pricing(r.Context(), site)
	if err != nil {
		commerceError(w, err)
		return
	}
	currencies := []commerce.CurrencyInfo{}
	if p.Enabled {
		for _, item := range commerce.SupportedCurrencies() {
			if p.CurrencyEnabled(item.Code) {
				currencies = append(currencies, item)
			}
		}
	}
	writeJSON(w, 200, map[string]any{"site_id": site, "enabled": p.Enabled, "base_currency": p.BaseCurrency, "currencies": currencies, "recommended_currency": p.RecommendedCurrency(pricingCountry(r, p)), "revision": p.Revision})
}
