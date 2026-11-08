package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"

	"gocms/internal/db"
)

func manualInput(a Account) ManualOrderInput {
	return ManualOrderInput{AccountID: a.ID, CustomerName: "Customer", Email: "customer@example.com", Language: "ar", Items: []ManualOrderItem{
		{Name: "Custom consulting", Kind: "service", Price: "12.34", Quantity: 2, ImageURL: "https://shop.example/service.png"},
		{Name: "Digital report", Kind: "digital", Price: "5.01", Quantity: 3, ImageURL: "https://shop.example/report.png"},
	}}
}

func linkToken(t *testing.T, link string) string {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	values, err := url.ParseQuery(u.Fragment)
	if err != nil {
		t.Fatal(err)
	}
	return values.Get("token")
}

func TestManualOrderPaymentLinkLifecycle(t *testing.T) {
	s, provider, account, _ := fixture(t)
	ctx := context.Background()
	in := manualInput(account)
	o, err := s.CreateManualOrder(ctx, 1, "admin", "manual-order-request-key", in)
	if err != nil {
		t.Fatal(err)
	}
	if o.Source != "admin" || o.BuyerID != 0 || o.PaymentAccountID != account.ID || o.Amount != 3971 || o.Quantity != 5 || len(o.Items) != 2 {
		t.Fatalf("snapshot: %+v", o)
	}
	for _, line := range o.Items {
		if line.ID != 0 || line.ContentID != nil {
			t.Fatal("manual order changed catalog")
		}
	}
	retried, err := s.CreateManualOrder(ctx, 1, "admin", "manual-order-request-key", in)
	if err != nil || retried.ID != o.ID {
		t.Fatal("retry duplicated order", err)
	}
	in.Items[0].Price = "1"
	if _, err := s.CreateManualOrder(ctx, 1, "admin", "manual-order-request-key", in); !errors.Is(err, ErrConflict) {
		t.Fatal("changed retry accepted", err)
	}
	if _, err := s.LinkedOrder(ctx, 1, o.ID, strings.Repeat("0", 64)); !errors.Is(err, ErrNotFound) {
		t.Fatal("order accessible before issuing link", err)
	}
	link, err := s.IssueOrderLink(ctx, 1, o.ID, "admin", o.Revision)
	if err != nil {
		t.Fatal(err)
	}
	token := linkToken(t, link)
	if len(token) != 64 || !strings.HasPrefix(link, account.PublicURL+"/shop/pay/"+o.ID+"#token=") {
		t.Fatal("invalid payment URL")
	}
	if _, err := s.LinkedOrder(ctx, 2, o.ID, token); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-site credential accepted", err)
	}
	if _, err := s.LinkedOrder(ctx, 1, o.ID, strings.Repeat("0", 64)); !errors.Is(err, ErrNotFound) {
		t.Fatal("wrong credential accepted", err)
	}
	o, err = s.LinkedOrder(ctx, 1, o.ID, token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.IssueOrderLink(ctx, 1, o.ID, "admin", 0); !errors.Is(err, ErrConflict) {
		t.Fatal("stale revision accepted", err)
	}
	link, err = s.IssueOrderLink(ctx, 1, o.ID, "admin", o.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.LinkedOrder(ctx, 1, o.ID, token); !errors.Is(err, ErrNotFound) {
		t.Fatal("old link survived rotation", err)
	}
	token = linkToken(t, link)
	if _, err := s.Pay(ctx, 1, o.ID, account.ID+1, "127.0.0.1"); !errors.Is(err, ErrInvalid) {
		t.Fatal("channel changed", err)
	}
	attempt, err := s.Pay(ctx, 1, o.ID, account.ID, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if provider.checkout.ReturnURL != account.PublicURL+"/shop/pay/"+o.ID+"?returned=1" || provider.checkout.Amount != 3971 || len(provider.checkout.Items) != 2 {
		t.Fatalf("wrong checkout: %+v", provider.checkout)
	}
	again, err := s.Pay(ctx, 1, o.ID, account.ID, "127.0.0.1")
	if err != nil || again.ID != attempt.ID {
		t.Fatal("payment retry duplicated attempt", err)
	}
	r := provider.payments[attempt.ID]
	r.Status = "succeeded"
	if err := s.Apply(ctx, account, r, ""); err != nil {
		t.Fatal(err)
	}
	o, err = s.LinkedOrder(ctx, 1, o.ID, token)
	if err != nil || o.Status != "paid" {
		t.Fatal("payment not visible", err)
	}
	if _, err := s.Pay(ctx, 1, o.ID, account.ID, "127.0.0.1"); !errors.Is(err, ErrConflict) {
		t.Fatal("paid twice", err)
	}
	if _, err := s.IssueOrderLink(ctx, 1, o.ID, "admin", o.Revision); !errors.Is(err, ErrConflict) {
		t.Fatal("paid link rotated", err)
	}
	events, err := s.OrderEvents(ctx, 1, o.ID)
	if err != nil || len(events) != 3 {
		t.Fatal("audit missing", err)
	}
	raw, _ := json.Marshal(events)
	if strings.Contains(string(raw), token) {
		t.Fatal("credential leaked into audit")
	}
	var stored string
	if err := s.DB.QueryRow(`SELECT payment_token_hash FROM gocms_commerce_order WHERE id=?`, o.ID).Scan(&stored); err != nil || stored == token || stored != digest([]byte(token)) {
		t.Fatal("plaintext credential persisted", err)
	}
	if _, err := s.RefundAttempt(ctx, 1, attempt.ID); err != nil {
		t.Fatal("manual order refund failed", err)
	}
}

func TestManualOrderValidationIsAtomic(t *testing.T) {
	s, _, a, _ := fixture(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		change func(*ManualOrderInput)
	}{
		{"empty", func(in *ManualOrderInput) { in.Items = nil }},
		{"negative", func(in *ManualOrderInput) { in.Items[1].Price = "-1" }},
		{"precision", func(in *ManualOrderInput) { in.Items[1].Price = "0.001" }},
		{"zero", func(in *ManualOrderInput) { in.Items[1].Quantity = 0 }},
		{"overflow", func(in *ManualOrderInput) { in.Items[1].Price = "999999999.99"; in.Items[1].Quantity = 2 }},
		{"kind", func(in *ManualOrderInput) { in.Items[1].Kind = "unknown" }},
		{"address", func(in *ManualOrderInput) { in.Items[1].Kind = "physical" }},
		{"email", func(in *ManualOrderInput) { in.Email = "bad" }},
		{"image", func(in *ManualOrderInput) { in.Items[1].ImageURL = "/assets/other-site.png" }},
		{"account", func(in *ManualOrderInput) { in.AccountID = 99999 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := manualInput(a)
			tc.change(&in)
			if _, err := s.CreateManualOrder(ctx, 1, "admin", "manual-validation-"+tc.name, in); err == nil {
				t.Fatal("invalid order accepted")
			}
			var count int
			if err := s.DB.QueryRow(`SELECT COUNT(*) FROM gocms_commerce_order`).Scan(&count); err != nil || count != 0 {
				t.Fatal("partial order persisted", err)
			}
		})
	}
	// A full physical snapshot can use the regular shipment workflow.
	in := manualInput(a)
	in.Items[0].Kind = "physical"
	in.Address = Address{Phone: "1234567", Street: "123 Main St", Postcode: "12345", City: "City", Country: "US"}
	if _, err := s.CreateManualOrder(ctx, 1, "admin", "manual-valid-physical", in); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE gocms_payment_account SET enabled=0 WHERE id=?`, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateManualOrder(ctx, 1, "admin", "manual-disabled-channel", in); !errors.Is(err, ErrInvalid) {
		t.Fatal("disabled channel accepted", err)
	}
}

func TestManualOrderCurrencyAndCheckoutMigration(t *testing.T) {
	s, _, a, offer := fixture(t)
	ctx := context.Background()
	checkout, err := s.CreateOrder(ctx, 1, 42, "existing-checkout-order", OrderInput{OfferID: offer.ID, Quantity: 1, CustomerName: "Member", Email: "member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the previous schema, then repeat startup migration to verify that
	// existing financial records retain their member access and snapshots.
	for _, column := range []string{"source", "payment_account_id", "payment_token_hash"} {
		if _, err := s.DB.Exec(`ALTER TABLE gocms_commerce_order DROP COLUMN ` + column); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := db.EnsureCommerce(ctx, s.DB); err != nil {
			t.Fatal(err)
		}
	}
	after, err := s.Order(ctx, 1, checkout.ID)
	if err != nil || after.BuyerID != 42 || after.Source != "checkout" || after.PaymentAccountID != 0 || after.Amount != checkout.Amount || after.Items[0].Name != checkout.Items[0].Name {
		t.Fatal("migration changed member order", err)
	}
	if _, err := s.LinkedOrder(ctx, 1, checkout.ID, strings.Repeat("a", 64)); !errors.Is(err, ErrNotFound) {
		t.Fatal("member order exposed via link", err)
	}
	if _, err := s.IssueOrderLink(ctx, 1, checkout.ID, "admin", after.Revision); !errors.Is(err, ErrNotFound) {
		t.Fatal("link created for member order", err)
	}
	if _, err := s.DB.Exec(`UPDATE gocms_payment_account SET currency='JPY' WHERE id=?`, a.ID); err != nil {
		t.Fatal(err)
	}
	in := manualInput(a)
	if _, err := s.CreateManualOrder(ctx, 1, "admin", "manual-yen-fraction", in); !errors.Is(err, ErrInvalid) {
		t.Fatal("fractional yen accepted", err)
	}
	in.Items[0].Price = "120"
	in.Items[1].Price = "5"
	yen, err := s.CreateManualOrder(ctx, 1, "admin", "manual-yen-integer", in)
	if err != nil || yen.Currency != "JPY" || yen.Amount != 255 {
		t.Fatal("yen amount converted incorrectly", err)
	}
}
