package site

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"gocms/internal/commerce"
)

func TestContentSpecificationsWithoutOnlineSales(t *testing.T) {
	for _, auth := range []string{"session", "api-key"} {
		t.Run(auth, func(t *testing.T) {
			testContentSpecificationsWithoutOnlineSales(t, auth)
		})
	}
}

func testContentSpecificationsWithoutOnlineSales(t *testing.T, auth string) {
	s, database, token := newCategoryTestServer(t)
	send := categoryRequest
	if auth == "api-key" {
		token = apiKeyForTest(t, s, "gocms").Key
		send = apiKeyRequest
	}
	ctx := context.Background()
	request := func(method, path string, payload any, want int) []byte {
		t.Helper()
		var body []byte
		if payload != nil {
			var err error
			body, err = json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
		}
		response := send(t, s, token, method, path, string(body))
		if response.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, response.Code, response.Body.String())
		}
		return response.Body.Bytes()
	}
	request(http.MethodPost, "/api/admin/categories", map[string]any{"name": "展示目录"}, http.StatusOK)
	category := categoryID(t, database, "展示目录")
	dimension, err := s.commerce.SaveDimension(ctx, 1, "", commerce.DimensionRecord{Dimension: commerce.Dimension{
		Name: "容量", Type: "select", Unit: "L", Options: []string{"10", "20"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	product := commerce.Product{
		Kind: "physical", Dimensions: []commerce.Dimension{dimension.Dimension},
		Variants: []commerce.Variant{{SKU: "DISPLAY-10", Enabled: true, Specifications: []commerce.Specification{{Name: "容量", Value: "10", Unit: "L"}}}},
	}
	// A display-only content item needs neither a price nor a sales cover image.
	payload := map[string]any{"title": "展示内容", "category_id": category, "visible": 1, "commerce": product}
	request(http.MethodPost, "/api/admin/content", payload, http.StatusOK)
	var content int64
	if err := database.QueryRow(`SELECT id FROM gocms_content WHERE title='展示内容'`).Scan(&content); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/admin/content/%d", content)
	var detail Content
	if err := json.Unmarshal(request(http.MethodGet, path, nil, http.StatusOK), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Commerce == nil || detail.Commerce.Enabled || len(detail.Commerce.Dimensions) != 1 || len(detail.Commerce.Variants) != 1 {
		t.Fatalf("specifications did not round-trip: %+v", detail.Commerce)
	}
	product = *detail.Commerce
	variant := product.Variants[0]
	if product.Currency != "" || variant.Amount != 0 || variant.SKU != "DISPLAY-10" || variant.Specifications[0].Value != "10" || variant.Specifications[0].Unit != "L" {
		t.Fatalf("specification data changed: %+v", variant)
	}
	assertUnavailable := func() {
		t.Helper()
		offers, err := s.commerce.Offers(ctx, 1, true, 1)
		if err != nil || len(offers) != 0 {
			t.Fatalf("display specifications became purchasable: %+v %v", offers, err)
		}
		if _, err := s.commerce.CreateOrder(ctx, 1, 9, "display-specification-order", commerce.OrderInput{
			OfferID: variant.ID, Quantity: 1, Email: "buyer@example.com", CustomerName: "Buyer",
		}); !errors.Is(err, commerce.ErrNotFound) {
			t.Fatalf("display specification accepted for an order: %v", err)
		}
	}
	assertUnavailable()
	// Opening the category alone must not turn saved specifications into offers.
	if _, err := database.Exec(`UPDATE gocms_category SET commerce_enabled=1 WHERE id=?`, category); err != nil {
		t.Fatal(err)
	}
	assertUnavailable()
	payload["title"] = "不应保存的标题"
	payload["cover_image"] = "https://example.com/item.png"
	product.Enabled = true
	product.Currency = "USD"
	payload["commerce"] = product
	request(http.MethodPut, path, payload, http.StatusBadRequest)
	if err := json.Unmarshal(request(http.MethodGet, path, nil, http.StatusOK), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Title != "展示内容" || detail.Commerce.Enabled || detail.Commerce.Variants[0].Amount != 0 {
		t.Fatalf("failed sales activation changed content: %+v", detail)
	}
	// Invalid prices and dimension values remain invalid even without sales.
	product.Enabled = false
	for _, tc := range []struct {
		name   string
		amount int64
		value  string
	}{
		{"negative price", -1, "10"},
		{"price above limit", 100000000000, "10"},
		{"unknown option", 0, "30"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			product.Variants[0].Amount = tc.amount
			product.Variants[0].Specifications[0].Value = tc.value
			payload["commerce"] = product
			request(http.MethodPut, path, payload, http.StatusBadRequest)
		})
	}
	product.Enabled = true
	product.Variants[0].Amount = 1500
	product.Variants[0].Specifications[0].Value = "10"
	payload["title"] = "展示内容"
	payload["commerce"] = product
	request(http.MethodPut, path, payload, http.StatusOK)
	offers, err := s.commerce.Offers(ctx, 1, true, 1)
	if err != nil || len(offers) != 1 || offers[0].ID != variant.ID || offers[0].Amount != 1500 {
		t.Fatalf("priced specifications could not be enabled for sales: %+v %v", offers, err)
	}
}
