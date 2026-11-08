package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"testing"
)

func TestOrderCatalogSearchQuotesAndPagination(t *testing.T) {
	s, _, a, offer := fixture(t)
	ctx := context.Background()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := s.DB.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE gocms_content SET title='100% Consulting' WHERE id=?`, *offer.ContentID)
	exec(`UPDATE gocms_commerce_offer SET sku='PLAN-PRO',specifications_json='[{"name":"Duration","value":"Annual","unit":"year"}]',fixed_prices_json='{"EUR":1999}' WHERE id=?`, offer.ID)
	for _, query := range []string{"consulting", "plan-pro", "Annual", "%", strconv.FormatInt(*offer.ContentID, 10)} {
		items, more, err := s.OrderVariants(ctx, 1, a.ID, 1, query)
		if err != nil || len(items) != 1 || more || items[0].ID != offer.ID || items[0].Currency != "USD" || items[0].Amount != offer.Amount {
			t.Fatalf("search %q: %+v %t %v", query, items, more, err)
		}
	}
	items, _, err := s.OrderVariants(ctx, 1, a.ID, 1, "no-match")
	if err != nil || len(items) != 0 {
		t.Fatal("unexpected search match", err)
	}
	if _, _, err := s.OrderVariants(ctx, 2, a.ID, 1, ""); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-site account accepted", err)
	}
	if _, _, err := s.OrderVariants(ctx, 1, a.ID, 0, ""); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid page accepted", err)
	}
	euro := a
	euro.ID = 0
	euro.Currency = "EUR"
	euro.Secret = "test-salt"
	euro, err = s.CreateAccount(ctx, 1, euro)
	if err != nil {
		t.Fatal(err)
	}
	items, _, err = s.OrderVariants(ctx, 1, euro.ID, 1, "")
	if err != nil || len(items) != 1 || !items[0].PricingUnavailable || items[0].Currency != "USD" {
		t.Fatal("another currency reinterpreted", err)
	}
	settings := PricingSettings{Enabled: true, BaseCurrency: "USD", Currencies: []CurrencyRule{{Currency: "USD", Enabled: true, Rate: "1"}, {Currency: "EUR", Enabled: true, Rate: "0.9"}}}
	raw, _ := json.Marshal(settings)
	exec(`INSERT INTO gocms_commerce_pricing(site_id,settings_json) VALUES(1,?)`, string(raw))
	items, _, err = s.OrderVariants(ctx, 1, euro.ID, 1, "")
	if err != nil || len(items) != 1 || items[0].PricingUnavailable || items[0].Amount != 1999 || items[0].Currency != "EUR" {
		t.Fatalf("fixed quote: %+v %v", items, err)
	}
	exec(`UPDATE gocms_commerce_offer SET fixed_prices_json='{}' WHERE id=?`, offer.ID)
	items, _, err = s.OrderVariants(ctx, 1, euro.ID, 1, "")
	if err != nil || len(items) != 1 || !items[0].PricingUnavailable {
		t.Fatal("missing exchange rate accepted", err)
	}
	for _, tc := range []struct{ disable, restore string }{
		{`UPDATE gocms_commerce_offer SET enabled=0 WHERE id=?`, `UPDATE gocms_commerce_offer SET enabled=1 WHERE id=?`},
		{`UPDATE gocms_commerce_offer SET archived=1 WHERE id=?`, `UPDATE gocms_commerce_offer SET archived=0 WHERE id=?`},
	} {
		exec(tc.disable, offer.ID)
		items, _, err = s.OrderVariants(ctx, 1, a.ID, 1, "")
		if err != nil || len(items) != 0 {
			t.Fatal("unavailable variant returned", err)
		}
		exec(tc.restore, offer.ID)
	}
	exec(`UPDATE gocms_content SET visible=0 WHERE id=?`, *offer.ContentID)
	items, _, err = s.OrderVariants(ctx, 1, a.ID, 1, "")
	if err != nil || len(items) != 0 {
		t.Fatal("hidden content returned", err)
	}
	exec(`UPDATE gocms_content SET visible=1 WHERE id=?`, *offer.ContentID)
	exec(`UPDATE gocms_category SET commerce_enabled=0 WHERE id=(SELECT category_id FROM gocms_content WHERE id=?)`, *offer.ContentID)
	items, _, err = s.OrderVariants(ctx, 1, a.ID, 1, "")
	if err != nil || len(items) != 0 {
		t.Fatal("disabled category returned", err)
	}
	exec(`UPDATE gocms_category SET commerce_enabled=1 WHERE id=(SELECT category_id FROM gocms_content WHERE id=?)`, *offer.ContentID)
	for i := 0; i < 51; i++ {
		exec(`INSERT INTO gocms_commerce_offer(site_id,content_id,name,kind,amount,currency,image_url,enabled,sku) VALUES(1,?,'Variant','service',100,'USD','https://shop.example/image.png',1,?)`, *offer.ContentID, fmt.Sprintf("PAGE-%d", i))
	}
	first, more, err := s.OrderVariants(ctx, 1, a.ID, 1, "")
	if err != nil || len(first) != 50 || !more {
		t.Fatal("first page truncated incorrectly", err)
	}
	last, more, err := s.OrderVariants(ctx, 1, a.ID, 2, "")
	if err != nil || len(last) != 2 || more || first[49].ID == last[0].ID {
		t.Fatal("pagination skipped or repeated variants", err)
	}
}

func TestManualOrderSelectedVariantSnapshotAndValidation(t *testing.T) {
	s, _, account, offer := fixture(t)
	ctx := context.Background()
	if _, err := s.DB.Exec(`UPDATE gocms_commerce_offer SET sku='SELECTED',specifications_json='[{"name":"Duration","value":"Annual","unit":"year"}]' WHERE id=?`, offer.ID); err != nil {
		t.Fatal(err)
	}
	in := manualInput(account)
	in.Items = []ManualOrderItem{{VariantID: offer.ID, Currency: "USD", Name: "Client supplied name", Kind: "digital", ImageURL: "https://other.example/ignored.png", Price: "10.00", Quantity: 3}}
	o, err := s.CreateManualOrder(ctx, 1, "admin", "selected-variant-order", in)
	if err != nil {
		t.Fatal(err)
	}
	line := o.Items[0]
	if o.Amount != 3000 || line.Amount != 1000 || line.ID != offer.ID || line.ContentID == nil || *line.ContentID != *offer.ContentID || line.Name != offer.Name || line.Kind != offer.Kind || line.ImageURL != offer.ImageURL || line.SKU != "SELECTED" || len(line.Specifications) != 1 || line.Specifications[0].Unit != "year" {
		t.Fatalf("bad selected snapshot: %+v", line)
	}
	var catalogAmount int64
	if err := s.DB.QueryRow(`SELECT amount FROM gocms_commerce_offer WHERE id=?`, offer.ID).Scan(&catalogAmount); err != nil || catalogAmount != offer.Amount {
		t.Fatal("negotiated price changed catalog", err)
	}
	in.Items[0].Currency = "EUR"
	if _, err := s.CreateManualOrder(ctx, 1, "admin", "wrong-selected-currency", in); !errors.Is(err, ErrInvalid) {
		t.Fatal("old currency accepted", err)
	}
	in.Items[0].Currency = "USD"
	if _, err := s.DB.Exec(`UPDATE gocms_commerce_offer SET enabled=0 WHERE id=?`, offer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateManualOrder(ctx, 1, "admin", "stale-selected-variant", in); !errors.Is(err, ErrNotFound) {
		t.Fatal("stale selection accepted", err)
	}
	retry, err := s.CreateManualOrder(ctx, 1, "admin", "selected-variant-order", in)
	if err != nil || retry.ID != o.ID || retry.Items[0].SKU != "SELECTED" {
		t.Fatal("retry lost original snapshot", err)
	}
	if _, err := s.DB.Exec(`INSERT INTO gocms_site(name,code,domain) VALUES('Other','other','other.example');UPDATE gocms_commerce_offer SET site_id=2,enabled=1 WHERE id=?`, offer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateManualOrder(ctx, 1, "admin", "cross-site-selected-variant", in); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-site variant accepted", err)
	}
}
