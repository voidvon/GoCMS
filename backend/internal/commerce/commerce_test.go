package commerce

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"gocms/internal/db"
	"gocms/internal/payment"
)

type fakeProvider struct {
	checkout      payment.Checkout
	mu            sync.Mutex
	payments      map[string]payment.Result
	refunds       map[string]payment.Result
	timeout       bool
	refundTimeout bool
	refundCalls   int
}

func (f *fakeProvider) Validate(payment.Account) error { return nil }
func (f *fakeProvider) Create(_ context.Context, _ payment.Account, c payment.Checkout) (payment.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checkout = c
	r, ok := f.payments[c.Reference]
	if !ok {
		r = payment.Result{Reference: c.Reference, TransactionID: "tx_" + c.Reference, Amount: c.Amount, Currency: c.Currency, Status: "pending", PaymentURL: "https://sandbox-safepay.pingpongx.com?token=test", Kind: "payment"}
		f.payments[c.Reference] = r
	}
	if f.timeout {
		return payment.Result{}, payment.ErrUnavailable
	}
	return r, nil
}
func (f *fakeProvider) Query(_ context.Context, _ payment.Account, id string) (payment.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.payments[id]
	if !ok {
		return r, payment.ErrUnavailable
	}
	return r, nil
}
func (f *fakeProvider) Refund(_ context.Context, _ payment.Account, r payment.Refund) (payment.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refundCalls++
	result, ok := f.refunds[r.Reference]
	if !ok {
		p := f.payments[r.PaymentReference]
		result = payment.Result{Reference: r.PaymentReference, TransactionID: p.TransactionID, RefundReference: r.Reference, RefundID: "refund_tx", Amount: r.Amount, Currency: r.Currency, Status: "pending", Kind: "refund"}
		f.refunds[r.Reference] = result
	}
	if f.refundTimeout {
		return payment.Result{}, payment.ErrUnavailable
	}
	return result, nil
}
func (f *fakeProvider) QueryRefund(_ context.Context, _ payment.Account, id, original string) (payment.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.refunds[id]
	if !ok {
		return r, payment.ErrUnavailable
	}
	return r, nil
}
func (f *fakeProvider) Notification(_ payment.Account, raw []byte) (payment.Result, error) {
	var r payment.Result
	err := json.Unmarshal(raw, &r)
	return r, err
}

