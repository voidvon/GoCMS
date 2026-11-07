package db

import (
	"context"
	"database/sql"
)

func ensureShipments(ctx context.Context, database *sql.DB) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS gocms_commerce_shipment (
 id TEXT PRIMARY KEY, order_id TEXT NOT NULL REFERENCES gocms_commerce_order(id),
 idempotency_key TEXT NOT NULL, request_hash TEXT NOT NULL, method TEXT NOT NULL,
 carrier TEXT NOT NULL, tracking_number TEXT NOT NULL, tracking_url TEXT NOT NULL,
 shipped_at TEXT NOT NULL, note TEXT NOT NULL, internal_note TEXT NOT NULL,
 created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, UNIQUE(order_id,idempotency_key))`,
		`CREATE TABLE IF NOT EXISTS gocms_commerce_shipment_item (
 shipment_id TEXT NOT NULL REFERENCES gocms_commerce_shipment(id), order_id TEXT NOT NULL,
 line_number INTEGER NOT NULL, quantity INTEGER NOT NULL CHECK(quantity>0),
 PRIMARY KEY(shipment_id,line_number), FOREIGN KEY(order_id,line_number) REFERENCES gocms_commerce_order_item(order_id,line_number))`,
		`CREATE INDEX IF NOT EXISTS idx_shipment_order ON gocms_commerce_shipment(order_id)`,
		`CREATE INDEX IF NOT EXISTS idx_shipment_item_order ON gocms_commerce_shipment_item(order_id,line_number)`,
		`CREATE TABLE IF NOT EXISTS gocms_commerce_order_event (
 id INTEGER PRIMARY KEY AUTOINCREMENT, order_id TEXT NOT NULL REFERENCES gocms_commerce_order(id),
 actor TEXT NOT NULL, action TEXT NOT NULL, detail TEXT NOT NULL,
 created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
	} {
		if _, err := database.ExecContext(ctx, q); err != nil {
			return err
		}
	}

	for _, c := range []struct{ name, definition string }{
		{"notice_status", "TEXT NOT NULL DEFAULT 'skipped'"}, {"notice_next", "INTEGER NOT NULL DEFAULT 0"}, {"notice_attempts", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := ensureColumn(ctx, database, "gocms_commerce_shipment", c.name, c.definition); err != nil {
			return err
		}
	}
	return nil
}
