package commerce

import (
	"context"
	"strings"
)

// OrderVariants searches the current site's purchasable content variants. Quote
// each row in the selected channel currency without interpreting another
// currency's amount as a price in that currency.
func (s Service) OrderVariants(ctx context.Context, site, accountID int64, page int, query string) ([]Offer, bool, error) {
	query = strings.TrimSpace(query)
	if len(query) > 128 || page < 1 || page > 100000 {
		return nil, false, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	a, err := scanAccount(tx.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM gocms_payment_account WHERE site_id=? AND id=?`, site, accountID))
	if err != nil {
		return nil, false, err
	}
	if !a.Enabled {
		return nil, false, ErrInvalid
	}
	pricing, err := pricingSettings(ctx, tx, site)
	if err != nil {
		return nil, false, err
	}
	filter := ""
	args := []any{site}
	if query != "" {
		filter = ` AND (instr(lower(c.title),lower(?))>0 OR instr(lower(f.sku),lower(?))>0 OR instr(lower(f.specifications_json),lower(?))>0 OR CAST(c.id AS TEXT)=?)`
		args = append(args, query, query, query, query)
	}
	args = append(args, (page-1)*50)
	rows, err := tx.QueryContext(ctx, `SELECT `+publicOfferColumns+availableOfferFrom+` WHERE `+availableOfferWhere+filter+` ORDER BY c.id DESC,f.id LIMIT 51 OFFSET ?`, args...)
	if err != nil {
		return nil, false, err
	}
	items := []Offer{}
	for rows.Next() {
		o, err := scanOffer(rows)
		if err != nil {
			rows.Close()
			return nil, false, err
		}
		items = append(items, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, false, err
	}
	more := len(items) > 50
	if more {
		items = items[:50]
	}
	for i, o := range items {
		if o.Currency != a.Currency && (!pricing.Enabled || !pricing.CurrencyEnabled(a.Currency)) {
			items[i].PricingUnavailable = true
			continue
		}
		items[i], err = quoteOffer(ctx, tx, site, pricing, o, a.Currency)
		if err != nil {
			return nil, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return items, more, nil
}
