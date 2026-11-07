package commerce

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
	"time"
)

type ShipmentItem struct {
	LineNumber int64 `json:"line_number"`
	Quantity   int64 `json:"quantity"`
}
type Shipment struct {
	NoticeStatus   string         `json:"-"`
	ID             string         `json:"id"`
	Method         string         `json:"method"`
	Carrier        string         `json:"carrier"`
	TrackingNumber string         `json:"tracking_number"`
	TrackingURL    string         `json:"tracking_url"`
	ShippedAt      string         `json:"shipped_at"`
	Note           string         `json:"note"`
	Items          []ShipmentItem `json:"items"`
	InternalNote   string         `json:"-"`
}
type ShipmentInput struct {
	Notify         bool           `json:"-"`
	Revision       int64          `json:"revision"`
	Key            string         `json:"idempotency_key"`
	Method         string         `json:"method"`
	Carrier        string         `json:"carrier"`
	TrackingNumber string         `json:"tracking_number"`
	TrackingURL    string         `json:"tracking_url"`
	ShippedAt      string         `json:"shipped_at"`
	Note           string         `json:"note"`
	InternalNote   string         `json:"internal_note"`
	Items          []ShipmentItem `json:"items"`
	Reason         string         `json:"reason"`
}
type OrderEvent struct {
	ID        int64  `json:"id"`
	Actor     string `json:"actor"`
	Action    string `json:"action"`
	Detail    string `json:"detail"`
	CreatedAt string `json:"created_at"`
}

