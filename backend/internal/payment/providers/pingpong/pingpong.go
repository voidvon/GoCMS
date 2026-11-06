// Package pingpong implements PingPong Checkout v4 using SHA256 signatures.
package pingpong

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"gocms/internal/payment"
)

type Provider struct{ Client *http.Client }

var endpoints = map[string]string{
	"sandbox":  "https://sandbox-acquirer-payment.pingpongx.com",
	"prod-fra": "https://acquirer-payment.pingpongx.com",
	"prod-sg":  "https://acquirer-payment-checkout-sg.pingpongx.com",
	"prod-us":  "https://acquirer-payment-checkout-us.pingpongx.com",
}

func (p Provider) Validate(a payment.Account) error {
	if endpoints[a.Environment] == "" || a.ClientID == "" || a.MerchantID == "" || a.Secret == "" || len(a.ClientID) > 64 || len(a.MerchantID) > 64 || len(a.Secret) > 1024 {
		return payment.ErrInvalid
	}
	return nil
}

// Sign matches the official v4 algorithm: nonempty top-level string values,
// sorted by key, prefixed by the salt. bizContent is signed as the original JSON
// string, without decoding and re-encoding it.
func Sign(fields map[string]string, salt string) string {
	keys := make([]string, 0, len(fields))
	for k, v := range fields {
		if k != "sign" && strings.TrimSpace(v) != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+fields[k])
	}
	digest := sha256.Sum256([]byte(salt + strings.Join(parts, "&")))
	return strings.ToUpper(hex.EncodeToString(digest[:]))
}

func decode(a payment.Account, raw []byte) (map[string]json.RawMessage, error) {
	var fields map[string]string
	if json.Unmarshal(raw, &fields) != nil || fields["signType"] != "SHA256" || fields["accId"] != a.MerchantID || fields["clientId"] != a.ClientID {
		return nil, payment.ErrInvalid
	}
	want := Sign(fields, a.Secret)
	if subtle.ConstantTimeCompare([]byte(want), []byte(strings.ToUpper(fields["sign"]))) != 1 {
		return nil, payment.ErrInvalid
	}
	if fields["code"] != "000000" && fields["code"] != "001000" {
		return nil, errors.New("PingPong rejected request (code " + safeCode(fields["code"]) + ")")
	}
	var business map[string]json.RawMessage
	if json.Unmarshal([]byte(fields["bizContent"]), &business) != nil {
		return nil, payment.ErrInvalid
	}
	return business, nil
}

func safeCode(s string) string {
	if len(s) > 12 {
		return "unknown"
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return "unknown"
		}
	}
	return s
}

func (p Provider) call(ctx context.Context, a payment.Account, path string, business any) (map[string]json.RawMessage, error) {
	if err := p.Validate(a); err != nil {
		return nil, err
	}
	biz, err := json.Marshal(business)
	if err != nil {
		return nil, err
	}
	fields := map[string]string{"accId": a.MerchantID, "clientId": a.ClientID, "signType": "SHA256", "version": "1.0", "bizContent": string(biz)}
	fields["sign"] = Sign(fields, a.Secret)
	body, _ := json.Marshal(fields)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoints[a.Environment]+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, payment.ErrUnavailable
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil || response.StatusCode != http.StatusOK {
		return nil, payment.ErrUnavailable
	}
	return decode(a, raw)
}

