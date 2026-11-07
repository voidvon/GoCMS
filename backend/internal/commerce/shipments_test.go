package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func paidShippingOrder(t *testing.T) (Service, Order) {
	t.Helper()
	s, _, _, offer := fixture(t)
	o := create(t, s, offer, "shipment-order-test")
	if _, err := s.DB.Exec(`UPDATE gocms_commerce_order SET status='paid' WHERE id=?`, o.ID); err != nil {
		t.Fatal(err)
	}
	o, err := s.Order(context.Background(), 1, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	return s, o
}
func shipInput(o Order, key string, qty int64) ShipmentInput {
	return ShipmentInput{Revision: o.Revision, Key: key, Method: "no_shipping", ShippedAt: time.Now().UTC().Format(time.RFC3339), InternalNote: "internal-secret", Note: "buyer note", Items: []ShipmentItem{{LineNumber: 1, Quantity: qty}}}
}
func TestShipmentAllocationIdempotencyCompletionAndPrivacy(t *testing.T) {
	s, o := paidShippingOrder(t)
	ctx := context.Background()
	in := shipInput(o, "shipment-request-1", 1)
	id, err := s.CreateShipment(ctx, 1, o.ID, "operator", in)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.CreateShipment(ctx, 1, o.ID, "operator", in)
	if err != nil || again != id {
		t.Fatalf("retry %s %v", again, err)
	}
	in.Note = "changed"
	if _, err = s.CreateShipment(ctx, 1, o.ID, "operator", in); !errors.Is(err, ErrConflict) {
		t.Fatalf("key reuse: %v", err)
	}
	detail, err := s.Order(ctx, 1, o.ID)
	if err != nil || detail.FulfillmentStatus != "partial" || detail.Items[0].ShippedQuantity != 1 || len(detail.Shipments) != 1 {
		t.Fatalf("partial %+v %v", detail, err)
	}
	raw, _ := json.Marshal(detail)
	if strings.Contains(string(raw), "internal-secret") || strings.Contains(string(raw), "operator") {
		t.Fatal("private metadata leaked")
	}
	if err = s.CompleteOrder(ctx, 1, o.ID, "operator", detail.Revision); !errors.Is(err, ErrConflict) {
		t.Fatalf("premature completion: %v", err)
	}
	over := shipInput(detail, "shipment-request-2", 2)
	if _, err = s.CreateShipment(ctx, 1, o.ID, "operator", over); !errors.Is(err, ErrConflict) {
		t.Fatalf("over-shipment: %v", err)
	}
	if _, err = s.CreateShipment(ctx, 2, o.ID, "other site", shipInput(detail, "shipment-request-x", 1)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("site leak: %v", err)
	}
	_, err = s.CreateShipment(ctx, 1, o.ID, "operator", shipInput(detail, "shipment-request-3", 1))
	if err != nil {
		t.Fatal(err)
	}
	detail, _ = s.Order(ctx, 1, o.ID)
	if detail.FulfillmentStatus != "shipped" {
		t.Fatal("shipping prematurely completed order")
	}
	if err = s.Fulfill(ctx, 1, o.ID, "completed"); !errors.Is(err, ErrConflict) {
		t.Fatalf("legacy bypass %v", err)
	}
	if err = s.CompleteOrder(ctx, 1, o.ID, "operator", detail.Revision); err != nil {
		t.Fatal(err)
	}
	detail, _ = s.Order(ctx, 1, o.ID)
	if detail.FulfillmentStatus != "completed" {
		t.Fatal("completion not saved")
	}
	correction := shipInput(detail, "correction-request-1", 1)
	correction.Reason = "correct customer note"
	correction.Note = "corrected"
	correction.Items = nil
	if err = s.EditShipment(ctx, 1, o.ID, id, "operator", correction); err != nil {
		t.Fatal(err)
	}
	events, _ := s.OrderEvents(ctx, 1, o.ID)
	if len(events) != 4 || events[0].Action != "shipment.corrected" || !strings.Contains(events[0].Detail, "internal-secret") {
		t.Fatalf("audit %+v", events)
	}
	matches, err := s.FilterOrders(ctx, 1, 1, "completed", o.ID)
	if err != nil || len(matches) != 1 {
		t.Fatalf("filter %v %v", matches, err)
	}
}
func TestShipmentConcurrentAllocation(t *testing.T) {
	s, o := paidShippingOrder(t)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, key := range []string{"concurrent-shipment-1", "concurrent-shipment-2"} {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			_, err := s.CreateShipment(context.Background(), 1, o.ID, "operator", shipInput(o, key, 2))
			results <- err
		}(key)
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	detail, err := s.Order(context.Background(), 1, o.ID)
	if err != nil || success != 1 || detail.Items[0].ShippedQuantity != 2 {
		t.Fatalf("concurrency %d %+v %v", success, detail, err)
	}
}
func TestShipmentPhysicalRecipientAndRefundGuards(t *testing.T) {
	s, o := paidShippingOrder(t)
	ctx := context.Background()
	_, err := s.DB.Exec(`UPDATE gocms_commerce_order_item SET kind='physical' WHERE order_id=?`, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	in := shipInput(o, "physical-shipment-1", 1)
	if _, err = s.CreateShipment(ctx, 1, o.ID, "operator", in); !errors.Is(err, ErrInvalid) {
		t.Fatalf("physical without logistics: %v", err)
	}
	in.Method = "courier"
	in.Carrier = "Any Carrier"
	in.TrackingNumber = "TRACK-42"
	if _, err = s.CreateShipment(ctx, 1, o.ID, "operator", in); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing phone/address: %v", err)
	}
	address := Address{Phone: "+1 555 0100", Street: "12 Example Road", City: "Any City", Country: "US"}
	if err = s.UpdateRecipient(ctx, 1, o.ID, "operator", o.Revision, "Recipient", address, "legacy recipient补录"); err != nil {
		t.Fatal(err)
	}
	o, _ = s.Order(ctx, 1, o.ID)
	in.Revision = o.Revision
	in.TrackingURL = "javascript:alert(1)"
	if _, err = s.CreateShipment(ctx, 1, o.ID, "operator", in); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unsafe link: %v", err)
	}
	in.TrackingURL = "https://carrier.example/track?q=42"
	if _, err = s.CreateShipment(ctx, 1, o.ID, "operator", in); err != nil {
		t.Fatal(err)
	}
	o, _ = s.Order(ctx, 1, o.ID)
	if err = s.UpdateRecipient(ctx, 1, o.ID, "operator", o.Revision, "Changed", address, "after dispatch"); !errors.Is(err, ErrConflict) {
		t.Fatalf("recipient after dispatch %v", err)
	}
	var account int64
	if err = s.DB.QueryRow(`SELECT id FROM gocms_payment_account LIMIT 1`).Scan(&account); err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.Exec(`INSERT INTO gocms_payment_attempt(id,order_id,account_id,status) VALUES('refund-attempt',?,?,'succeeded')`, o.ID, account)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.Exec(`INSERT INTO gocms_payment_refund(id,attempt_id,amount,currency,status) VALUES('pending-refund','refund-attempt',1234,'USD','pending')`)
	if err != nil {
		t.Fatal(err)
	}
	in.Key = "physical-shipment-2"
	in.Revision = o.Revision
	if _, err = s.CreateShipment(ctx, 1, o.ID, "operator", in); !errors.Is(err, ErrConflict) {
		t.Fatalf("pending refund dispatch %v", err)
	}
	_, err = s.DB.Exec(`UPDATE gocms_commerce_order SET status='refunded' WHERE id=?`, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	o, _ = s.Order(ctx, 1, o.ID)
	if len(o.Shipments) != 1 || o.Items[0].ShippedQuantity != 1 {
		t.Fatal("refund erased shipment history")
	}
}

func TestShipmentMultipleLinesAndDeliveryModes(t *testing.T) {
	s, o := paidShippingOrder(t)
	ctx := context.Background()
	_, err := s.DB.Exec(`INSERT INTO gocms_commerce_order_item(order_id,line_number,offer_id,name,kind,unit_price,quantity,image_url) VALUES(?,2,999,'Physical snapshot','physical',100,3,'https://example.com/item.png')`, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	address := Address{Phone: "+44 123 456", Street: "Any Road", City: "Any Town", Country: "GB"}
	if err = s.UpdateRecipient(ctx, 1, o.ID, "operator", o.Revision, "Buyer", address, "add recipient"); err != nil {
		t.Fatal(err)
	}
	o, _ = s.Order(ctx, 1, o.ID)
	in := shipInput(o, "multi-line-digital", 2)
	if _, err = s.CreateShipment(ctx, 1, o.ID, "operator", in); err != nil {
		t.Fatal(err)
	}
	o, _ = s.Order(ctx, 1, o.ID)
	if o.FulfillmentStatus != "partial" || o.Items[0].ShippedQuantity != 2 || o.Items[1].ShippedQuantity != 0 {
		t.Fatalf("per-line totals %+v", o.Items)
	}
	in = shipInput(o, "multi-line-physical", 2)
	in.Method = "pickup"
	in.Items = []ShipmentItem{{LineNumber: 2, Quantity: 2}}
	if _, err = s.CreateShipment(ctx, 1, o.ID, "operator", in); err != nil {
		t.Fatal(err)
	}
	o, _ = s.Order(ctx, 1, o.ID)
	if o.FulfillmentStatus != "partial" {
		t.Fatal("partial pickup incorrectly shipped all items")
	}
	in = shipInput(o, "multi-line-final", 1)
	in.Method = "courier"
	in.Carrier = "Generic Logistics"
	in.TrackingNumber = "PKG-1"
	in.Items = []ShipmentItem{{LineNumber: 2, Quantity: 1}}
	if _, err = s.CreateShipment(ctx, 1, o.ID, "operator", in); err != nil {
		t.Fatal(err)
	}
	o, _ = s.Order(ctx, 1, o.ID)
	if o.FulfillmentStatus != "shipped" || len(o.Shipments) != 3 || o.Items[1].ShippedQuantity != 3 {
		t.Fatalf("mixed delivery %+v", o)
	}
	results, err := s.FilterOrders(ctx, 1, 1, "shipped", "PKG-1")
	if err != nil || len(results) != 1 {
		t.Fatalf("tracking search %+v %v", results, err)
	}
}
