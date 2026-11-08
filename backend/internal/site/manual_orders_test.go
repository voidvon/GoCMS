package site

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gocms/internal/commerce"
	"gocms/internal/payment/providers/pingpong"
)

func TestManualOrderHTTPGuestPaymentAndScope(t *testing.T) {
	t.Setenv("GOCMS_PAYMENT_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	s, database, root := newCategoryTestServer(t)
	call := func(session, token, method, path, body, origin string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "admin-manual-order-http")
		if session != "" {
			r.AddCookie(&http.Cookie{Name: "gocms_admin", Value: session})
		}
		if token != "" {
			r.Header.Set("X-Order-Token", token)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "payment_token_hash") {
			t.Fatal("token hash leaked")
		}
		return w
	}
	a, err := s.commerce.CreateAccount(context.Background(), 1, commerce.Account{Name: "Checkout", Provider: "pingpong", Environment: "sandbox", ClientID: "client", MerchantID: "shop", Secret: "salt", Currency: "USD", PublicURL: "https://example.com", ReturnPath: "/shop/orders", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	in := commerce.ManualOrderInput{AccountID: a.ID, CustomerName: "Buyer", Email: "buyer@example.com", Language: "zh-CN", Items: []commerce.ManualOrderItem{{Name: "Custom service", Kind: "service", Price: "15.25", Quantity: 2, ImageURL: "https://example.com/image.png"}}}
	raw, _ := json.Marshal(in)
	body := string(raw)
	if _, err := database.Exec(`INSERT INTO gocms_admin_group(name,permissions) VALUES('manual-viewer','["orders"]');INSERT INTO gocms_admin_user(username,group_id) SELECT 'manual-viewer',id FROM gocms_admin_group WHERE name='manual-viewer'`); err != nil {
		t.Fatal(err)
	}
	viewer, err := s.createSession("manual-viewer")
	if err != nil {
		t.Fatal(err)
	}
	call(viewer, "", "POST", "/api/admin/orders", body, "", 403)
	call(viewer, "", "GET", "/api/admin/orders/creation-options", "", "", 403)
	call(viewer, "", "GET", fmt.Sprintf("/api/admin/orders/catalog?account_id=%d", a.ID), "", "", 403)
	call("", "", "GET", fmt.Sprintf("/api/admin/orders/catalog?account_id=%d", a.ID), "", "", 401)
	call(root, "", "POST", "/api/admin/orders", body, "https://evil.example", 403)
	options := call(root, "", "GET", "/api/admin/orders/creation-options", "", "", 200)
	if strings.Contains(options.Body.String(), "merchant_id") || strings.Contains(options.Body.String(), "ciphertext") {
		t.Fatal("channel credentials leaked")
	}
	if _, err := database.Exec(`INSERT INTO gocms_admin_group(name,permissions) VALUES('manual-creator','["orders","orders.create"]');INSERT INTO gocms_admin_user(username,group_id) SELECT 'manual-creator',id FROM gocms_admin_group WHERE name='manual-creator'`); err != nil {
		t.Fatal(err)
	}
	creator, err := s.createSession("manual-creator")
	if err != nil {
		t.Fatal(err)
	}
	call(creator, "", "GET", "/api/admin/orders/creation-options", "", "", 200)
	call(creator, "", "GET", fmt.Sprintf("/api/admin/orders/catalog?account_id=%d&q=anything", a.ID), "", "", 200)
	w := call(creator, "", "POST", "/api/admin/orders", body, "", 201)
	var order commerce.Order
	if err := json.Unmarshal(w.Body.Bytes(), &order); err != nil || order.Amount != 3050 || order.Source != "admin" {
		t.Fatal("incorrect manual order", err)
	}
	w = call(creator, "", "POST", "/api/admin/orders", body, "", 201)
	var retry commerce.Order
	_ = json.Unmarshal(w.Body.Bytes(), &retry)
	if retry.ID != order.ID {
		t.Fatal("HTTP retry created a duplicate")
	}
	linkPath := "/api/admin/orders/" + order.ID + "/payment-link"
	call(viewer, "", "POST", linkPath, `{"revision":0}`, "", 403)
	call(root, "", "POST", linkPath, `{"revision":0}`, "https://evil.example", 403)
	w = call(root, "", "POST", linkPath, `{"revision":0}`, "", 200)
	var link struct {
		PaymentURL string `json:"payment_url"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &link)
	u, err := url.Parse(link.PaymentURL)
	if err != nil {
		t.Fatal(err)
	}
	values, _ := url.ParseQuery(u.Fragment)
	token := values.Get("token")
	if len(token) != 64 {
		t.Fatal("missing secure token")
	}
	orderPath := "/api/commerce/orders/" + order.ID
	call("", "", "GET", orderPath, "", "", 401)
	call("", strings.Repeat("0", 64), "GET", orderPath, "", "", 404)
	call("", token, "GET", orderPath, "", "", 200)
	call("", token, "GET", "/api/commerce/orders", "", "", 401)
	call("", token, "POST", "/api/commerce/orders", body, "", 401)
	call("", token, "GET", "/api/commerce/orders/missing", "", "", 404)
	if _, err := database.Exec(`INSERT INTO gocms_site(name,code,domain) VALUES('Other','other','other.example')`); err != nil {
		t.Fatal(err)
	}
	call("", token, "GET", orderPath+"?site_id=2", "", "", 404)
	call(root, "", "POST", linkPath+"?site_id=2", `{"revision":1}`, "", 404)
	payBody := fmt.Sprintf(`{"account_id":%d}`, a.ID)
	call("", token, "POST", orderPath+"/pay", payBody, "https://evil.example", 403)
	call("", token, "POST", orderPath+"/pay", fmt.Sprintf(`{"account_id":%d}`, a.ID+1), "", 400)
	s.commerce.Providers["pingpong"] = pingpong.Provider{Client: &http.Client{Transport: paymentTestTransport(func(r *http.Request) (*http.Response, error) {
		var fields map[string]string
		if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
			t.Fatal(err)
		}
		var biz map[string]any
		if err := json.Unmarshal([]byte(fields["bizContent"]), &biz); err != nil {
			t.Fatal(err)
		}
		if r.URL.Path == "/v4/payment/prePay" && biz["payResultUrl"] != "https://example.com/shop/pay/"+order.ID+"?returned=1" {
			t.Fatal("guest return path requires member login")
		}
		result := map[string]any{"merchantTransactionId": biz["merchantTransactionId"], "transactionId": "manual_tx", "amount": "30.500000", "currency": "USD", "status": "SUCCESS"}
		if r.URL.Path == "/v4/payment/prePay" {
			result["paymentUrl"] = "https://sandbox-safepay.pingpongx.com?token=opaque"
		}
		encoded, _ := json.Marshal(result)
		response := map[string]string{"accId": "shop", "clientId": "client", "signType": "SHA256", "code": "000000", "bizContent": string(encoded)}
		response["sign"] = pingpong.Sign(response, "salt")
		payload, _ := json.Marshal(response)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(payload)))}, nil
	})}}
	call("", token, "POST", orderPath+"/pay", payBody, "", 200)
	w = call("", token, "POST", orderPath+"/sync", `{}`, "", 200)
	if err := json.Unmarshal(w.Body.Bytes(), &order); err != nil || order.Status != "paid" {
		t.Fatal("guest could not see confirmed payment", err)
	}
	if strings.Contains(w.Body.String(), "internal_note") || strings.Contains(w.Body.String(), "actor") {
		t.Fatal("private admin data exposed")
	}
	call("", token, "POST", orderPath+"/pay", payBody, "", 409)
	call(root, "", "POST", linkPath, fmt.Sprintf(`{"revision":%d}`, order.Revision), "", 409)
}
