package commerce

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/mail"
	"strings"

	"gocms/internal/payment"
)

// Manual orders carry their own immutable snapshots; they never create or
// change catalog offers or infer product meaning from a category or theme.
type ManualOrderItem struct {
	VariantID int64  `json:"variant_id,omitempty"`
	Currency  string `json:"currency,omitempty"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Price     string `json:"price"`
	Quantity  int64  `json:"quantity"`
	ImageURL  string `json:"image_url"`
}

type ManualOrderInput struct {
	AccountID    int64             `json:"account_id"`
	CustomerName string            `json:"customer_name"`
	Email        string            `json:"email"`
	Address      Address           `json:"address"`
	Language     string            `json:"language"`
	Items        []ManualOrderItem `json:"items"`
}

func (s Service) CreateManualOrder(ctx context.Context, site int64, actor, key string, in ManualOrderInput) (Order, error) {
	if !validText(actor, 128) || !validText(key, 128) || len(key) < 16 || !validText(in.CustomerName, 100) || len(in.Email) > 128 || len(in.Language) > 16 || len(in.Items) == 0 || len(in.Items) > 50 {
		return Order{}, ErrInvalid
	}
	email, err := mail.ParseAddress(in.Email)
	if err != nil || email.Address != in.Email || (in.Address.Phone != "" && !validText(in.Address.Phone, 32)) {
		return Order{}, ErrInvalid
	}
	raw, err := json.Marshal(struct {
		Actor string
		Input ManualOrderInput
	}{actor, in})
	if err != nil {
		return Order{}, err
	}
	hash := digest(raw)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Order{}, err
	}
	defer tx.Rollback()
	var existing, oldHash string
	err = tx.QueryRowContext(ctx, `SELECT id,request_hash FROM gocms_commerce_order WHERE site_id=? AND buyer_id=0 AND idempotency_key=?`, site, key).Scan(&existing, &oldHash)
	if err == nil {
		if oldHash != hash {
			return Order{}, ErrConflict
		}
		if err := tx.Commit(); err != nil {
			return Order{}, err
		}
		return s.Order(ctx, site, existing)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Order{}, err
	}
	a, err := scanAccount(tx.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM gocms_payment_account WHERE site_id=? AND id=?`, site, in.AccountID))
	if err != nil {
		return Order{}, err
	}
	if !a.Enabled {
		return Order{}, ErrInvalid
	}
	if _, _, err := s.provider(a); err != nil {
		return Order{}, err
	}
	var total int64
	prices := make([]int64, len(in.Items))
	lines := make([]Offer, len(in.Items))
	for i, item := range in.Items {
		if item.VariantID < 0 || (item.Currency != "" && item.Currency != a.Currency) {
			return Order{}, ErrInvalid
		}
		line := Offer{Name: item.Name, Kind: item.Kind, ImageURL: item.ImageURL, Specifications: []Specification{}}
		if item.VariantID > 0 {
			line, err = scanOffer(tx.QueryRowContext(ctx, `SELECT `+publicOfferColumns+availableOfferFrom+` WHERE `+availableOfferWhere+` AND f.id=?`, site, item.VariantID))
			if errors.Is(err, sql.ErrNoRows) {
				return Order{}, ErrNotFound
			}
			if err != nil {
				return Order{}, err
			}
		}
		price, err := payment.ParseAmount(item.Price, a.Currency)
		if err != nil || price <= 0 || item.Quantity < 1 || item.Quantity > 1000 || !validText(line.Name, 128) || (line.Kind != "physical" && line.Kind != "digital" && line.Kind != "service") || price > 99999999999/item.Quantity {
			return Order{}, ErrInvalid
		}
		if valid, err := validOfferImage(ctx, tx, site, line.ImageURL); err != nil {
			return Order{}, err
		} else if !valid || len(line.ImageURL) > 255 {
			return Order{}, ErrInvalid
		}
		if line.Kind == "physical" && (!validText(in.Address.Phone, 32) || !validText(in.Address.Street, 256) || !validText(in.Address.Postcode, 16) || !validText(in.Address.City, 50) || !validCountry(in.Address.Country) || len(in.Address.State) > 128) {
			return Order{}, ErrInvalid
		}
		if total > 99999999999-price*item.Quantity {
			return Order{}, ErrInvalid
		}
		total += price * item.Quantity
		prices[i] = price
		lines[i] = line
	}
	id, err := randomID("ord_")
	if err != nil {
		return Order{}, err
	}
	address, err := json.Marshal(in.Address)
	if err != nil {
		return Order{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO gocms_commerce_order(id,site_id,buyer_id,idempotency_key,request_hash,amount,currency,email,customer_name,address_json,language,source,payment_account_id) VALUES(?,?,0,?,?,?,?,?,?,?,?,'admin',?)`, id, site, key, hash, total, a.Currency, in.Email, in.CustomerName, string(address), in.Language, a.ID)
	if err != nil {
		return Order{}, err
	}
	for i, item := range in.Items {
		line := lines[i]
		specifications, err := json.Marshal(line.Specifications)
		if err != nil {
			return Order{}, err
		}
		// Manual lines have no catalog reference; selected variants retain their
		// identity and specifications alongside the negotiated order price.
		_, err = tx.ExecContext(ctx, `INSERT INTO gocms_commerce_order_item(order_id,line_number,offer_id,name,kind,unit_price,quantity,image_url,content_id,sku,specifications_json) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, id, i+1, line.ID, line.Name, line.Kind, prices[i], item.Quantity, line.ImageURL, line.ContentID, line.SKU, string(specifications))
		if err != nil {
			return Order{}, err
		}
	}
	if err := orderEvent(ctx, tx, id, actor, "order.created", map[string]any{"source": "admin", "amount": total, "currency": a.Currency, "account_id": a.ID}); err != nil {
		return Order{}, err
	}
	if err := tx.Commit(); err != nil {
		return Order{}, err
	}
	return s.Order(ctx, site, id)
}

