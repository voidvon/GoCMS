package commerce

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"gocms/internal/payment"
)

type Attempt struct {
	ID            string `json:"id"`
	OrderID       string `json:"order_id"`
	AccountID     int64  `json:"account_id"`
	Status        string `json:"status"`
	TransactionID string `json:"transaction_id"`
	PaymentURL    string `json:"payment_url,omitempty"`
	Error         string `json:"error"`
	CreatedAt     string `json:"created_at"`
}

type Refund struct {
	ID        string `json:"id"`
	AttemptID string `json:"attempt_id"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	Status    string `json:"status"`
	RefundID  string `json:"refund_id"`
	Error     string `json:"error"`
}

const orderSelect = `SELECT o.id,o.buyer_id,o.amount,o.currency,o.status,o.fulfillment_status,o.paid_attempt,o.email,o.customer_name,o.address_json,o.created_at,o.revision,o.source,o.payment_account_id,i.offer_id,i.name,i.kind,i.unit_price,i.quantity,i.image_url,i.content_id,i.sku,i.specifications_json FROM gocms_commerce_order o JOIN gocms_commerce_order_item i ON i.order_id=o.id AND i.line_number=1`

func scanOrder(row scanner) (Order, error) {
	var o Order
	var address, specifications string
	err := row.Scan(&o.ID, &o.BuyerID, &o.Amount, &o.Currency, &o.Status, &o.FulfillmentStatus, &o.PaidAttempt, &o.Email, &o.CustomerName, &address, &o.CreatedAt, &o.Revision, &o.Source, &o.PaymentAccountID, &o.Item.ID, &o.Item.Name, &o.Item.Kind, &o.Item.Amount, &o.Quantity, &o.Item.ImageURL, &o.Item.ContentID, &o.Item.SKU, &specifications)
	if errors.Is(err, sql.ErrNoRows) {
		return o, ErrNotFound
	}
	if err != nil {
		return o, err
	}
	if err := json.Unmarshal([]byte(specifications), &o.Item.Specifications); err != nil {
		return o, err
	}
	err = json.Unmarshal([]byte(address), &o.Address)
	o.Item.Currency = o.Currency
	return o, err
}

func (s Service) Orders(ctx context.Context, site, buyer int64, page int) ([]Order, error) {
	return s.orders(ctx, site, buyer, page, "", "")
}
func (s Service) FilterOrders(ctx context.Context, site int64, page int, status, q string) ([]Order, error) {
	return s.orders(ctx, site, 0, page, status, q)
}
func (s Service) orders(ctx context.Context, site, buyer int64, page int, status, q string) ([]Order, error) {
	filter := ""
	args := []any{site}
	if buyer > 0 {
		filter = " AND o.buyer_id=?"
		args = append(args, buyer)
	}
	if len(q) > 128 {
		return nil, ErrInvalid
	}
	if q != "" {
		filter += ` AND (instr(o.id,?)>0 OR instr(o.customer_name,?)>0 OR instr(o.address_json,?)>0 OR EXISTS(SELECT 1 FROM gocms_commerce_shipment sh WHERE sh.order_id=o.id AND instr(sh.tracking_number,?)>0))`
		args = append(args, q, q, q, q)
	}
	switch status {
	case "":
	case "unpaid", "refunded":
		filter += ` AND o.status=?`
		args = append(args, status)
	case "pending", "partial", "shipped", "completed":
		filter += ` AND o.status='paid' AND o.fulfillment_status=?`
		args = append(args, status)
	default:
		return nil, ErrInvalid
	}
	args = append(args, (page-1)*50)
	rows, err := s.DB.QueryContext(ctx, orderSelect+` WHERE o.site_id=?`+filter+` ORDER BY o.created_at DESC,o.id DESC LIMIT 50 OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Order{}
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := s.populateOrderLines(ctx, items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s Service) Order(ctx context.Context, site int64, id string) (Order, error) {
	o, err := scanOrder(s.DB.QueryRowContext(ctx, orderSelect+` WHERE o.site_id=? AND o.id=?`, site, id))
	if err != nil {
		return o, err
	}
	orders := []Order{o}
	if err := s.populateOrderLines(ctx, orders); err != nil {
		return o, err
	}
	o = orders[0]
	rows, err := s.DB.QueryContext(ctx, `SELECT id,order_id,account_id,status,transaction_id,payment_url,error,created_at FROM gocms_payment_attempt WHERE order_id=? ORDER BY created_at,id`, id)
	if err != nil {
		return o, err
	}
	o.Attempts = []Attempt{}
	for rows.Next() {
		var a Attempt
		if err := rows.Scan(&a.ID, &a.OrderID, &a.AccountID, &a.Status, &a.TransactionID, &a.PaymentURL, &a.Error, &a.CreatedAt); err != nil {
			rows.Close()
			return o, err
		}
		o.Attempts = append(o.Attempts, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return o, err
	}
	rows, err = s.DB.QueryContext(ctx, `SELECT r.id,r.attempt_id,r.amount,r.currency,r.status,r.refund_id,r.error FROM gocms_payment_refund r JOIN gocms_payment_attempt a ON a.id=r.attempt_id WHERE a.order_id=? ORDER BY r.created_at`, id)
	if err != nil {
		return o, err
	}
	defer rows.Close()
	o.Refunds = []Refund{}
	for rows.Next() {
		var r Refund
		if err := rows.Scan(&r.ID, &r.AttemptID, &r.Amount, &r.Currency, &r.Status, &r.RefundID, &r.Error); err != nil {
			return o, err
		}
		o.Refunds = append(o.Refunds, r)
	}
	if err := rows.Err(); err != nil {
		return o, err
	}
	rows.Close()
	o.Shipments, err = s.Shipments(ctx, site, id)
	return o, err
}

func (s Service) Pay(ctx context.Context, site int64, id string, accountID int64, ip string) (Attempt, error) {
	// IP is supplied by the trusted HTTP boundary, never by the buyer's JSON.
	if net.ParseIP(ip) == nil {
		return Attempt{}, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Attempt{}, err
	}
	defer tx.Rollback()
	var status, currency string
	var fixedAccount int64
	if err := tx.QueryRowContext(ctx, `SELECT status,currency,payment_account_id FROM gocms_commerce_order WHERE id=? AND site_id=?`, id, site).Scan(&status, &currency, &fixedAccount); err != nil {
		return Attempt{}, ErrNotFound
	}
	if status != "unpaid" {
		return Attempt{}, ErrConflict
	}
	if fixedAccount != 0 && fixedAccount != accountID {
		return Attempt{}, ErrInvalid
	}
	a, err := scanAccount(tx.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM gocms_payment_account WHERE id=? AND site_id=?`, accountID, site))
	if err != nil {
		return Attempt{}, err
	}
	if !a.Enabled || a.Currency != currency {
		return Attempt{}, ErrInvalid
	}
	var attempt Attempt
	err = tx.QueryRowContext(ctx, `SELECT id,account_id,payment_url,status FROM gocms_payment_attempt WHERE order_id=? AND status IN ('creating','pending','failed')`, id).Scan(&attempt.ID, &attempt.AccountID, &attempt.PaymentURL, &attempt.Status)
	if err == nil {
		if attempt.AccountID != accountID {
			return Attempt{}, ErrConflict
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		attempt.ID, err = randomID("pay_")
		if err != nil {
			return Attempt{}, err
		}
		attempt.AccountID = accountID
		_, err = tx.ExecContext(ctx, `INSERT INTO gocms_payment_attempt(id,order_id,account_id,next_sync) VALUES(?,?,?,?)`, attempt.ID, id, accountID, time.Now().Unix()+60)
		if err != nil {
			return Attempt{}, err
		}
	} else {
		return Attempt{}, err
	}
	if err := tx.Commit(); err != nil {
		return Attempt{}, err
	}
	attempt.OrderID = id
	if attempt.PaymentURL != "" {
		return attempt, nil
	}
	o, err := s.Order(ctx, site, id)
	if err != nil {
		return attempt, err
	}
	if len(o.Items) == 0 {
		return attempt, ErrInvalid
	}
	p, credentials, err := s.provider(a)
	if err != nil {
		s.recordAttemptError(ctx, attempt.ID, err)
		return attempt, err
	}

	checkoutItems := make([]payment.CheckoutItem, 0, len(o.Items))
	for _, line := range o.Items {
		imageURL := line.ImageURL
		if strings.HasPrefix(imageURL, "/") {
			imageURL = a.PublicURL + imageURL
		}
		checkoutItems = append(checkoutItems, payment.CheckoutItem{Name: line.Name, Kind: line.Kind, Quantity: line.Quantity, UnitPrice: line.Amount, ImageURL: imageURL})
	}
	returnURL := a.PublicURL + a.ReturnPath + "?payment_order=" + o.ID
	if o.Source == "admin" {
		returnURL = a.PublicURL + "/shop/pay/" + o.ID + "?returned=1"
	}
	r, err := p.Create(ctx, credentials, payment.Checkout{Reference: attempt.ID, Amount: o.Amount, Currency: o.Currency, Items: checkoutItems, Name: o.Item.Name, Kind: o.Item.Kind, Quantity: o.Quantity, UnitPrice: o.Item.Amount, ImageURL: checkoutItems[0].ImageURL, Email: o.Email, CustomerName: o.CustomerName, Address: payment.Address{Street: o.Address.Street, Postcode: o.Address.Postcode, City: o.Address.City, State: o.Address.State, Country: o.Address.Country}, IP: ip, NotifyURL: a.PublicURL + "/api/payments/webhooks/" + a.PublicID, ReturnURL: returnURL})
	if err == nil {
		if r.Reference != attempt.ID {
			err = ErrInvalid
		} else {
			err = s.Apply(ctx, a, r, "")
		}
		if err == nil {
			_, err = s.DB.ExecContext(ctx, `UPDATE gocms_payment_attempt SET payment_url=? WHERE id=?`, r.PaymentURL, attempt.ID)
			attempt.PaymentURL = r.PaymentURL
			attempt.Status = "pending"
			attempt.TransactionID = r.TransactionID
		}
	}
	if err != nil {
		s.recordAttemptError(ctx, attempt.ID, err)
	}
	return attempt, err
}

func (s Service) recordAttemptError(ctx context.Context, id string, err error) {
	_, _ = s.DB.ExecContext(ctx, `UPDATE gocms_payment_attempt SET error=?,next_sync=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND status NOT IN ('succeeded','closed')`, safeError(err), time.Now().Unix()+120, id)
}

func safeError(err error) string {
	if errors.Is(err, payment.ErrInvalid) {
		return "支付响应校验失败，请检查账户或联系服务商"
	}
	return "交易结果待确认，请同步状态；请勿另建订单重复付款"
}

func (s Service) Sync(ctx context.Context, site int64, id string) error {
	var accountID int64
	if err := s.DB.QueryRowContext(ctx, `SELECT a.account_id FROM gocms_payment_attempt a JOIN gocms_commerce_order o ON o.id=a.order_id WHERE a.id=? AND o.site_id=?`, id, site).Scan(&accountID); err != nil {
		return ErrNotFound
	}
	a, err := scanAccount(s.DB.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM gocms_payment_account WHERE id=? AND site_id=?`, accountID, site))
	if err != nil {
		return err
	}
	p, credentials, err := s.provider(a)
	if err != nil {
		s.recordAttemptError(ctx, id, err)
		return err
	}
	r, err := p.Query(ctx, credentials, id)
	if err == nil {
		if r.Reference != id {
			err = ErrInvalid
		} else {
			err = s.Apply(ctx, a, r, "")
		}
	}
	if err != nil {
		s.recordAttemptError(ctx, id, err)
	}
	return err
}

// Apply validates the financial identity before atomically recording a provider
// result and transitioning the order. A successful payment is never downgraded.
func (s Service) Apply(ctx context.Context, a Account, r payment.Result, eventDigest string) error {
	if r.Kind != "payment" && r.Kind != "refund" {
		return ErrInvalid
	}
	if r.Status != "succeeded" && r.Status != "pending" && r.Status != "failed" && r.Status != "closed" {
		return ErrInvalid
	}
	if r.TransactionID == "" {
		return ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var orderID, transactionID, oldStatus, currency, paidAttempt string
	var amount int64
	err = tx.QueryRowContext(ctx, `SELECT p.order_id,p.transaction_id,p.status,o.amount,o.currency,o.paid_attempt FROM gocms_payment_attempt p JOIN gocms_commerce_order o ON o.id=p.order_id WHERE p.id=? AND p.account_id=? AND o.site_id=?`, r.Reference, a.ID, a.SiteID).Scan(&orderID, &transactionID, &oldStatus, &amount, &currency, &paidAttempt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if r.Amount != amount || r.Currency != currency || (transactionID != "" && transactionID != r.TransactionID) {
		return ErrInvalid
	}
	if eventDigest != "" {
		res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO gocms_payment_event(account_id,digest,reference,kind,status) VALUES(?,?,?,?,?)`, a.ID, eventDigest, r.Reference, r.Kind, r.Status)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return tx.Commit()
		}
	}
	if r.Kind == "refund" {
		var refundAmount int64
		var refundCurrency, refundStatus, refundID string
		err = tx.QueryRowContext(ctx, `SELECT amount,currency,status,refund_id FROM gocms_payment_refund WHERE id=? AND attempt_id=?`, r.RefundReference, r.Reference).Scan(&refundAmount, &refundCurrency, &refundStatus, &refundID)
		if err != nil {
			return ErrNotFound
		}
		if refundAmount != r.Amount || refundCurrency != r.Currency || r.RefundID == "" || (refundID != "" && refundID != r.RefundID) {
			return ErrInvalid
		}
		if refundStatus != "succeeded" {
			_, err = tx.ExecContext(ctx, `UPDATE gocms_payment_refund SET status=?,refund_id=?,error='',next_sync=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, r.Status, r.RefundID, time.Now().Unix()+120, r.RefundReference)
			if err != nil {
				return err
			}
			if r.Status == "succeeded" && paidAttempt == r.Reference {
				_, err = tx.ExecContext(ctx, `UPDATE gocms_commerce_order SET status='refunded',updated_at=CURRENT_TIMESTAMP WHERE id=?`, orderID)
				if err != nil {
					return err
				}
				_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO gocms_commerce_outbox(order_id,kind) VALUES(?,'order.refunded')`, orderID)
				if err != nil {
					return err
				}
			}
		}
		return tx.Commit()
	}
	if oldStatus != "succeeded" {
		// CLOSED/CANCEL can only be superseded by verified success. FAILED is
		// nonterminal in PingPong's default cashier mode.
		if oldStatus != "closed" || r.Status == "succeeded" {
			problem := ""
			if r.Status == "succeeded" && paidAttempt != "" && paidAttempt != r.Reference {
				problem = "重复收款，需要人工退款"
			}
			_, err = tx.ExecContext(ctx, `UPDATE gocms_payment_attempt SET status=?,transaction_id=?,error=?,next_sync=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, r.Status, r.TransactionID, problem, time.Now().Unix()+120, r.Reference)
			if err != nil {
				return err
			}
		}
	}
	if r.Status == "succeeded" && paidAttempt == "" {
		_, err = tx.ExecContext(ctx, `UPDATE gocms_commerce_order SET status='paid',paid_attempt=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND paid_attempt=''`, r.Reference, orderID)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO gocms_commerce_outbox(order_id,kind) VALUES(?,'order.paid')`, orderID)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s Service) Notify(ctx context.Context, publicID string, raw []byte) error {
	a, err := scanAccount(s.DB.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM gocms_payment_account WHERE public_id=?`, publicID))
	if err != nil {
		return err
	}
	p, credentials, err := s.provider(a)
	if err != nil {
		return err
	}
	r, err := p.Notification(credentials, raw)
	if err != nil {
		return err
	}
	return s.Apply(ctx, a, r, digest(raw))
}

