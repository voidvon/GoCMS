package commerce

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/netip"
	"regexp"
	"strings"
	"time"

	"gocms/internal/payment"
)

// Rates always mean target major units per one base major unit. No currency is
// special: cross rates are target/base divided by source/base.
type CurrencyRule struct {
	Currency  string   `json:"currency"`
	Rate      string   `json:"rate"`
	Enabled   bool     `json:"enabled"`
	Countries []string `json:"countries"`
}
type PricingSettings struct {
	Enabled        bool           `json:"enabled"`
	BaseCurrency   string         `json:"base_currency"`
	Currencies     []CurrencyRule `json:"currencies"`
	MaxAgeHours    int            `json:"max_age_hours"`
	RatesUpdatedAt string         `json:"rates_updated_at"`
	Revision       int64          `json:"revision"`
	CountryHeader  string         `json:"country_header"`
	TrustedProxies []string       `json:"trusted_proxies"`
	RefreshRates   bool           `json:"refresh_rates,omitempty"`
}
type CurrencyInfo struct {
	Code     string `json:"code"`
	Exponent int    `json:"exponent"`
}

func SupportedCurrencies() []CurrencyInfo {
	codes := []string{"AUD", "CAD", "CHF", "CNY", "EUR", "GBP", "HKD", "JPY", "NZD", "SGD", "USD"}
	result := make([]CurrencyInfo, 0, len(codes))
	for _, code := range codes {
		exponent, _ := payment.Exponent(code)
		result = append(result, CurrencyInfo{code, exponent})
	}
	return result
}
func pricingSettings(ctx context.Context, q rowQuerier, site int64) (PricingSettings, error) {
	p := PricingSettings{Currencies: []CurrencyRule{}, MaxAgeHours: 168, TrustedProxies: []string{}}
	var raw string
	err := q.QueryRowContext(ctx, `SELECT settings_json FROM gocms_commerce_pricing WHERE site_id=?`, site).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	err = json.Unmarshal([]byte(raw), &p)
	return p, err
}
func (s Service) Pricing(ctx context.Context, site int64) (PricingSettings, error) {
	return pricingSettings(ctx, s.DB, site)
}