func validCountry(country string) bool {
	return len(country) == 2 && country[0] >= 'A' && country[0] <= 'Z' && country[1] >= 'A' && country[1] <= 'Z'
}

// IssueOrderLink rotates the bearer credential. Only its hash is persisted;
// existing links are revoked and neither audit records nor order JSON contain it.
func (s Service) IssueOrderLink(ctx context.Context, site int64, id, actor string, revision int64) (string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	token := hex.EncodeToString(secret)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var publicURL string
	err = tx.QueryRowContext(ctx, `SELECT a.public_url FROM gocms_commerce_order o JOIN gocms_payment_account a ON a.id=o.payment_account_id AND a.site_id=o.site_id WHERE o.id=? AND o.site_id=? AND o.source='admin' AND a.enabled=1`, id, site).Scan(&publicURL)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	r, err := tx.ExecContext(ctx, `UPDATE gocms_commerce_order SET payment_token_hash=?,revision=revision+1,updated_at=CURRENT_TIMESTAMP WHERE id=? AND site_id=? AND status='unpaid' AND revision=?`, digest([]byte(token)), id, site, revision)
	if err != nil {
		return "", err
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return "", ErrConflict
	}
	if err := orderEvent(ctx, tx, id, actor, "order.payment_link_issued", map[string]any{"previous_link_revoked": true}); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return publicURL + "/shop/pay/" + id + "#token=" + token, nil
}

func (s Service) LinkedOrder(ctx context.Context, site int64, id, token string) (Order, error) {
	if len(token) != 64 || strings.TrimSpace(token) != token {
		return Order{}, ErrNotFound
	}
	var hash string
	err := s.DB.QueryRowContext(ctx, `SELECT payment_token_hash FROM gocms_commerce_order WHERE site_id=? AND id=? AND source='admin'`, site, id).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, err
	}
	if hash == "" || subtle.ConstantTimeCompare([]byte(hash), []byte(digest([]byte(token)))) != 1 {
		return Order{}, ErrNotFound
	}
	return s.Order(ctx, site, id)
}