func (s Service) RefundAttempt(ctx context.Context, site int64, attemptID string) (Refund, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Refund{}, err
	}
	defer tx.Rollback()
	var r Refund
	var status string
	submit := false
	err = tx.QueryRowContext(ctx, `SELECT a.status,o.amount,o.currency FROM gocms_payment_attempt a JOIN gocms_commerce_order o ON o.id=a.order_id WHERE a.id=? AND o.site_id=?`, attemptID, site).Scan(&status, &r.Amount, &r.Currency)
	if err != nil {
		return r, ErrNotFound
	}
	if status != "succeeded" {
		return r, ErrConflict
	}
	err = tx.QueryRowContext(ctx, `SELECT id,status FROM gocms_payment_refund WHERE attempt_id=?`, attemptID).Scan(&r.ID, &r.Status)
	if errors.Is(err, sql.ErrNoRows) {
		submit = true
		r.ID, err = randomID("ref_")
		if err != nil {
			return r, err
		}
		r.Status = "creating"
		_, err = tx.ExecContext(ctx, `INSERT INTO gocms_payment_refund(id,attempt_id,amount,currency,next_sync) VALUES(?,?,?,?,?)`, r.ID, attemptID, r.Amount, r.Currency, time.Now().Unix()+60)
		if err != nil {
			return r, err
		}
	} else if err != nil {
		return r, err
	}
	if err := tx.Commit(); err != nil {
		return r, err
	}
	r.AttemptID = attemptID
	if r.Status == "succeeded" {
		return r, nil
	}
	err = s.syncRefund(ctx, site, r.ID, submit || r.Status == "failed" || r.Status == "closed")
	return r, err
}