func fixture(t *testing.T) (Service, *fakeProvider, Account, Offer) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	ctx := context.Background()
	if err := db.CreateSchema(ctx, database); err != nil {
		t.Fatal(err)
	}
	cipher, err := payment.NewCipher(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeProvider{payments: map[string]payment.Result{}, refunds: map[string]payment.Result{}}
	s := Service{DB: database, Cipher: cipher, Providers: map[string]payment.Provider{"test": f}}
	a, err := s.CreateAccount(ctx, 1, Account{Name: "Sandbox", Provider: "test", Environment: "sandbox", ClientID: "client", MerchantID: "merchant", Secret: "never-return-this-salt", Currency: "USD", PublicURL: "https://shop.example", ReturnPath: "/buy.html", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	categoryResult, err := database.ExecContext(ctx, `INSERT INTO gocms_category(site_id,name,commerce_enabled) VALUES(1,'任意栏目',1)`)
	if err != nil {
		t.Fatal(err)
	}
	categoryID, _ := categoryResult.LastInsertId()
	contentResult, err := database.ExecContext(ctx, `INSERT INTO gocms_content(site_id,category_id,title,cover_image,visible) VALUES(1,?,'任意行业的服务','https://shop.example/image.png',1)`, categoryID)
	if err != nil {
		t.Fatal(err)
	}
	contentID, _ := contentResult.LastInsertId()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := s.SaveProduct(ctx, tx, 1, contentID, Product{Enabled: true, Kind: "service", Currency: "USD", Variants: []Variant{{Amount: 1234, Enabled: true}}}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	offers, err := s.Offers(ctx, 1, true, 1)
	if err != nil || len(offers) != 1 {
		t.Fatalf("content variants: %+v %v", offers, err)
	}
	o := offers[0]
	return s, f, a, o
}
func orderInput(o Offer) OrderInput {
	return OrderInput{OfferID: o.ID, Quantity: 2, Email: "buyer@example.com", CustomerName: "Buyer Name"}
}
func create(t *testing.T, s Service, o Offer, key string) Order {
	t.Helper()
	r, err := s.CreateOrder(context.Background(), 1, 9, key, orderInput(o))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestOrderSnapshotIdempotencyAndSiteIsolation(t *testing.T) {
	s, _, a, o := fixture(t)
	ctx := context.Background()
	in := orderInput(o)
	order := create(t, s, o, "idempotent-order-1")
	o.Name = "改价后的名称"
	o.Amount = 9999
	if _, err := s.SaveOffer(ctx, 1, o); err != nil {
		t.Fatal(err)
	}
	again, err := s.CreateOrder(ctx, 1, 9, "idempotent-order-1", in)
	if err != nil || again.ID != order.ID || again.Amount != 2468 || again.Item.Name != order.Item.Name {
		t.Fatalf("snapshot changed: %+v %v", again, err)
	}
	in.Quantity = 1
	if _, err := s.CreateOrder(ctx, 1, 9, "idempotent-order-1", in); !errors.Is(err, ErrConflict) {
		t.Fatal("idempotency reused for different request")
	}
	if _, err := s.Order(ctx, 2, order.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-site order exposed")
	}
	if _, err := s.Pay(ctx, 2, order.ID, a.ID, "127.0.0.1"); err == nil {
		t.Fatal("cross-site payment allowed")
	}
	var ciphertext string
	if err := s.DB.QueryRow(`SELECT secret_ciphertext FROM gocms_payment_account WHERE id=?`, a.ID).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ciphertext, "never-return-this-salt") {
		t.Fatal("plaintext secret persisted")
	}
	accounts, err := s.Accounts(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(accounts)
	if strings.Contains(string(raw), "salt") || strings.Contains(string(raw), "ciphertext") {
		t.Fatal("credential returned by API")
	}
	if err := s.UpdateAccount(ctx, 2, a.ID, "unauthorized", false); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-site account mutation")
	}
}

func TestConcurrentPaymentRetryUsesOneReference(t *testing.T) {
	s, f, a, o := fixture(t)
	order := create(t, s, o, "concurrent-order-1")
	ctx := context.Background()
	var wg sync.WaitGroup
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.Pay(ctx, 1, order.ID, a.ID, "127.0.0.1"); failures <- err }()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(f.payments) != 1 {
		t.Fatal("duplicate remote payment created")
	}
	detail, err := s.Order(ctx, 1, order.ID)
	if err != nil || len(detail.Attempts) != 1 {
		t.Fatal("duplicate payment attempt")
	}
	if err := s.UpdateAccount(ctx, 1, a.ID, "disabled", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pay(ctx, 1, order.ID, a.ID, "127.0.0.1"); err == nil {
		t.Fatal("disabled account accepted new payment")
	}
	if err := s.Sync(ctx, 1, detail.Attempts[0].ID); err != nil {
		t.Fatal("disabled account could not query old payment")
	}
}

func TestLostResponseReconciliationAfterRestart(t *testing.T) {
	s, f, a, o := fixture(t)
	ctx := context.Background()
	order := create(t, s, o, "lost-response-order")
	f.timeout = true
	if _, err := s.Pay(ctx, 1, order.ID, a.ID, "127.0.0.1"); !errors.Is(err, payment.ErrUnavailable) {
		t.Fatal("expected ambiguous network failure")
	}
	f.timeout = false
	detail, err := s.Order(ctx, 1, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	attempt := detail.Attempts[0]
	r := f.payments[attempt.ID]
	r.Status = "succeeded"
	f.payments[attempt.ID] = r
	if _, err := s.DB.Exec(`UPDATE gocms_payment_attempt SET next_sync=0`); err != nil {
		t.Fatal(err)
	}
	restarted := Service{DB: s.DB, Cipher: s.Cipher, Providers: s.Providers}
	if err := restarted.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	detail, err = s.Order(ctx, 1, order.ID)
	if err != nil || detail.Status != "paid" {
		t.Fatalf("not recovered: %+v %v", detail, err)
	}
	if len(f.payments) != 1 {
		t.Fatal("recovery created a duplicate charge")
	}
}

func TestVerifiedPaymentAndRefundAreAtomicAndMonotonic(t *testing.T) {
	s, f, a, o := fixture(t)
	ctx := context.Background()
	order := create(t, s, o, "paid-refund-order-1")
	attempt, err := s.Pay(ctx, 1, order.ID, a.ID, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	r := f.payments[attempt.ID]
	r.Status = "succeeded"
	bad := r
	bad.Amount++
	if err := s.Apply(ctx, a, bad, "bad-amount"); err == nil {
		t.Fatal("wrong payment amount accepted")
	}
	bad = r
	bad.TransactionID = "other"
	if err := s.Apply(ctx, a, bad, "bad-tx"); err == nil {
		t.Fatal("wrong transaction accepted")
	}
	for i := 0; i < 3; i++ {
		if err := s.Apply(ctx, a, r, "same-event"); err != nil {
			t.Fatal(err)
		}
	}
	late := r
	late.Status = "failed"
	if err := s.Apply(ctx, a, late, "late-failure"); err != nil {
		t.Fatal(err)
	}
	detail, err := s.Order(ctx, 1, order.ID)
	if err != nil || detail.Status != "paid" || detail.Attempts[0].Status != "succeeded" {
		t.Fatal("late notification downgraded payment")
	}
	var count int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM gocms_commerce_outbox WHERE kind='order.paid'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate or missing fulfillment event")
	}
	if err := s.Fulfill(ctx, 1, order.ID, "completed"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pay(ctx, 1, order.ID, a.ID, "127.0.0.1"); !errors.Is(err, ErrConflict) {
		t.Fatal("paid order accepted another payment")
	}
	refund, err := s.RefundAttempt(ctx, 1, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.RefundAttempt(ctx, 1, attempt.ID)
	if err != nil || again.ID != refund.ID || len(f.refunds) != 1 {
		t.Fatal("duplicate refund created")
	}
	r = f.refunds[refund.ID]
	r.Status = "succeeded"
	if err := s.Apply(ctx, a, r, "refund-success"); err != nil {
		t.Fatal(err)
	}
	r.Status = "failed"
	if err := s.Apply(ctx, a, r, "refund-old-failure"); err != nil {
		t.Fatal(err)
	}
	detail, err = s.Order(ctx, 1, order.ID)
	if err != nil || detail.Status != "refunded" || detail.Refunds[0].Status != "succeeded" || detail.FulfillmentStatus != "completed" {
		t.Fatal("refund status or delivery state incorrect")
	}
	if err := s.Fulfill(ctx, 1, order.ID, "processing"); !errors.Is(err, ErrConflict) {
		t.Fatal("refunded order newly fulfilled")
	}
}

func TestPhysicalOrderRequiresAddressAndCrossSiteContentRejected(t *testing.T) {
	s, _, _, o := fixture(t)
	ctx := context.Background()
	o.Kind = "physical"
	if _, err := s.DB.Exec(`UPDATE gocms_commerce_product SET kind='physical' WHERE content_id=?`, o.ContentID); err != nil {
		t.Fatal(err)
	}
	o, err := s.SaveOffer(ctx, 1, o)
	if err != nil {
		t.Fatal(err)
	}
	in := orderInput(o)
	if _, err := s.CreateOrder(ctx, 1, 9, "physical-order-key", in); !errors.Is(err, ErrInvalid) {
		t.Fatal("physical order accepted without shipping address")
	}
	in.Address = Address{Street: "123 Road", Postcode: "12345", City: "Town", Country: "US"}
	if _, err := s.CreateOrder(ctx, 1, 9, "physical-order-key", in); err != nil {
		t.Fatal(err)
	}
	badID := int64(999)
	o.ContentID = &badID
	if _, err := s.SaveOffer(ctx, 1, o); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid content binding accepted")
	}
}

func TestPaymentPersistenceFailureRollsBackNotificationAndCanRetry(t *testing.T) {
	s, f, a, o := fixture(t)
	ctx := context.Background()
	order := create(t, s, o, "rollback-event-order")
	attempt, err := s.Pay(ctx, 1, order.ID, a.ID, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	r := f.payments[attempt.ID]
	r.Status = "succeeded"
	if _, err := s.DB.Exec(`CREATE TRIGGER simulate_outbox_failure BEFORE INSERT ON gocms_commerce_outbox BEGIN SELECT RAISE(ABORT,'simulated disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(ctx, a, r, "retryable-event"); err == nil {
		t.Fatal("persistence failure was acknowledged")
	}
	detail, err := s.Order(ctx, 1, order.ID)
	if err != nil || detail.Status != "unpaid" || detail.Attempts[0].Status != "pending" {
		t.Fatal("partially committed financial update")
	}
	var count int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM gocms_payment_event`).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed notification retained deduplication marker")
	}
	if _, err := s.DB.Exec(`DROP TRIGGER simulate_outbox_failure`); err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(ctx, a, r, "retryable-event"); err != nil {
		t.Fatal(err)
	}
	detail, err = s.Order(ctx, 1, order.ID)
	if err != nil || detail.Status != "paid" {
		t.Fatal("notification could not recover after rollback")
	}
}

func TestLateSuccessOnClosedAttemptDoesNotDuplicateFulfillment(t *testing.T) {
	s, f, a, o := fixture(t)
	ctx := context.Background()
	order := create(t, s, o, "late-success-order")
	first, err := s.Pay(ctx, 1, order.ID, a.ID, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	r := f.payments[first.ID]
	r.Status = "closed"
	if err := s.Apply(ctx, a, r, "first-closed"); err != nil {
		t.Fatal(err)
	}
	second, err := s.Pay(ctx, 1, order.ID, a.ID, "127.0.0.1")
	if err != nil || second.ID == first.ID {
		t.Fatal("closed attempt could not be replaced")
	}
	paid := f.payments[second.ID]
	paid.Status = "succeeded"
	if err := s.Apply(ctx, a, paid, "second-paid"); err != nil {
		t.Fatal(err)
	}
	r.Status = "succeeded"
	if err := s.Apply(ctx, a, r, "first-late-success"); err != nil {
		t.Fatal(err)
	}
	detail, err := s.Order(ctx, 1, order.ID)
	if err != nil || detail.PaidAttempt != second.ID || detail.Status != "paid" {
		t.Fatal("late success changed the original fulfillment identity")
	}
	var problem string
	var count int
	if err := s.DB.QueryRow(`SELECT error FROM gocms_payment_attempt WHERE id=?`, first.ID).Scan(&problem); err != nil || problem == "" {
		t.Fatal("duplicate charge not flagged for refund")
	}
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM gocms_commerce_outbox WHERE kind='order.paid'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate fulfillment event")
	}
	refund, err := s.RefundAttempt(ctx, 1, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	r = f.refunds[refund.ID]
	r.Status = "succeeded"
	f.refunds[refund.ID] = r
	if err := s.SyncRefund(ctx, 1, refund.ID); err != nil {
		t.Fatal(err)
	}
	detail, err = s.Order(ctx, 1, order.ID)
	if err != nil || detail.Status != "paid" {
		t.Fatal("refunding duplicate charge reversed the original order")
	}
}

func TestLostRefundResponseRecoveredByQueryAfterRestart(t *testing.T) {
	s, f, a, o := fixture(t)
	ctx := context.Background()
	order := create(t, s, o, "refund-recovery-key")
	attempt, err := s.Pay(ctx, 1, order.ID, a.ID, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	r := f.payments[attempt.ID]
	r.Status = "succeeded"
	if err := s.Apply(ctx, a, r, "payment-success"); err != nil {
		t.Fatal(err)
	}
	f.refundTimeout = true
	refund, err := s.RefundAttempt(ctx, 1, attempt.ID)
	if !errors.Is(err, payment.ErrUnavailable) {
		t.Fatal("expected lost refund response")
	}
	f.refundTimeout = false
	r = f.refunds[refund.ID]
	r.Status = "succeeded"
	f.refunds[refund.ID] = r
	if _, err := s.DB.Exec(`UPDATE gocms_payment_refund SET next_sync=0`); err != nil {
		t.Fatal(err)
	}
	restarted := Service{DB: s.DB, Cipher: s.Cipher, Providers: s.Providers}
	if err := restarted.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	detail, err := s.Order(ctx, 1, order.ID)
	if err != nil || detail.Status != "refunded" || f.refundCalls != 1 {
		t.Fatalf("refund recovery resubmitted or lost result: %+v calls=%d err=%v", detail, f.refundCalls, err)
	}
}
