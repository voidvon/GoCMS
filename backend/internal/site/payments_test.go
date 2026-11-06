package site

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gocms/internal/commerce"
	"gocms/internal/payment/providers/pingpong"
)

type paymentTestTransport func(*http.Request) (*http.Response, error)

func (f paymentTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPaymentHTTPCheckoutWebhookRefundAndOwnership(t *testing.T) {
	t.Setenv("GOCMS_PAYMENT_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	s, database, root := newCategoryTestServer(t)
	admin := func(method, path, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		w := categoryRequest(t, s, root, method, path, body)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w
	}
	accountResponse := admin("POST", "/api/admin/payments/accounts", `{"name":"国际信用卡","provider":"pingpong","environment":"sandbox","client_id":"client","merchant_id":"shop","secret":"test-salt","currency":"USD","public_url":"https://example.com","return_path":"/purchase.html","enabled":true}`, 201)
	var account commerce.Account
	if err := json.Unmarshal(accountResponse.Body.Bytes(), &account); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/admin/payments/accounts", "/api/admin/site-settings"} {
		w := admin("GET", path, "", 200)
		if strings.Contains(w.Body.String(), "test-salt") || strings.Contains(w.Body.String(), "ciphertext") {
			t.Fatal("credential leaked")
		}
	}
	admin("PUT", fmt.Sprintf("/api/admin/payments/accounts/%d", account.ID), `{"name":"immutable","enabled":true,"secret":"changed"}`, 400)
	admin("POST", "/api/admin/categories", `{"name":"可销售服务","commerce_enabled":true}`, 200)
	var categoryID int64
	if err := database.QueryRow(`SELECT id FROM gocms_category WHERE name='可销售服务'`).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}
	admin("POST", "/api/admin/content", fmt.Sprintf(`{"title":"服务预约","category_id":%d,"cover_image":"https://example.com/item.png","visible":1,"commerce":{"enabled":true,"kind":"service","currency":"USD","variants":[{"id":0,"sku":"booking","specifications":[],"amount":1500,"enabled":true}]}}`, categoryID), 200)
	offers, err := s.commerce.Offers(context.Background(), 1, true, 1)
	if err != nil || len(offers) != 1 {
		t.Fatalf("content variants: %+v %v", offers, err)
	}
	offer := offers[0]
	var w *httptest.ResponseRecorder
	sign := func(biz map[string]any) []byte {
		t.Helper()
		b, _ := json.Marshal(biz)
		fields := map[string]string{"accId": "shop", "clientId": "client", "signType": "SHA256", "code": "000000", "bizContent": string(b)}
		fields["sign"] = pingpong.Sign(fields, "test-salt")
		raw, _ := json.Marshal(fields)
		return raw
	}
	s.commerce.Providers["pingpong"] = pingpong.Provider{Client: &http.Client{Transport: paymentTestTransport(func(r *http.Request) (*http.Response, error) {
		var fields map[string]string
		if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
			t.Fatal(err)
		}
		var biz map[string]any
		if err := json.Unmarshal([]byte(fields["bizContent"]), &biz); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(biz["merchantTransactionId"].(string), "pay_") {
			t.Fatal("order ID used as payment attempt ID")
		}
		result := map[string]any{"merchantTransactionId": biz["merchantTransactionId"], "transactionId": "tx_123", "amount": "15.000000", "currency": "USD", "status": "SUCCESS"}
		if r.URL.Path == "/v4/payment/prePay" {
			if biz["payResultUrl"] != "https://example.com/purchase.html?payment_order="+orderIDForTest(t, database) {
				t.Fatal("return page did not follow configured path")
			}
			result["paymentUrl"] = "https://sandbox-safepay.pingpongx.com?token=opaque"
		} else if r.URL.Path == "/v4/payment/refund" {
			result["merchantRefundId"] = biz["merchantRefundId"]
			result["refundId"] = "refund_tx"
			result["status"] = "PROCESSING"
		} else if r.URL.Path == "/v4/refund/query" {
			result["merchantRefundId"] = biz["merchantRefundId"]
			result["refundId"] = "refund_tx"
		} else if r.URL.Path != "/v4/payment/query" {
			t.Fatal("unexpected API")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(sign(result))))}, nil
	})}}
	call := func(method, path, body string, cookie *http.Cookie, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "browser-order-request-key")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w
	}
	catalog := call("GET", "/api/commerce/catalog", "", nil, 200)
	if strings.Contains(catalog.Body.String(), "merchant_id") || strings.Contains(catalog.Body.String(), "client_id") {
		t.Fatal("public merchant metadata leak")
	}
	call("POST", "/api/commerce/orders", `{}`, nil, 401)
	call("POST", "/api/v1/auth/register", `{"username":"buyer","password":"test-password"}`, nil, 201)
	login := call("POST", "/api/v1/auth/login", `{"username":"buyer","password":"test-password"}`, nil, 200)
	cookie := login.Result().Cookies()[0]
	body := fmt.Sprintf(`{"offer_id":%d,"quantity":1,"email":"buyer@example.com","customer_name":"Buyer Name"}`, offer.ID)
	w = call("POST", "/api/commerce/orders", body, cookie, 201)
	var order commerce.Order
	if err := json.Unmarshal(w.Body.Bytes(), &order); err != nil {
		t.Fatal(err)
	}
	call("POST", "/api/commerce/orders", strings.TrimSuffix(body, "}")+`,"amount":1}`, cookie, 400)
	w = call("POST", "/api/commerce/orders/"+order.ID+"/pay", fmt.Sprintf(`{"account_id":%d}`, account.ID), cookie, 200)
	var attempt commerce.Attempt
	if err := json.Unmarshal(w.Body.Bytes(), &attempt); err != nil {
		t.Fatal(err)
	}
	call("POST", "/api/v1/auth/register", `{"username":"otherbuyer","password":"test-password"}`, nil, 201)
	other := call("POST", "/api/v1/auth/login", `{"username":"otherbuyer","password":"test-password"}`, nil, 200).Result().Cookies()[0]
	call("GET", "/api/commerce/orders/"+order.ID, "", other, 404)
	call("POST", "/api/commerce/orders/"+order.ID+"/pay", fmt.Sprintf(`{"account_id":%d}`, account.ID), other, 404)
	biz := map[string]any{"merchantTransactionId": attempt.ID, "transactionId": "tx_123", "amount": "15.000000", "currency": "USD", "status": "SUCCESS", "notifyType": "RECHARGE"}
	raw := sign(biz)
	path := "/api/payments/webhooks/" + account.PublicID
	for i := 0; i < 2; i++ {
		w = call("POST", path, string(raw), nil, 200)
		if w.Body.String() != "OK" {
			t.Fatal("incorrect provider acknowledgment")
		}
	}
	tampered := strings.Replace(string(raw), "15.000000", "99.000000", 1)
	call("POST", path, tampered, nil, 400)
	biz["amount"] = "99.000000"
	call("POST", path, string(sign(biz)), nil, 400)
	w = call("GET", "/api/commerce/orders/"+order.ID, "", cookie, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &order); err != nil || order.Status != "paid" {
		t.Fatal("verified payment not applied")
	}
	if _, err := database.Exec(`INSERT INTO gocms_site(name,code,domain) VALUES('Other','other','other.example')`); err != nil {
		t.Fatal(err)
	}
	admin("GET", "/api/admin/payments/orders/"+order.ID+"?site_id=2", "", 404)
	admin("PUT", "/api/admin/payments/orders/"+order.ID+"/fulfillment", `{"status":"completed"}`, 200)
	admin("POST", "/api/admin/payments/attempts/"+attempt.ID+"/refund", `{}`, 200)
	w = admin("GET", "/api/admin/payments/orders/"+order.ID, "", 200)
	if err := json.Unmarshal(w.Body.Bytes(), &order); err != nil || order.Status != "paid" || len(order.Refunds) != 1 || order.Refunds[0].Status != "pending" {
		t.Fatal("refund acceptance incorrectly marked order refunded")
	}
	refund := order.Refunds[0]
	refundEvent := map[string]any{"merchantTransactionId": attempt.ID, "transactionId": "tx_123", "merchantRefundId": "ref_wrong", "refundId": "refund_tx", "amount": "15.000000", "currency": "USD", "status": "SUCCESS", "notifyType": "REFUND"}
	call("POST", path, string(sign(refundEvent)), nil, 400)
	refundEvent["merchantRefundId"] = refund.ID
	for i := 0; i < 2; i++ {
		call("POST", path, string(sign(refundEvent)), nil, 200)
	}
	admin("POST", "/api/admin/payments/refunds/"+refund.ID+"/sync", `{}`, 200)
	w = admin("GET", "/api/admin/payments/orders/"+order.ID, "", 200)
	if err := json.Unmarshal(w.Body.Bytes(), &order); err != nil || order.Status != "refunded" {
		t.Fatal("refund not applied")
	}
	var events int
	if err := database.QueryRow(`SELECT COUNT(*) FROM gocms_payment_event`).Scan(&events); err != nil || events != 2 {
		t.Fatal("invalid or duplicate notification persisted")
	}
}

