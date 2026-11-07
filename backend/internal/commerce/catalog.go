// Package commerce owns content trading configuration, orders and payment
// orchestration without depending on category names or theme presentation rules.
package commerce

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"path"
	"strings"

	"gocms/internal/payment"
)

var ErrInvalid = errors.New("invalid commerce input")
var ErrNotFound = errors.New("commerce record not found")
var ErrConflict = errors.New("commerce state conflict")
var ErrPriceChanged = errors.New("commerce price changed")

type Service struct {
	DB        *sql.DB
	Cipher    *payment.Cipher
	Providers map[string]payment.Provider
}

type Offer struct {
	ID             int64           `json:"id"`
	Name           string          `json:"name"`
	Kind           string          `json:"kind"`
	Amount         int64           `json:"amount"`
	Currency       string          `json:"currency"`
	ImageURL       string          `json:"image_url"`
	ContentID      *int64          `json:"content_id"`
	Enabled        bool            `json:"enabled"`
	SKU            string          `json:"sku"`
	Specifications []Specification `json:"specifications"`
}

type Account struct {
	ID          int64  `json:"id"`
	SiteID      int64  `json:"-"`
	PublicID    string `json:"public_id"`
	Name        string `json:"name"`
	Provider    string `json:"provider"`
	Environment string `json:"environment"`
	ClientID    string `json:"client_id"`
	MerchantID  string `json:"merchant_id"`
	Currency    string `json:"currency"`
	PublicURL   string `json:"public_url"`
	ReturnPath  string `json:"return_path"`
	Enabled     bool   `json:"enabled"`
	Secret      string `json:"-"`
	Ciphertext  string `json:"-"`
}

func randomID(prefix string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b), nil
}

func digest(value []byte) string { d := sha256.Sum256(value); return hex.EncodeToString(d[:]) }

func validText(value string, max int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= max && !strings.ContainsAny(value, "\x00\r\n")
}

func secureURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Fragment == "" && u.RawQuery == ""
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s Service) validOfferImage(ctx context.Context, site int64, raw string) (bool, error) {
	return validOfferImage(ctx, s.DB, site, raw)
}