var decimalRate = regexp.MustCompile(`^[0-9]{1,13}(\.[0-9]{1,12})?$`)
var countryHeaderName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{0,63}$`)

func parseRate(value string) (*big.Rat, bool) {
	if !decimalRate.MatchString(value) {
		return nil, false
	}
	r, ok := new(big.Rat).SetString(value)
	return r, ok && r.Sign() > 0 && r.Cmp(new(big.Rat).SetInt64(1000000000000)) <= 0
}
func (s Service) SavePricing(ctx context.Context, site int64, p PricingSettings) (PricingSettings, error) {
	if _, err := payment.Exponent(p.BaseCurrency); err != nil {
		return p, fmt.Errorf("请选择有效的基准币种: %w", ErrInvalid)
	}
	if len(p.Currencies) == 0 || len(p.Currencies) > len(SupportedCurrencies()) || p.MaxAgeHours < 1 || p.MaxAgeHours > 8760 || len(p.TrustedProxies) > 32 {
		return p, ErrInvalid
	}
	if p.CountryHeader != "" && (!countryHeaderName.MatchString(p.CountryHeader) || len(p.TrustedProxies) == 0) {
		return p, fmt.Errorf("地区请求头必须配置可信代理网段: %w", ErrInvalid)
	}
	for _, network := range p.TrustedProxies {
		if _, err := netip.ParsePrefix(network); err != nil {
			return p, fmt.Errorf("可信代理必须为 CIDR 网段: %w", ErrInvalid)
		}
	}
	if p.TrustedProxies == nil {
		p.TrustedProxies = []string{}
	}
	seen, countries := map[string]bool{}, map[string]bool{}
	base := false
	for i := range p.Currencies {
		rule := &p.Currencies[i]
		if rule.Countries == nil {
			rule.Countries = []string{}
		}
		if _, err := payment.Exponent(rule.Currency); err != nil || seen[rule.Currency] {
			return p, ErrInvalid
		}
		seen[rule.Currency] = true
		rate, valid := parseRate(rule.Rate)
		if !valid || len(rule.Countries) > 250 {
			return p, fmt.Errorf("汇率必须为正数，最多 12 位小数: %w", ErrInvalid)
		}
		if rule.Currency == p.BaseCurrency {
			base = rule.Enabled && rate.Cmp(big.NewRat(1, 1)) == 0
		}
		for j, country := range rule.Countries {
			country = strings.ToUpper(strings.TrimSpace(country))
			if len(country) != 2 || country[0] < 'A' || country[0] > 'Z' || country[1] < 'A' || country[1] > 'Z' || countries[country] {
				return p, fmt.Errorf("国家代码需为不重复的两位字母: %w", ErrInvalid)
			}
			countries[country] = true
			rule.Countries[j] = country
		}
	}
	if !base {
		return p, fmt.Errorf("基准币种必须启用且汇率为 1: %w", ErrInvalid)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	old, err := pricingSettings(ctx, tx, site)
	if err != nil {
		return p, err
	}
	if p.Revision != old.Revision {
		return p, ErrPriceChanged
	}
	oldRates, newRates := map[string]string{}, map[string]string{}
	for _, r := range old.Currencies {
		oldRates[r.Currency] = r.Rate
	}
	for _, r := range p.Currencies {
		newRates[r.Currency] = r.Rate
	}
	before, _ := json.Marshal(oldRates)
	after, _ := json.Marshal(newRates)
	p.RatesUpdatedAt = old.RatesUpdatedAt
	if p.RefreshRates || old.BaseCurrency != p.BaseCurrency || string(before) != string(after) || p.RatesUpdatedAt == "" {
		p.RatesUpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	p.RefreshRates = false
	p.Revision = old.Revision + 1
	raw, err := json.Marshal(p)
	if err != nil {
		return p, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO gocms_commerce_pricing(site_id,settings_json) VALUES(?,?) ON CONFLICT(site_id) DO UPDATE SET settings_json=excluded.settings_json`, site, string(raw))
	if err != nil {
		return p, err
	}
	return p, tx.Commit()
}
func (p PricingSettings) CurrencyEnabled(code string) bool {
	if !p.Enabled {
		return false
	}
	for _, rule := range p.Currencies {
		if rule.Currency == code {
			return rule.Enabled
		}
	}
	return false
}
func (p PricingSettings) RecommendedCurrency(country string) string {
	if !p.Enabled {
		return ""
	}
	for _, rule := range p.Currencies {
		if rule.Enabled {
			for _, code := range rule.Countries {
				if code == country {
					return rule.Currency
				}
			}
		}
	}
	return p.BaseCurrency
}
func (p PricingSettings) rate(code string) (*big.Rat, bool) {
	if code == p.BaseCurrency {
		return big.NewRat(1, 1), true
	}
	for _, r := range p.Currencies {
		if r.Currency == code {
			return parseRate(r.Rate)
		}
	}
	return nil, false
}
func (p PricingSettings) converted(amount int64, source, target string) (int64, bool) {
	updated, err := time.Parse(time.RFC3339, p.RatesUpdatedAt)
	if err != nil || time.Since(updated) > time.Duration(p.MaxAgeHours)*time.Hour {
		return 0, false
	}
	sourceRate, ok := p.rate(source)
	if !ok {
		return 0, false
	}
	targetRate, ok := p.rate(target)
	if !ok {
		return 0, false
	}
	from, err := payment.Exponent(source)
	if err != nil {
		return 0, false
	}
	to, err := payment.Exponent(target)
	if err != nil {
		return 0, false
	}
	scale := func(n int) int64 {
		v := int64(1)
		for i := 0; i < n; i++ {
			v *= 10
		}
		return v
	}
	value := new(big.Rat).SetFrac(big.NewInt(amount), big.NewInt(scale(from)))
	value.Mul(value, new(big.Rat).Quo(targetRate, sourceRate))
	value.Mul(value, new(big.Rat).SetInt64(scale(to)))
	// Round the unit price half up using exact rational arithmetic, never float64.
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(value.Num(), value.Denom(), remainder)
	if new(big.Int).Lsh(remainder, 1).Cmp(value.Denom()) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() || quotient.Int64() <= 0 || quotient.Int64() > 99999999999 {
		return 0, false
	}
	return quotient.Int64(), true
}
func quoteOffer(ctx context.Context, q rowQuerier, site int64, p PricingSettings, o Offer, target string) (Offer, error) {
	o.PricingRevision = p.Revision
	if target == "" || !p.Enabled || target == o.Currency {
		return o, nil
	}
	if !p.CurrencyEnabled(target) {
		return o, ErrInvalid
	}
	amount, ok := o.FixedPrices[target]
	method := "fixed"
	if !ok {
		amount, ok = p.converted(o.Amount, o.Currency, target)
		method = "exchange"
	}
	if !ok {
		o.PricingUnavailable = true
		return o, nil
	}
	var channels int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM gocms_payment_account WHERE site_id=? AND currency=? AND enabled=1`, site, target).Scan(&channels); err != nil {
		return o, err
	}
	if channels == 0 {
		o.DisplayAmount = amount
		o.DisplayCurrency = target
		return o, nil
	}
	o.SourceAmount, o.SourceCurrency = o.Amount, o.Currency
	o.Amount, o.Currency, o.PricingMethod = amount, target, method
	return o, nil
}
func (s Service) QuoteOffers(ctx context.Context, site int64, items []Offer, target string) ([]Offer, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := pricingSettings(ctx, tx, site)
	if err != nil {
		return nil, err
	}
	if target == "" && p.Enabled {
		target = p.BaseCurrency
	}
	if target != "" && p.Enabled && !p.CurrencyEnabled(target) {
		return nil, ErrInvalid
	}
	for i := range items {
		items[i], err = quoteOffer(ctx, tx, site, p, items[i], target)
		if err != nil {
			return nil, err
		}
	}
	return items, tx.Commit()
}