func orderIDForTest(t *testing.T, database interface{ QueryRow(string, ...any) *sql.Row }) string {
	t.Helper()
	var id string
	if err := database.QueryRow(`SELECT id FROM gocms_commerce_order LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestPaymentPermissionsAndCrossOrigin(t *testing.T) {
	s, database, root := newCategoryTestServer(t)
	if _, err := database.Exec(`INSERT INTO gocms_admin_group(name,permissions) VALUES('payment-viewer','["payments"]'); INSERT INTO gocms_admin_user(username,group_id) SELECT 'payment-viewer',id FROM gocms_admin_group WHERE name='payment-viewer'`); err != nil {
		t.Fatal(err)
	}
	viewer, err := s.createSession("payment-viewer")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path string
		want         int
	}{{"GET", "orders", 200}, {"GET", "accounts", 403}, {"POST", "accounts", 403}, {"POST", "attempts/unknown/refund", 403}, {"PUT", "orders/unknown/fulfillment", 403}} {
		w := categoryRequest(t, s, viewer, tc.method, "/api/admin/payments/"+tc.path, "{}")
		if w.Code != tc.want {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest("POST", "/api/admin/payments/accounts", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://evil.example")
	r.AddCookie(&http.Cookie{Name: "gocms_admin", Value: root})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin payment configuration allowed")
	}
	if _, err := database.Exec(`INSERT INTO gocms_site(name,code,domain) VALUES('Other','other','other.example')`); err != nil {
		t.Fatal(err)
	}
	w = categoryRequest(t, s, root, "GET", "/api/admin/payments/orders/ord_missing?site_id=2", "")
	if w.Code != 404 {
		t.Fatal("cross-site request not scoped")
	}
}
