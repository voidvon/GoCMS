// Package payment defines provider-independent payment operations. Providers do
// not update orders or grant entitlements; the commerce service owns those steps.
package payment

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

var ErrInvalid = errors.New("invalid payment data")
var ErrUnavailable = errors.New("payment provider unavailable; synchronize before retrying")

type Account struct {
	Environment string
	ClientID    string
	MerchantID  string
	Secret      string
}

type CheckoutItem struct {
	Name      string
	Kind      string
	Quantity  int64
	UnitPrice int64
	ImageURL  string
}

type Checkout struct {
	Items        []CheckoutItem
	Reference    string
	Amount       int64
	Currency     string
	Name         string
	Kind         string
	Quantity     int64
	UnitPrice    int64
	ImageURL     string
	Email        string
	CustomerName string
	Address      Address
	IP           string
	NotifyURL    string
	ReturnURL    string
}

type Address struct {
	Street   string `json:"street"`
	Postcode string `json:"postcode"`
	City     string `json:"city"`
	State    string `json:"state"`
	Country  string `json:"country"`
}

type Result struct {
	Reference       string
	TransactionID   string
	RefundReference string
	RefundID        string
	Amount          int64
	Currency        string
	Status          string
	PaymentURL      string
	Kind            string
}

type Refund struct {
	Reference        string
	PaymentReference string
	Amount           int64
	Currency         string
	NotifyURL        string
}

type Provider interface {
	Validate(Account) error
	Create(context.Context, Account, Checkout) (Result, error)
	Query(context.Context, Account, string) (Result, error)
	Refund(context.Context, Account, Refund) (Result, error)
	QueryRefund(context.Context, Account, string, string) (Result, error)
	Notification(Account, []byte) (Result, error)
}

// The first release deliberately supports a bounded set of currencies. Adding
// currencies requires confirming both their exponent and provider availability.
func Exponent(currency string) (int, error) {
	switch currency {
	case "USD", "EUR", "GBP", "HKD", "CNY", "AUD", "CAD", "CHF", "SGD", "NZD":
		return 2, nil
	case "JPY":
		return 0, nil
	default:
		return 0, ErrInvalid
	}
}

func ParseAmount(value, currency string) (int64, error) {
	exponent, err := Exponent(currency)
	if err != nil {
		return 0, err
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, ErrInvalid
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	// PingPong returns six decimal places even for two-decimal currencies.
	if len(fraction) > exponent {
		if strings.Trim(fraction[exponent:], "0") != "" {
			return 0, ErrInvalid
		}
		fraction = fraction[:exponent]
	}
	digits := parts[0] + fraction + strings.Repeat("0", exponent-len(fraction))
	for _, c := range digits {
		if c < '0' || c > '9' {
			return 0, ErrInvalid
		}
	}
	amount, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || amount > 99999999999 {
		return 0, ErrInvalid
	}
	return amount, nil
}

func FormatAmount(amount int64, currency string) (string, error) {
	e, err := Exponent(currency)
	if err != nil || amount < 0 || amount > 99999999999 {
		return "", ErrInvalid
	}
	if e == 0 {
		return strconv.FormatInt(amount, 10), nil
	}
	return fmt.Sprintf("%d.%02d", amount/100, amount%100), nil
}

// Cipher encrypts merchant credentials with a deployment key managed separately
// from the database. The zero value can be initialized once at runtime.
type Cipher struct {
	mu   sync.RWMutex
	aead cipher.AEAD
}

func (c *Cipher) Configured() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.aead != nil
}

// Initialize publishes a key once without racing with in-flight payment work.
// Rotation requires re-encrypting existing credentials and is a separate operation.
func (c *Cipher) Initialize(key string) error {
	next, err := NewCipher(key)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.aead != nil {
		return errors.New("payment key is already configured")
	}
	c.aead = next.aead
	return nil
}

func NewCipher(key string) (*Cipher, error) {
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(raw) != 32 {
		return nil, errors.New("GOCMS_PAYMENT_KEY must be a base64-encoded 32-byte key")
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

func (c *Cipher) Seal(secret, scope string) (string, error) {
	if c == nil {
		return "", errors.New("configure GOCMS_PAYMENT_KEY before saving payment accounts")
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.aead == nil {
		return "", errors.New("payment key unavailable")
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(c.aead.Seal(nonce, nonce, []byte(secret), []byte(scope))), nil
}

func (c *Cipher) Open(value, scope string) (string, error) {
	if c == nil {
		return "", errors.New("payment key unavailable")
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.aead == nil {
		return "", errors.New("payment key unavailable")
	}
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(raw) < c.aead.NonceSize() {
		return "", ErrInvalid
	}
	plain, err := c.aead.Open(nil, raw[:c.aead.NonceSize()], raw[c.aead.NonceSize():], []byte(scope))
	if err != nil {
		return "", errors.New("unable to decrypt payment account")
	}
	return string(plain), nil
}
