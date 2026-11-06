package commerce

import (
	"context"
	"strings"
)

// Variants resolves cart IDs through the same availability rules used at checkout.
// Missing or disabled variants are omitted so the client can mark stale rows.
func (s Service) Variants(ctx context.Context, site int64, ids []int64) ([]Offer, error) {
	if len(ids) == 0 || len(ids) > 50 {
		return nil, ErrInvalid
	}
	placeholders := make([]string, len(ids))
	args := []any{site}
	for i, id := range ids {
		if id <= 0 {
			return nil, ErrInvalid
		}
		placeholders[i] = "?"
		args = append(args, id)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+publicOfferColumns+availableOfferFrom+` WHERE `+availableOfferWhere+` AND f.id IN (`+strings.Join(placeholders, ",")+`) ORDER BY f.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Offer{}
	for rows.Next() {
		offer, err := scanOffer(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, offer)
	}
	return result, rows.Err()
}