func validOfferImage(ctx context.Context, store rowQuerier, site int64, raw string) (bool, error) {
	if secureURL(raw) {
		return true, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.ContainsAny(raw, "\\\r\n") || path.Clean(raw) != raw {
		return false, nil
	}
	var count int
	err = store.QueryRowContext(ctx, `SELECT COUNT(*) FROM gocms_media WHERE site_id=? AND public_path=? AND kind='image' AND status='active'`, site, raw).Scan(&count)
	return count == 1, err
}

func (s Service) SaveOffer(ctx context.Context, site int64, o Offer) (Offer, error) {
	o.Name = strings.TrimSpace(o.Name)
	if !validText(o.Name, 128) || (o.Kind != "physical" && o.Kind != "digital" && o.Kind != "service") || o.Amount <= 0 || len(o.ImageURL) > 255 {
		return o, ErrInvalid
	}
	if valid, err := s.validOfferImage(ctx, site, o.ImageURL); err != nil {
		return o, err
	} else if !valid {
		return o, ErrInvalid
	}
	if _, err := payment.FormatAmount(o.Amount, o.Currency); err != nil {
		return o, ErrInvalid
	}
	if o.ContentID != nil {
		var n int
		if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM gocms_content WHERE id=? AND site_id=?`, *o.ContentID, site).Scan(&n); err != nil {
			return o, err
		}
		if n != 1 {
			return o, ErrInvalid
		}
	}
	if o.ID == 0 {
		r, err := s.DB.ExecContext(ctx, `INSERT INTO gocms_commerce_offer(site_id,name,kind,amount,currency,image_url,content_id,enabled) VALUES(?,?,?,?,?,?,?,?)`, site, o.Name, o.Kind, o.Amount, o.Currency, o.ImageURL, o.ContentID, o.Enabled)
		if err != nil {
			return o, err
		}
		o.ID, err = r.LastInsertId()
		return o, err
	}
	r, err := s.DB.ExecContext(ctx, `UPDATE gocms_commerce_offer SET name=?,kind=?,amount=?,currency=?,image_url=?,content_id=?,enabled=? WHERE id=? AND site_id=?`, o.Name, o.Kind, o.Amount, o.Currency, o.ImageURL, o.ContentID, o.Enabled, o.ID, site)
	if err != nil {
		return o, err
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return o, ErrNotFound
	}
	return o, nil
}

// Offers returns purchasable content variants for the public catalog. Legacy
// standalone offers remain readable by administrators for historical migration.
func (s Service) Offers(ctx context.Context, site int64, public bool, page int, filters ...int64) ([]Offer, error) {
	query := `SELECT f.id,f.name,f.kind,f.amount,f.currency,f.image_url,f.content_id,f.enabled,f.sku,f.specifications_json FROM gocms_commerce_offer f WHERE f.site_id=? AND f.archived=0`
	args := []any{site}
	if public {
		query = `SELECT ` + publicOfferColumns + availableOfferFrom + ` WHERE ` + availableOfferWhere
		if len(filters) > 0 && filters[0] > 0 {
			query += ` AND c.id=?`
			args = append(args, filters[0])
		}
		if len(filters) > 1 && filters[1] > 0 {
			query += ` AND c.category_id=?`
			args = append(args, filters[1])
		}
	}
	query += ` ORDER BY f.content_id DESC,f.id LIMIT 50 OFFSET ?`
	args = append(args, (page-1)*50)
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Offer{}
	for rows.Next() {
		o, err := scanOffer(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, o)
	}
	return items, rows.Err()
}

const accountColumns = `id,site_id,public_id,name,provider,environment,client_id,merchant_id,currency,public_url,return_path,enabled,secret_ciphertext`

type scanner interface{ Scan(...any) error }

func scanAccount(row scanner) (Account, error) {
	var a Account
	err := row.Scan(&a.ID, &a.SiteID, &a.PublicID, &a.Name, &a.Provider, &a.Environment, &a.ClientID, &a.MerchantID, &a.Currency, &a.PublicURL, &a.ReturnPath, &a.Enabled, &a.Ciphertext)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return a, err
}

func (s Service) Accounts(ctx context.Context, site int64) ([]Account, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+accountColumns+` FROM gocms_payment_account WHERE site_id=? ORDER BY id DESC LIMIT 100`, site)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Account{}
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	return items, rows.Err()
}

func (s Service) CreateAccount(ctx context.Context, site int64, a Account) (Account, error) {
	a.PublicURL = strings.TrimRight(a.PublicURL, "/")
	if a.ReturnPath == "" {
		a.ReturnPath = "/"
	}
	returnURL, parseErr := url.Parse(a.ReturnPath)
	if parseErr != nil || !strings.HasPrefix(a.ReturnPath, "/") || strings.HasPrefix(a.ReturnPath, "//") || returnURL.Host != "" || returnURL.Scheme != "" || returnURL.RawQuery != "" || returnURL.Fragment != "" || strings.ContainsAny(a.ReturnPath, "\\\r\n") || len(a.ReturnPath) > 100 {
		return a, ErrInvalid
	}
	if !validText(a.Name, 80) || !secureURL(a.PublicURL) || len(a.PublicURL) > 150 {
		return a, ErrInvalid
	}
	u, _ := url.Parse(a.PublicURL)
	if u.Path != "" {
		return a, ErrInvalid
	}
	if _, err := payment.Exponent(a.Currency); err != nil {
		return a, ErrInvalid
	}
	p := s.Providers[a.Provider]
	if p == nil {
		return a, ErrInvalid
	}
	if err := p.Validate(payment.Account{Environment: a.Environment, ClientID: a.ClientID, MerchantID: a.MerchantID, Secret: a.Secret}); err != nil {
		return a, ErrInvalid
	}
	var err error
	a.PublicID, err = randomID("acct_")
	if err != nil {
		return a, err
	}
	a.Ciphertext, err = s.Cipher.Seal(a.Secret, fmt.Sprintf("%d:%s", site, a.PublicID))
	if err != nil {
		return a, err
	}
	r, err := s.DB.ExecContext(ctx, `INSERT INTO gocms_payment_account(site_id,public_id,name,provider,environment,client_id,merchant_id,secret_ciphertext,currency,public_url,return_path,enabled) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, site, a.PublicID, a.Name, a.Provider, a.Environment, a.ClientID, a.MerchantID, a.Ciphertext, a.Currency, a.PublicURL, a.ReturnPath, a.Enabled)
	if err != nil {
		return a, err
	}
	a.ID, err = r.LastInsertId()
	a.SiteID = site
	a.Secret = ""
	return a, err
}

func (s Service) UpdateAccount(ctx context.Context, site, id int64, name string, enabled bool) error {
	if !validText(name, 80) {
		return ErrInvalid
	}
	r, err := s.DB.ExecContext(ctx, `UPDATE gocms_payment_account SET name=?,enabled=? WHERE id=? AND site_id=?`, name, enabled, id, site)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	return nil
}

func (s Service) provider(a Account) (payment.Provider, payment.Account, error) {
	p := s.Providers[a.Provider]
	if p == nil {
		return nil, payment.Account{}, ErrInvalid
	}
	secret, err := s.Cipher.Open(a.Ciphertext, fmt.Sprintf("%d:%s", a.SiteID, a.PublicID))
	return p, payment.Account{Environment: a.Environment, ClientID: a.ClientID, MerchantID: a.MerchantID, Secret: secret}, err
}

type Address struct {
	Phone    string `json:"phone,omitempty"`
	Street   string `json:"street"`
	Postcode string `json:"postcode"`
	City     string `json:"city"`
	State    string `json:"state"`
	Country  string `json:"country"`
}

type CartItemInput struct {
	VariantID int64 `json:"variant_id"`
	ContentID int64 `json:"content_id,omitempty"`
	Quantity  int64 `json:"quantity"`
}

type OrderLine struct {
	LineNumber      int64 `json:"line_number"`
	ShippedQuantity int64 `json:"shipped_quantity"`
	Offer
	Quantity int64 `json:"quantity"`
}

type OrderInput struct {
	Language       string          `json:"language,omitempty"`
	Items          []CartItemInput `json:"items,omitempty"`
	ExpectedAmount *int64          `json:"expected_amount,omitempty"`
	Currency       string          `json:"currency,omitempty"`
	OfferID        int64           `json:"offer_id,omitempty"`
	VariantID      int64           `json:"variant_id,omitempty"`
	ContentID      int64           `json:"content_id,omitempty"`
	Quantity       int64           `json:"quantity"`
	Email          string          `json:"email"`
	CustomerName   string          `json:"customer_name"`
	Address        Address         `json:"address"`
}

type Order struct {
	Revision          int64       `json:"revision"`
	Shipments         []Shipment  `json:"shipments"`
	Items             []OrderLine `json:"items"`
	ID                string      `json:"id"`
	BuyerID           int64       `json:"buyer_id"`
	Amount            int64       `json:"amount"`
	Currency          string      `json:"currency"`
	Status            string      `json:"status"`
	FulfillmentStatus string      `json:"fulfillment_status"`
	PaidAttempt       string      `json:"paid_attempt"`
	Email             string      `json:"email"`
	CustomerName      string      `json:"customer_name"`
	Address           Address     `json:"address"`
	CreatedAt         string      `json:"created_at"`
	Item              Offer       `json:"item"`
	Quantity          int64       `json:"quantity"`
	Attempts          []Attempt   `json:"attempts,omitempty"`
	Refunds           []Refund    `json:"refunds,omitempty"`
}

func (s Service) CreateOrder(ctx context.Context, site, buyer int64, key string, in OrderInput) (Order, error) {
	if len(in.Language) > 16 || (in.Address.Phone != "" && !validText(in.Address.Phone, 32)) {
		return Order{}, ErrInvalid
	}
	if !validText(key, 128) || len(key) < 16 || buyer <= 0 || !validText(in.CustomerName, 100) || len(in.Email) > 128 {
		return Order{}, ErrInvalid
	}
	email, err := mail.ParseAddress(in.Email)
	if err != nil || email.Address != in.Email {
		return Order{}, ErrInvalid
	}
	encoded, _ := json.Marshal(in)
	hash := digest(encoded)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Order{}, err
	}
	defer tx.Rollback()
	var existing, oldHash string
	err = tx.QueryRowContext(ctx, `SELECT id,request_hash FROM gocms_commerce_order WHERE site_id=? AND buyer_id=? AND idempotency_key=?`, site, buyer, key).Scan(&existing, &oldHash)
	if err == nil {
		if hash != oldHash {
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

	inputs := in.Items
	if len(inputs) == 0 {
		variantID := in.VariantID
		if variantID == 0 {
			variantID = in.OfferID
		}
		if in.OfferID != 0 && in.VariantID != 0 && in.OfferID != in.VariantID {
			return Order{}, ErrInvalid
		}
		inputs = []CartItemInput{{VariantID: variantID, ContentID: in.ContentID, Quantity: in.Quantity}}
	} else if in.OfferID != 0 || in.VariantID != 0 || in.ContentID != 0 || in.Quantity != 0 {
		return Order{}, ErrInvalid
	}
	if len(inputs) > 50 {
		return Order{}, ErrInvalid
	}
	lines := make([]OrderLine, 0, len(inputs))
	seen := map[int64]bool{}
	var amount int64
	currency := ""
	for _, input := range inputs {
		if input.VariantID <= 0 || input.Quantity < 1 || input.Quantity > 1000 || seen[input.VariantID] {
			return Order{}, ErrInvalid
		}
		seen[input.VariantID] = true
		o, err := scanOffer(tx.QueryRowContext(ctx, `SELECT `+publicOfferColumns+availableOfferFrom+` WHERE `+availableOfferWhere+` AND f.id=?`, site, input.VariantID))
		if errors.Is(err, sql.ErrNoRows) {
			return Order{}, ErrNotFound
		}
		if err != nil {
			return Order{}, err
		}
		if input.ContentID != 0 && (o.ContentID == nil || input.ContentID != *o.ContentID) {
			return Order{}, ErrInvalid
		}
		if valid, err := validOfferImage(ctx, tx, site, o.ImageURL); err != nil {
			return Order{}, err
		} else if !valid || len(o.ImageURL) > 255 || !validText(o.Name, 128) {
			return Order{}, ErrInvalid
		}
		if o.Kind == "physical" && (!validText(in.Address.Street, 256) || !validText(in.Address.Postcode, 16) || !validText(in.Address.City, 50) || len(in.Address.Country) != 2 || len(in.Address.State) > 128) {
			return Order{}, ErrInvalid
		}
		if currency != "" && currency != o.Currency {
			return Order{}, ErrInvalid
		}
		currency = o.Currency
		if o.Amount <= 0 || o.Amount > 99999999999/input.Quantity {
			return Order{}, ErrInvalid
		}
		lineAmount := o.Amount * input.Quantity
		if amount > 99999999999-lineAmount {
			return Order{}, ErrInvalid
		}
		amount += lineAmount
		lines = append(lines, OrderLine{Offer: o, Quantity: input.Quantity})
	}
	if _, err := payment.FormatAmount(amount, currency); err != nil {
		return Order{}, ErrInvalid
	}
	if (in.ExpectedAmount != nil && *in.ExpectedAmount != amount) || (in.Currency != "" && in.Currency != currency) {
		return Order{}, ErrPriceChanged
	}
	id, err := randomID("ord_")
	if err != nil {
		return Order{}, err
	}
	address, _ := json.Marshal(in.Address)
	_, err = tx.ExecContext(ctx, `INSERT INTO gocms_commerce_order(id,site_id,buyer_id,idempotency_key,request_hash,amount,currency,email,customer_name,address_json,language) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, id, site, buyer, key, hash, amount, currency, in.Email, in.CustomerName, string(address), in.Language)
	if err != nil {
		return Order{}, err
	}

	for index, line := range lines {
		specifications, _ := json.Marshal(line.Specifications)
		if _, err := tx.ExecContext(ctx, `INSERT INTO gocms_commerce_order_item(order_id,line_number,offer_id,name,kind,unit_price,quantity,image_url,content_id,sku,specifications_json) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, id, index+1, line.ID, line.Name, line.Kind, line.Amount, line.Quantity, line.ImageURL, line.ContentID, line.SKU, string(specifications)); err != nil {
			return Order{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Order{}, err
	}
	return s.Order(ctx, site, id)
}
