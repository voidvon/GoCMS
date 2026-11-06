package pingpong

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"gocms/internal/payment"
)

func account() payment.Account {
	return payment.Account{Environment: "sandbox", ClientID: "client", MerchantID: "shop", Secret: "salt"}
}

func signed(t *testing.T, business map[string]any) []byte {
	t.Helper()
	biz, err := json.Marshal(business)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{"accId": "shop", "clientId": "client", "code": "000000", "description": "Transaction succeeded", "bizContent": string(biz), "signType": "SHA256"}
	m["sign"] = Sign(m, "salt")
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestCanonicalSignature(t *testing.T) {
	m := map[string]string{"version": "1.0", "bizContent": "{\"b\":2, \"a\":1}", "clientId": "client", "accId": "shop", "signType": "SHA256", "empty": "", "sign": "ignored"}
	d := sha256.Sum256([]byte(`saltaccId=shop&bizContent={"b":2, "a":1}&clientId=client&signType=SHA256&version=1.0`))
	if got := Sign(m, "salt"); got != strings.ToUpper(hex.EncodeToString(d[:])) {
		t.Fatalf("unexpected signature %s", got)
	}
}

func TestNotificationValidationAndStateMapping(t *testing.T) {
	p := Provider{}
	for _, tc := range []struct{ status, want string }{{"SUCCESS", "succeeded"}, {"FAILED", "failed"}, {"PROCESSING", "pending"}, {"CLOSED", "closed"}, {"CANCEL", "closed"}} {
		raw := signed(t, map[string]any{"merchantTransactionId": "pay_1", "transactionId": "tx_1", "amount": "10.000000", "currency": "USD", "status": tc.status, "notifyType": "RECHARGE"})
		r, err := p.Notification(account(), raw)
		if err != nil || r.Status != tc.want || r.Amount != 1000 {
			t.Fatalf("%s: %+v %v", tc.status, r, err)
		}
		bad := strings.Replace(string(raw), "10.000000", "11.000000", 1)
		if _, err := p.Notification(account(), []byte(bad)); err == nil {
			t.Fatal("tampered amount accepted")
		}
		a := account()
		a.MerchantID = "other"
		if _, err := p.Notification(a, raw); err == nil {
			t.Fatal("wrong merchant accepted")
		}
	}
	for _, bad := range []map[string]any{
		{"merchantTransactionId": "x", "transactionId": "t", "amount": "1.001", "currency": "USD", "status": "SUCCESS", "notifyType": "RECHARGE"},
		{"merchantTransactionId": "x", "transactionId": "t", "amount": "1", "currency": "USD", "status": "AUTH_SUCCESS", "notifyType": "RECHARGE"},
		{"merchantTransactionId": "x", "transactionId": "t", "amount": "1", "currency": "USD", "status": "SUCCESS", "notifyType": "TOKEN"},
	} {
		if _, err := p.Notification(account(), signed(t, bad)); err == nil {
			t.Fatal("invalid notification accepted")
		}
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHostedCheckoutAndRefundWireContract(t *testing.T) {
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "sandbox-acquirer-payment.pingpongx.com" {
			t.Fatal("unexpected endpoint")
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]string
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		if fields["sign"] != Sign(fields, "salt") {
			t.Fatal("invalid request signature")
		}
		var biz map[string]any
		if err := json.Unmarshal([]byte(fields["bizContent"]), &biz); err != nil {
			t.Fatal(err)
		}
		var response []byte
		switch r.URL.Path {
		case "/v4/payment/prePay":
			if biz["amount"] != "10.00" || biz["captureDelayHours"] != float64(0) {
				t.Fatal("incorrect payment amount or capture mode")
			}
			goods := biz["goods"].([]any)[0].(map[string]any)
			if goods["itemType"] != "2" || goods["virtualProduct"] != "N" {
				t.Fatal("physical item sent as virtual")
			}
			if biz["shippingAddress"] == nil || biz["billingAddress"] == nil {
				t.Fatal("physical address missing")
			}
			response = signed(t, map[string]any{"merchantTransactionId": "pay_1", "transactionId": "tx_1", "amount": "10", "currency": "USD", "paymentUrl": "https://sandbox-safepay.pingpongx.com?token=opaque"})
		case "/v4/payment/refund":
			if biz["amount"] != "10.00" || biz["merchantRefundId"] != "ref_1" {
				t.Fatal("incorrect refund wire format")
			}
			response = signed(t, map[string]any{"merchantTransactionId": "pay_1", "transactionId": "tx_1", "merchantRefundId": "ref_1", "refundId": "refund_tx", "amount": "10.000000", "currency": "USD", "status": "PROCESSING"})
		case "/v4/refund/query":
			if biz["merchantTransactionId"] != "pay_1" || biz["merchantRefundId"] != "ref_1" {
				t.Fatal("refund query identity missing")
			}
			response = signed(t, map[string]any{"merchantTransactionId": "pay_1", "transactionId": "tx_1", "amount": "10.000000", "currency": "USD", "status": "SUCCESS"})
		default:
			t.Fatalf("unexpected API %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(response))), Header: make(http.Header)}, nil
	})}
	p := Provider{Client: client}
	r, err := p.Create(context.Background(), account(), payment.Checkout{Reference: "pay_1", Amount: 1000, Currency: "USD", UnitPrice: 500, Quantity: 2, Kind: "physical", CustomerName: "Jane Doe", Address: payment.Address{Street: "Street", Country: "US"}})
	if err != nil || r.TransactionID != "tx_1" || r.PaymentURL == "" {
		t.Fatalf("checkout: %+v %v", r, err)
	}
	r, err = p.Refund(context.Background(), account(), payment.Refund{Reference: "ref_1", PaymentReference: "pay_1", Amount: 1000, Currency: "USD"})
	if err != nil || r.Status != "pending" {
		t.Fatalf("refund: %+v %v", r, err)
	}
	r, err = p.QueryRefund(context.Background(), account(), "ref_1", "pay_1")
	if err != nil || r.Status != "succeeded" {
		t.Fatalf("refund query: %+v %v", r, err)
	}
}