func str(m map[string]json.RawMessage, key string) string {
	var s string
	if json.Unmarshal(m[key], &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(m[key], &n) == nil {
		return n.String()
	}
	return ""
}

func result(m map[string]json.RawMessage, kind string) (payment.Result, error) {
	r := payment.Result{Reference: str(m, "merchantTransactionId"), TransactionID: str(m, "transactionId"), RefundReference: str(m, "merchantRefundId"), RefundID: str(m, "refundId"), Currency: str(m, "currency"), Kind: kind}
	var err error
	r.Amount, err = payment.ParseAmount(str(m, "amount"), r.Currency)
	if err != nil || r.Reference == "" || r.TransactionID == "" {
		return r, payment.ErrInvalid
	}
	switch str(m, "status") {
	case "SUCCESS":
		r.Status = "succeeded"
	case "FAILED":
		r.Status = "failed"
	case "CLOSED", "CANCEL":
		r.Status = "closed"
	case "INIT", "PROCESSING", "PENDING":
		r.Status = "pending"
	default:
		return r, payment.ErrInvalid
	}
	return r, nil
}

func (p Provider) Create(ctx context.Context, a payment.Account, c payment.Checkout) (payment.Result, error) {
	amount, err := payment.FormatAmount(c.Amount, c.Currency)
	if err != nil {
		return payment.Result{}, err
	}
	business := map[string]any{"merchantTransactionId": c.Reference, "amount": amount, "currency": c.Currency, "captureDelayHours": 0, "notificationUrl": c.NotifyURL, "closeNotificationUrl": c.NotifyURL, "payResultUrl": c.ReturnURL, "payCancelUrl": c.ReturnURL, "shopperIP": c.IP}
	names := strings.Fields(c.CustomerName)
	first, last := c.CustomerName, c.CustomerName
	if len(names) > 1 {
		first = names[0]
		last = strings.Join(names[1:], " ")
	}
	business["customer"] = map[string]string{"email": c.Email, "firstName": first, "lastName": last}

	items := c.Items
	if len(items) == 0 {
		items = []payment.CheckoutItem{{Name: c.Name, Kind: c.Kind, Quantity: c.Quantity, UnitPrice: c.UnitPrice, ImageURL: c.ImageURL}}
	}
	if len(items) > 50 {
		return payment.Result{}, payment.ErrInvalid
	}
	goods := make([]map[string]any, 0, len(items))
	for _, item := range items {
		unit, err := payment.FormatAmount(item.UnitPrice, c.Currency)
		if err != nil || item.UnitPrice <= 0 || item.Quantity < 1 {
			return payment.Result{}, payment.ErrInvalid
		}
		virtual, itemType := "Y", "3"
		if item.Kind == "physical" {
			virtual, itemType = "N", "2"
			business["shippingAddress"] = c.Address
			business["billingAddress"] = c.Address
		}
		if item.Kind == "digital" {
			itemType = "1"
		}
		goods = append(goods, map[string]any{"name": item.Name, "unitPrice": unit, "number": strconv.FormatInt(item.Quantity, 10), "imgUrl": item.ImageURL, "virtualProduct": virtual, "itemType": itemType})
	}
	business["goods"] = goods
	m, err := p.call(ctx, a, "/v4/payment/prePay", business)
	if err != nil {
		return payment.Result{}, err
	}
	r := payment.Result{Reference: str(m, "merchantTransactionId"), TransactionID: str(m, "transactionId"), PaymentURL: str(m, "paymentUrl"), Currency: str(m, "currency"), Status: "pending", Kind: "payment"}
	r.Amount, err = payment.ParseAmount(str(m, "amount"), r.Currency)
	u, parseErr := url.Parse(r.PaymentURL)
	if err != nil || parseErr != nil || u.Scheme != "https" || !strings.HasSuffix(u.Hostname(), ".pingpongx.com") || u.User != nil || u.Port() != "" || r.TransactionID == "" {
		return r, payment.ErrInvalid
	}
	return r, nil
}

func (p Provider) Query(ctx context.Context, a payment.Account, ref string) (payment.Result, error) {
	m, err := p.call(ctx, a, "/v4/payment/query", map[string]string{"merchantTransactionId": ref})
	if err != nil {
		return payment.Result{}, err
	}
	return result(m, "payment")
}

func (p Provider) Refund(ctx context.Context, a payment.Account, r payment.Refund) (payment.Result, error) {
	amount, err := payment.FormatAmount(r.Amount, r.Currency)
	if err != nil {
		return payment.Result{}, err
	}
	m, err := p.call(ctx, a, "/v4/payment/refund", map[string]string{"merchantTransactionId": r.PaymentReference, "merchantRefundId": r.Reference, "amount": amount, "currency": r.Currency, "notificationUrl": r.NotifyURL})
	if err != nil {
		return payment.Result{}, err
	}
	return result(m, "refund")
}

func (p Provider) QueryRefund(ctx context.Context, a payment.Account, ref, original string) (payment.Result, error) {
	m, err := p.call(ctx, a, "/v4/refund/query", map[string]string{"merchantRefundId": ref, "merchantTransactionId": original})
	if err != nil {
		return payment.Result{}, err
	}
	return result(m, "refund")
}

func (p Provider) Notification(a payment.Account, raw []byte) (payment.Result, error) {
	m, err := decode(a, raw)
	if err != nil {
		return payment.Result{}, err
	}
	kind := "payment"
	switch str(m, "notifyType") {
	case "RECHARGE", "CLOSE":
	case "REFUND":
		kind = "refund"
	default:
		return payment.Result{}, payment.ErrInvalid
	}
	return result(m, kind)
}