func (s Service) Shipments(ctx context.Context, site int64, order string) ([]Shipment, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT p.id,p.method,p.carrier,p.tracking_number,p.tracking_url,p.shipped_at,p.note,p.internal_note,p.notice_status FROM gocms_commerce_shipment p JOIN gocms_commerce_order o ON o.id=p.order_id WHERE o.site_id=? AND o.id=? ORDER BY p.created_at,p.id`, site, order)
	if err != nil {
		return nil, err
	}
	result := []Shipment{}
	for rows.Next() {
		var p Shipment
		if err = rows.Scan(&p.ID, &p.Method, &p.Carrier, &p.TrackingNumber, &p.TrackingURL, &p.ShippedAt, &p.Note, &p.InternalNote, &p.NoticeStatus); err != nil {
			rows.Close()
			return nil, err
		}
		p.Items = []ShipmentItem{}
		result = append(result, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = s.DB.QueryContext(ctx, `SELECT i.shipment_id,i.line_number,i.quantity FROM gocms_commerce_shipment_item i JOIN gocms_commerce_order o ON o.id=i.order_id WHERE o.site_id=? AND o.id=? ORDER BY i.line_number`, site, order)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var item ShipmentItem
		if err = rows.Scan(&id, &item.LineNumber, &item.Quantity); err != nil {
			return nil, err
		}
		for i := range result {
			if result[i].ID == id {
				result[i].Items = append(result[i].Items, item)
				break
			}
		}
	}
	return result, rows.Err()
}
func (s Service) OrderEvents(ctx context.Context, site int64, order string) ([]OrderEvent, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT e.id,e.actor,e.action,e.detail,e.created_at FROM gocms_commerce_order_event e JOIN gocms_commerce_order o ON o.id=e.order_id WHERE o.site_id=? AND o.id=? ORDER BY e.id DESC`, site, order)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OrderEvent{}
	for rows.Next() {
		var e OrderEvent
		if err := rows.Scan(&e.ID, &e.Actor, &e.Action, &e.Detail, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
func shipmentValid(in ShipmentInput) bool {
	if in.Method != "courier" && in.Method != "pickup" && in.Method != "no_shipping" {
		return false
	}
	for _, v := range []string{in.Carrier, in.TrackingNumber, in.TrackingURL, in.Note, in.InternalNote, in.Reason} {
		if len(v) > 2000 || strings.ContainsRune(v, '\x00') {
			return false
		}
	}
	if len(in.Carrier) > 100 || len(in.TrackingNumber) > 128 || strings.ContainsAny(in.Carrier+in.TrackingNumber, "\r\n") {
		return false
	}
	if in.Method == "courier" && (strings.TrimSpace(in.Carrier) == "" || strings.TrimSpace(in.TrackingNumber) == "") {
		return false
	}
	if in.Method != "courier" && (in.TrackingNumber != "" || in.TrackingURL != "" || in.Carrier != "") {
		return false
	}
	if in.TrackingURL != "" {
		u, err := url.Parse(in.TrackingURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
			return false
		}
	}
	when, err := time.Parse(time.RFC3339, in.ShippedAt)
	return err == nil && !when.After(time.Now().Add(5*time.Minute))
}

// All logistics writes acquire the order's write lock before reading allocation totals.
// The revision also rejects stale browser dialogs and cross-operation races.
func lockOrder(ctx context.Context, tx *sql.Tx, site int64, order string, revision int64) error {
	r, err := tx.ExecContext(ctx, `UPDATE gocms_commerce_order SET revision=revision+1,updated_at=CURRENT_TIMESTAMP WHERE site_id=? AND id=? AND revision=?`, site, order, revision)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrConflict
	}
	return nil
}
func shippingAllowed(ctx context.Context, tx *sql.Tx, order string) error {
	var status, fulfillment string
	var refunds int
	if err := tx.QueryRowContext(ctx, `SELECT status,fulfillment_status FROM gocms_commerce_order WHERE id=?`, order).Scan(&status, &fulfillment); err != nil {
		return err
	}
	if status != "paid" || fulfillment == "completed" {
		return ErrConflict
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM gocms_payment_refund r JOIN gocms_payment_attempt a ON a.id=r.attempt_id WHERE a.order_id=? AND r.status IN ('creating','pending','failed')`, order).Scan(&refunds); err != nil {
		return err
	}
	if refunds > 0 {
		return ErrConflict
	}
	return nil
}
func orderEvent(ctx context.Context, tx *sql.Tx, order, actor, action string, detail any) error {
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO gocms_commerce_order_event(order_id,actor,action,detail) VALUES(?,?,?,?)`, order, actor, action, string(raw))
	return err
}

func (s Service) CreateShipment(ctx context.Context, site int64, order, actor string, in ShipmentInput) (string, error) {
	if !shipmentValid(in) || len(in.Key) < 16 || len(in.Key) > 128 || len(in.Items) == 0 || len(in.Items) > 50 {
		return "", ErrInvalid
	}
	raw, _ := json.Marshal(in)
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	// A no-op write serializes duplicate retries even when their original revision is stale.
	r, err := tx.ExecContext(ctx, `UPDATE gocms_commerce_order SET revision=revision WHERE site_id=? AND id=?`, site, order)
	if err != nil {
		return "", err
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return "", ErrNotFound
	}
	var existing, oldHash string
	err = tx.QueryRowContext(ctx, `SELECT id,request_hash FROM gocms_commerce_shipment WHERE order_id=? AND idempotency_key=?`, order, in.Key).Scan(&existing, &oldHash)
	if err == nil {
		if oldHash != hash {
			return "", ErrConflict
		}
		return existing, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	if err = lockOrder(ctx, tx, site, order, in.Revision); err != nil {
		return "", err
	}
	if err = shippingAllowed(ctx, tx, order); err != nil {
		return "", err
	}
	var address, created string
	if err = tx.QueryRowContext(ctx, `SELECT address_json,created_at FROM gocms_commerce_order WHERE id=?`, order).Scan(&address, &created); err != nil {
		return "", err
	}
	var addr Address
	_ = json.Unmarshal([]byte(address), &addr)
	if in.Method == "courier" && (strings.TrimSpace(addr.Phone) == "" || strings.TrimSpace(addr.Street) == "" || addr.Country == "") {
		return "", ErrInvalid
	}
	createdTime, _ := time.Parse("2006-01-02 15:04:05", created)
	shippedTime, _ := time.Parse(time.RFC3339, in.ShippedAt)
	if !createdTime.IsZero() && shippedTime.Before(createdTime) {
		return "", ErrInvalid
	}
	seen := map[int64]bool{}
	for _, item := range in.Items {
		if item.Quantity <= 0 || seen[item.LineNumber] {
			return "", ErrInvalid
		}
		seen[item.LineNumber] = true
		var qty, shipped int64
		var kind string
		err = tx.QueryRowContext(ctx, `SELECT quantity,kind,(SELECT COALESCE(SUM(quantity),0) FROM gocms_commerce_shipment_item WHERE order_id=? AND line_number=?) FROM gocms_commerce_order_item WHERE order_id=? AND line_number=?`, order, item.LineNumber, order, item.LineNumber).Scan(&qty, &kind, &shipped)
		if err == sql.ErrNoRows {
			return "", ErrInvalid
		}
		if err != nil {
			return "", err
		}
		if item.Quantity > qty-shipped {
			return "", ErrConflict
		}
		if (kind == "physical" && in.Method == "no_shipping") || (kind != "physical" && in.Method == "courier") {
			return "", ErrInvalid
		}
	}
	id, err := randomID("shp_")
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO gocms_commerce_shipment(id,order_id,idempotency_key,request_hash,method,carrier,tracking_number,tracking_url,shipped_at,note,internal_note) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, id, order, in.Key, hash, in.Method, in.Carrier, in.TrackingNumber, in.TrackingURL, in.ShippedAt, in.Note, in.InternalNote)
	if err != nil {
		return "", err
	}
	if in.Notify {
		if _, err = tx.ExecContext(ctx, `UPDATE gocms_commerce_shipment SET notice_status='pending' WHERE id=?`, id); err != nil {
			return "", err
		}
	}
	for _, item := range in.Items {
		if _, err = tx.ExecContext(ctx, `INSERT INTO gocms_commerce_shipment_item(shipment_id,order_id,line_number,quantity) VALUES(?,?,?,?)`, id, order, item.LineNumber, item.Quantity); err != nil {
			return "", err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE gocms_commerce_order SET fulfillment_status=CASE WHEN (SELECT SUM(quantity) FROM gocms_commerce_shipment_item WHERE order_id=?)=(SELECT SUM(quantity) FROM gocms_commerce_order_item WHERE order_id=?) THEN 'shipped' ELSE 'partial' END WHERE id=?`, order, order, order)
	if err != nil {
		return "", err
	}
	if err = orderEvent(ctx, tx, order, actor, "shipment.created", map[string]any{"shipment_id": id, "input": in}); err != nil {
		return "", err
	}
	return id, tx.Commit()
}
func (s Service) EditShipment(ctx context.Context, site int64, order, id, actor string, in ShipmentInput) error {
	if !shipmentValid(in) || !validText(in.Reason, 500) {
		return ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockOrder(ctx, tx, site, order, in.Revision); err != nil {
		return err
	}
	var before Shipment
	err = tx.QueryRowContext(ctx, `SELECT method,carrier,tracking_number,tracking_url,shipped_at,note,internal_note FROM gocms_commerce_shipment WHERE id=? AND order_id=?`, id, order).Scan(&before.Method, &before.Carrier, &before.TrackingNumber, &before.TrackingURL, &before.ShippedAt, &before.Note, &before.InternalNote)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if before.Method != in.Method {
		return ErrInvalid
	}
	_, err = tx.ExecContext(ctx, `UPDATE gocms_commerce_shipment SET carrier=?,tracking_number=?,tracking_url=?,shipped_at=?,note=?,internal_note=? WHERE id=?`, in.Carrier, in.TrackingNumber, in.TrackingURL, in.ShippedAt, in.Note, in.InternalNote, id)
	if err != nil {
		return err
	}
	if err = orderEvent(ctx, tx, order, actor, "shipment.corrected", map[string]any{"shipment_id": id, "before": before, "before_internal_note": before.InternalNote, "after": in}); err != nil {
		return err
	}
	return tx.Commit()
}
func (s Service) CompleteOrder(ctx context.Context, site int64, order, actor string, revision int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockOrder(ctx, tx, site, order, revision); err != nil {
		return err
	}
	if err = shippingAllowed(ctx, tx, order); err != nil {
		return err
	}
	var remaining int64
	if err = tx.QueryRowContext(ctx, `SELECT (SELECT COALESCE(SUM(quantity),0) FROM gocms_commerce_order_item WHERE order_id=?)-(SELECT COALESCE(SUM(quantity),0) FROM gocms_commerce_shipment_item WHERE order_id=?)`, order, order).Scan(&remaining); err != nil {
		return err
	}
	if remaining != 0 {
		return ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE gocms_commerce_order SET fulfillment_status='completed' WHERE id=?`, order); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE gocms_commerce_outbox SET processed=1 WHERE order_id=? AND kind='order.paid'`, order); err != nil {
		return err
	}
	if err = orderEvent(ctx, tx, order, actor, "order.completed", nil); err != nil {
		return err
	}
	return tx.Commit()
}
func (s Service) UpdateRecipient(ctx context.Context, site int64, order, actor string, revision int64, name string, address Address, reason string) error {
	if !validText(name, 100) || !validText(reason, 500) || !validText(address.Phone, 32) || !validText(address.Street, 256) || !validText(address.City, 50) || len(address.Country) != 2 || len(address.State) > 128 || len(address.Postcode) > 16 {
		return ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockOrder(ctx, tx, site, order, revision); err != nil {
		return err
	}
	var count int
	var status, before, oldName string
	if err = tx.QueryRowContext(ctx, `SELECT status,address_json,customer_name,(SELECT COUNT(*) FROM gocms_commerce_shipment WHERE order_id=?) FROM gocms_commerce_order WHERE id=?`, order, order).Scan(&status, &before, &oldName, &count); err != nil {
		return err
	}
	if count > 0 || status == "refunded" {
		return ErrConflict
	}
	raw, _ := json.Marshal(address)
	if _, err = tx.ExecContext(ctx, `UPDATE gocms_commerce_order SET customer_name=?,address_json=? WHERE id=?`, name, string(raw), order); err != nil {
		return err
	}
	if err = orderEvent(ctx, tx, order, actor, "recipient.updated", map[string]any{"before_name": oldName, "before_address": json.RawMessage(before), "name": name, "address": address, "reason": reason}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s Service) ShipmentCarriers(ctx context.Context, site int64) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT p.carrier FROM gocms_commerce_shipment p JOIN gocms_commerce_order o ON o.id=p.order_id WHERE o.site_id=? AND p.carrier<>'' ORDER BY p.carrier LIMIT 100`, site)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