func (s Service) SyncRefund(ctx context.Context, site int64, id string) error {
	return s.syncRefund(ctx, site, id, false)
}

func (s Service) syncRefund(ctx context.Context, site int64, id string, submit bool) (err error) {
	var r Refund
	var accountID int64
	err = s.DB.QueryRowContext(ctx, `SELECT r.id,r.attempt_id,r.amount,r.currency,r.status,a.account_id FROM gocms_payment_refund r JOIN gocms_payment_attempt a ON a.id=r.attempt_id JOIN gocms_commerce_order o ON o.id=a.order_id WHERE r.id=? AND o.site_id=?`, id, site).Scan(&r.ID, &r.AttemptID, &r.Amount, &r.Currency, &r.Status, &accountID)
	if err != nil {
		return ErrNotFound
	}
	defer func() {
		if err != nil {
			_, _ = s.DB.ExecContext(ctx, `UPDATE gocms_payment_refund SET error=?,next_sync=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND status!='succeeded'`, safeError(err), time.Now().Unix()+120, id)
		}
	}()
	a, err := scanAccount(s.DB.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM gocms_payment_account WHERE id=?`, accountID))
	if err != nil {
		return err
	}
	p, credentials, err := s.provider(a)
	if err != nil {
		return err
	}
	var result payment.Result
	if submit {
		result, err = p.Refund(ctx, credentials, payment.Refund{Reference: r.ID, PaymentReference: r.AttemptID, Amount: r.Amount, Currency: r.Currency, NotifyURL: a.PublicURL + "/api/payments/webhooks/" + a.PublicID})
	} else {
		result, err = p.QueryRefund(ctx, credentials, r.ID, r.AttemptID)
		// A lost submit response must be queried before reusing the refund ID.
		// If the process stopped before sending, retry that same ID; never make
		// a second refund identity to resolve an ambiguous outcome.
		if err != nil && r.Status == "creating" && !errors.Is(err, payment.ErrInvalid) {
			result, err = p.Refund(ctx, credentials, payment.Refund{Reference: r.ID, PaymentReference: r.AttemptID, Amount: r.Amount, Currency: r.Currency, NotifyURL: a.PublicURL + "/api/payments/webhooks/" + a.PublicID})
		}
	}
	if err == nil {
		if result.RefundReference == "" {
			result.RefundReference = r.ID
		}
		if result.RefundReference != r.ID || result.Reference != r.AttemptID {
			err = ErrInvalid
		} else {
			err = s.Apply(ctx, a, result, "")
		}
	}
	return err
}

func (s Service) Fulfill(ctx context.Context, site int64, id, status string) error {
	if status != "processing" && status != "completed" && status != "failed" {
		return ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := tx.ExecContext(ctx, `UPDATE gocms_commerce_order SET fulfillment_status=?,revision=revision+1,updated_at=CURRENT_TIMESTAMP WHERE id=? AND site_id=? AND status='paid' AND fulfillment_status<>'completed' AND NOT EXISTS(SELECT 1 FROM gocms_payment_refund r JOIN gocms_payment_attempt a ON a.id=r.attempt_id WHERE a.order_id=gocms_commerce_order.id AND r.status IN ('creating','pending','failed')) AND NOT EXISTS(SELECT 1 FROM gocms_commerce_shipment WHERE order_id=gocms_commerce_order.id) AND NOT EXISTS(SELECT 1 FROM gocms_commerce_order_item WHERE order_id=gocms_commerce_order.id AND kind='physical')`, status, id, site)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return ErrConflict
	}
	if status == "completed" {
		if _, err := tx.ExecContext(ctx, `UPDATE gocms_commerce_outbox SET processed=1 WHERE order_id=? AND kind='order.paid'`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Reconcile survives process restarts because work and retry times live in SQL.
// A bounded batch and context prevent a broken provider from monopolizing it.
func (s Service) Reconcile(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT p.id,o.site_id FROM gocms_payment_attempt p JOIN gocms_commerce_order o ON o.id=p.order_id WHERE p.status IN ('creating','pending','failed') AND p.next_sync<=? ORDER BY p.next_sync LIMIT 15`, time.Now().Unix())
	if err != nil {
		return err
	}
	type task struct {
		id   string
		site int64
	}
	tasks := []task{}
	for rows.Next() {
		var t task
		if err := rows.Scan(&t.id, &t.site); err != nil {
			rows.Close()
			return err
		}
		tasks = append(tasks, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_ = s.Sync(ctx, t.site, t.id)
	}
	rows, err = s.DB.QueryContext(ctx, `SELECT r.id,o.site_id FROM gocms_payment_refund r JOIN gocms_payment_attempt p ON p.id=r.attempt_id JOIN gocms_commerce_order o ON o.id=p.order_id WHERE r.status IN ('creating','pending') AND r.next_sync<=? ORDER BY r.next_sync LIMIT 15`, time.Now().Unix())
	if err != nil {
		return err
	}
	tasks = nil
	for rows.Next() {
		var t task
		if err := rows.Scan(&t.id, &t.site); err != nil {
			rows.Close()
			return err
		}
		tasks = append(tasks, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_ = s.syncRefund(ctx, t.site, t.id, false)
	}
	return nil
}

func (s Service) Events(ctx context.Context, site int64, page int) ([]map[string]any, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT e.id,e.reference,e.kind,e.status,e.created_at FROM gocms_payment_event e JOIN gocms_payment_account a ON a.id=e.account_id WHERE a.site_id=? ORDER BY e.id DESC LIMIT 50 OFFSET ?`, site, (page-1)*50)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var ref, kind, status, created string
		if err := rows.Scan(&id, &ref, &kind, &status, &created); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{"id": strconv.FormatInt(id, 10), "reference": ref, "kind": kind, "status": status, "created_at": created})
	}
	return items, rows.Err()
}

func (s Service) populateOrderLines(ctx context.Context, orders []Order) error {
	if len(orders) == 0 {
		return nil
	}
	placeholders := make([]string, len(orders))
	args := make([]any, len(orders))
	indexes := map[string]int{}
	for i := range orders {
		placeholders[i] = "?"
		args[i] = orders[i].ID
		indexes[orders[i].ID] = i
		orders[i].Items = []OrderLine{}
		orders[i].Quantity = 0
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT order_id,offer_id,name,kind,unit_price,quantity,image_url,content_id,sku,specifications_json,line_number,(SELECT COALESCE(SUM(si.quantity),0) FROM gocms_commerce_shipment_item si WHERE si.order_id=gocms_commerce_order_item.order_id AND si.line_number=gocms_commerce_order_item.line_number) FROM gocms_commerce_order_item WHERE order_id IN (`+strings.Join(placeholders, ",")+`) ORDER BY order_id,line_number`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, raw string
		var line OrderLine
		if err := rows.Scan(&id, &line.ID, &line.Name, &line.Kind, &line.Amount, &line.Quantity, &line.ImageURL, &line.ContentID, &line.SKU, &raw, &line.LineNumber, &line.ShippedQuantity); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(raw), &line.Specifications); err != nil {
			return err
		}
		index := indexes[id]
		line.Currency = orders[index].Currency
		orders[index].Items = append(orders[index].Items, line)
		orders[index].Quantity += line.Quantity
	}
	return rows.Err()
}
