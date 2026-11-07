package site

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gocms/internal/commerce"
	"gocms/internal/membermail"
)

func TestOrderManagementPermissionsAndOrigin(t *testing.T) {
	s, database, root := newCategoryTestServer(t)
	if _, err := database.Exec(`INSERT INTO gocms_admin_group(name,permissions) VALUES('order-viewer','["orders"]');INSERT INTO gocms_admin_user(username,group_id) SELECT 'order-viewer',id FROM gocms_admin_group WHERE name='order-viewer'`); err != nil {
		t.Fatal(err)
	}
	viewer, err := s.createSession("order-viewer")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path string
		want         int
	}{{"GET", "", 200}, {"GET", "/carriers", 200}, {"POST", "/unknown/shipments", 403}, {"PUT", "/unknown/shipments/id", 403}, {"PUT", "/unknown/recipient", 403}, {"POST", "/unknown/complete", 403}, {"POST", "/unknown/refund", 403}, {"POST", "/unknown/refund-sync", 403}, {"GET", "/missing", 404}} {
		w := categoryRequest(t, s, viewer, tc.method, "/api/admin/orders"+tc.path, `{}`)
		if w.Code != tc.want {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
	req := httptest.NewRequest("POST", "/api/admin/orders/missing/shipments", strings.NewReader(`{}`))
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "gocms_admin", Value: root})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("cross-origin mutation allowed")
	}
}
func TestOrderShipmentHTTPAndMailQueue(t *testing.T) {
	s, database, root := newCategoryTestServer(t)
	ctx := context.Background()
	run := func(q string, args ...any) {
		t.Helper()
		if _, err := database.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	run(`INSERT INTO gocms_category(site_id,name,commerce_enabled) VALUES(1,'Any industry',1)`)
	var category int64
	_ = database.QueryRow(`SELECT id FROM gocms_category WHERE name='Any industry'`).Scan(&category)
	run(`INSERT INTO gocms_content(site_id,category_id,title,cover_image,visible) VALUES(1,?,'Service','https://example.com/product.png',1)`, category)
	var content int64
	_ = database.QueryRow(`SELECT id FROM gocms_content WHERE title='Service'`).Scan(&content)
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = s.commerce.SaveProduct(ctx, tx, 1, content, commerce.Product{Enabled: true, Kind: "service", Currency: "USD", Variants: []commerce.Variant{{Amount: 100, Enabled: true}}})
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	offers, err := s.commerce.Offers(ctx, 1, true, 1)
	if err != nil || len(offers) != 1 {
		t.Fatal(err)
	}
	o, err := s.commerce.CreateOrder(ctx, 1, 9, "shipment-http-order", commerce.OrderInput{OfferID: offers[0].ID, Quantity: 2, Email: "buyer@example.com", CustomerName: "Buyer", Language: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	run(`UPDATE gocms_commerce_order SET status='paid' WHERE id=?`, o.ID)
	config := membermail.Config{Enabled: true, Host: "smtp.example.com", Port: 465, TLS: "tls", From: "shop@example.com"}
	raw, _ := json.Marshal(config)
	run(`INSERT INTO gocms_platform_mail_config(id,config,revision) VALUES(1,?,'notice-test')`, string(raw))
	in := commerce.ShipmentInput{Key: "shipment-http-request", Method: "no_shipping", ShippedAt: time.Now().UTC().Format(time.RFC3339), Note: "public note", InternalNote: "private note", Items: []commerce.ShipmentItem{{LineNumber: 1, Quantity: 1}}}
	body, _ := json.Marshal(in)
	w := categoryRequest(t, s, root, "POST", "/api/admin/orders/"+o.ID+"/shipments", string(body))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "private note") || !strings.Contains(w.Body.String(), `"notice_status":"pending"`) {
		t.Fatalf("shipment %d %s", w.Code, w.Body.String())
	}
	run(`INSERT INTO gocms_site(name,code,domain) VALUES('Other','other','other.example')`)
	w = categoryRequest(t, s, root, "GET", "/api/admin/orders/"+o.ID+"?site_id=2", "")
	if w.Code != 404 {
		t.Fatalf("cross-site detail %d", w.Code)
	}
	calls := 0
	s.mailSend = func(_ context.Context, _ membermail.Config, secret, to, subject, body string) error {
		calls++
		if to != "buyer@example.com" || strings.Contains(body, "private note") || !strings.Contains(subject, "交付") || !strings.Contains(body, "public note") {
			t.Fatalf("mail contents %s %s %s", to, subject, body)
		}
		return errors.New("fake smtp failure")
	}
	s.shipmentNotices(ctx)
	var state string
	_ = database.QueryRow(`SELECT notice_status FROM gocms_commerce_shipment`).Scan(&state)
	if calls != 1 || state != "pending" {
		t.Fatalf("failure not queued %d %s", calls, state)
	}
	detail, err := s.commerce.Order(ctx, 1, o.ID)
	if err != nil || detail.FulfillmentStatus != "partial" {
		t.Fatal("SMTP failure rolled back shipment")
	}
	run(`UPDATE gocms_commerce_shipment SET notice_next=0`)
	s.mailSend = func(context.Context, membermail.Config, string, string, string, string) error { calls++; return nil }
	s.shipmentNotices(ctx)
	s.shipmentNotices(ctx)
	_ = database.QueryRow(`SELECT notice_status FROM gocms_commerce_shipment`).Scan(&state)
	if calls != 2 || state != "sent" {
		t.Fatalf("duplicate notice %d %s", calls, state)
	}
	// The same HTTP retry never creates a second shipment or second notice.
	w = categoryRequest(t, s, root, "POST", "/api/admin/orders/"+o.ID+"/shipments", string(body))
	if w.Code != 200 {
		t.Fatalf("retry %d %s", w.Code, w.Body.String())
	}
	s.shipmentNotices(ctx)
	if calls != 2 {
		t.Fatal("retry mailed twice")
	}
	config.Enabled = false
	raw, _ = json.Marshal(config)
	run(`UPDATE gocms_platform_mail_config SET config=? WHERE id=1`, string(raw))
	in.Revision = detail.Revision
	in.Key = "shipment-disabled-request"
	body, _ = json.Marshal(in)
	w = categoryRequest(t, s, root, "POST", "/api/admin/orders/"+o.ID+"/shipments", string(body))
	if w.Code != 200 {
		t.Fatalf("disabled mail shipment %d %s", w.Code, w.Body.String())
	}
	s.shipmentNotices(ctx)
	if calls != 2 {
		t.Fatal("disabled mail sent")
	}
	detail, err = s.commerce.Order(ctx, 1, o.ID)
	if err != nil || detail.FulfillmentStatus != "shipped" {
		t.Fatal("disabled mail blocked shipment")
	}
	in.Revision = detail.Revision
	in.Reason = "correct public delivery note"
	in.Note = "corrected note"
	in.Items = nil
	body, _ = json.Marshal(in)
	w = categoryRequest(t, s, root, "PUT", "/api/admin/orders/"+o.ID+"/shipments/"+detail.Shipments[0].ID, string(body))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "corrected note") {
		t.Fatalf("correction %d %s", w.Code, w.Body.String())
	}
	s.shipmentNotices(ctx)
	if calls != 2 {
		t.Fatal("correction resent mail")
	}

}
