package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Financial records intentionally restrict site deletion and are never part of
// the content publishing pipeline or ordinary content deletion cascades.
func EnsureCommerce(ctx context.Context, database *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS gocms_commerce_dimension (
		 site_id INTEGER NOT NULL REFERENCES gocms_site(id), dimension_key TEXT NOT NULL,
		 identity_key TEXT NOT NULL, name_key TEXT NOT NULL, definition_json TEXT NOT NULL,
		 version INTEGER NOT NULL DEFAULT 1, PRIMARY KEY(site_id,dimension_key), UNIQUE(site_id,identity_key))`,
		`CREATE TABLE IF NOT EXISTS gocms_commerce_pricing (site_id INTEGER PRIMARY KEY REFERENCES gocms_site(id), settings_json TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS gocms_commerce_offer (
		 id INTEGER PRIMARY KEY AUTOINCREMENT, site_id INTEGER NOT NULL REFERENCES gocms_site(id),
		 name TEXT NOT NULL, kind TEXT NOT NULL CHECK(kind IN ('physical','digital','service')),
		 amount INTEGER NOT NULL CHECK(amount >= 0), currency TEXT NOT NULL, image_url TEXT NOT NULL,
		 content_id INTEGER, enabled INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_commerce_offer_site ON gocms_commerce_offer(site_id,id)`,
		`CREATE TABLE IF NOT EXISTS gocms_commerce_product (
		 content_id INTEGER PRIMARY KEY REFERENCES gocms_content(id) ON DELETE CASCADE,
		 site_id INTEGER NOT NULL REFERENCES gocms_site(id), enabled INTEGER NOT NULL DEFAULT 0,
		 kind TEXT NOT NULL CHECK(kind IN ('physical','digital','service')), currency TEXT NOT NULL,
		 dimensions_json TEXT NOT NULL DEFAULT '[]'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_commerce_product_site ON gocms_commerce_product(site_id,content_id)`,
		`CREATE TABLE IF NOT EXISTS gocms_payment_account (
		 id INTEGER PRIMARY KEY AUTOINCREMENT, site_id INTEGER NOT NULL REFERENCES gocms_site(id),
		 public_id TEXT NOT NULL UNIQUE, name TEXT NOT NULL, provider TEXT NOT NULL,
		 environment TEXT NOT NULL, client_id TEXT NOT NULL, merchant_id TEXT NOT NULL,
		 secret_ciphertext TEXT NOT NULL, currency TEXT NOT NULL, public_url TEXT NOT NULL, return_path TEXT NOT NULL DEFAULT '/',
		 enabled INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_payment_account_site ON gocms_payment_account(site_id,id)`,
		`CREATE TABLE IF NOT EXISTS gocms_commerce_order (
		 id TEXT PRIMARY KEY, site_id INTEGER NOT NULL REFERENCES gocms_site(id), buyer_id INTEGER NOT NULL,
		 idempotency_key TEXT NOT NULL, request_hash TEXT NOT NULL,
		 amount INTEGER NOT NULL CHECK(amount > 0), currency TEXT NOT NULL,
		 status TEXT NOT NULL DEFAULT 'unpaid', fulfillment_status TEXT NOT NULL DEFAULT 'pending',
		 paid_attempt TEXT NOT NULL DEFAULT '', email TEXT NOT NULL, customer_name TEXT NOT NULL,
		 address_json TEXT NOT NULL DEFAULT '{}', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
		 updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
		 UNIQUE(site_id,buyer_id,idempotency_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_commerce_order_site ON gocms_commerce_order(site_id,created_at,id)`,
		`CREATE TABLE IF NOT EXISTS gocms_commerce_order_item (
		 order_id TEXT PRIMARY KEY REFERENCES gocms_commerce_order(id), offer_id INTEGER NOT NULL,
		 name TEXT NOT NULL, kind TEXT NOT NULL, unit_price INTEGER NOT NULL, quantity INTEGER NOT NULL,
		 image_url TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS gocms_payment_attempt (
		 id TEXT PRIMARY KEY, order_id TEXT NOT NULL REFERENCES gocms_commerce_order(id),
		 account_id INTEGER NOT NULL REFERENCES gocms_payment_account(id), status TEXT NOT NULL DEFAULT 'creating',
		 transaction_id TEXT NOT NULL DEFAULT '', payment_url TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT '',
		 next_sync INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
		 updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_attempt_active ON gocms_payment_attempt(order_id) WHERE status IN ('creating','pending','failed')`,
		`CREATE INDEX IF NOT EXISTS idx_payment_attempt_sync ON gocms_payment_attempt(status,next_sync)`,
		`CREATE TABLE IF NOT EXISTS gocms_payment_refund (
		 id TEXT PRIMARY KEY, attempt_id TEXT NOT NULL UNIQUE REFERENCES gocms_payment_attempt(id),
		 amount INTEGER NOT NULL CHECK(amount > 0), currency TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'creating',
		 refund_id TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT '', next_sync INTEGER NOT NULL DEFAULT 0,
		 created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_payment_refund_sync ON gocms_payment_refund(status,next_sync)`,
		`CREATE TABLE IF NOT EXISTS gocms_payment_event (
		 id INTEGER PRIMARY KEY AUTOINCREMENT, account_id INTEGER NOT NULL REFERENCES gocms_payment_account(id),
		 digest TEXT NOT NULL, reference TEXT NOT NULL, kind TEXT NOT NULL, status TEXT NOT NULL,
		 created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, UNIQUE(account_id,digest)
		)`,
		`CREATE TABLE IF NOT EXISTS gocms_commerce_outbox (
		 id INTEGER PRIMARY KEY AUTOINCREMENT, order_id TEXT NOT NULL REFERENCES gocms_commerce_order(id),
		 kind TEXT NOT NULL, processed INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
		 UNIQUE(order_id,kind)
		)`,
	}
	for _, statement := range statements {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure commerce: %w", err)
		}
	}
	for _, column := range []struct{ table, name, definition string }{
		{"gocms_commerce_product", "dimensions_json", "TEXT NOT NULL DEFAULT '[]'"},
		{"gocms_commerce_order", "language", "TEXT NOT NULL DEFAULT 'en'"},
		{"gocms_commerce_order", "source", "TEXT NOT NULL DEFAULT 'checkout'"},
		{"gocms_commerce_order", "payment_account_id", "INTEGER NOT NULL DEFAULT 0"},
		{"gocms_commerce_order", "payment_token_hash", "TEXT NOT NULL DEFAULT ''"},
		{"gocms_commerce_order", "revision", "INTEGER NOT NULL DEFAULT 0"},
		{"gocms_commerce_offer", "fixed_prices_json", "TEXT NOT NULL DEFAULT '{}'"},
		{"gocms_commerce_order", "pricing_json", "TEXT NOT NULL DEFAULT '{}'"},
		{"gocms_commerce_offer", "sku", "TEXT NOT NULL DEFAULT ''"},
		{"gocms_commerce_offer", "specifications_json", "TEXT NOT NULL DEFAULT '[]'"},
		{"gocms_commerce_offer", "archived", "INTEGER NOT NULL DEFAULT 0"},
		{"gocms_commerce_order_item", "content_id", "INTEGER"},
		{"gocms_commerce_order_item", "sku", "TEXT NOT NULL DEFAULT ''"},
		{"gocms_commerce_order_item", "specifications_json", "TEXT NOT NULL DEFAULT '[]'"},
	} {
		if err := ensureColumn(ctx, database, column.table, column.name, column.definition); err != nil {
			return err
		}
	}
	if err := ensureUnpricedSpecifications(ctx, database); err != nil {
		return err
	}
	if _, err := database.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_commerce_variant_sku ON gocms_commerce_offer(site_id,content_id,sku COLLATE NOCASE) WHERE content_id IS NOT NULL AND sku<>'' AND archived=0`); err != nil {
		return err
	}
	if err := ensureCommerceOrderLines(ctx, database); err != nil {
		return err
	}
	return ensureShipments(ctx, database)
}

// Allow display-only specifications to have no price. Orders and refunds keep
// their positive-amount constraints; offer IDs and historical snapshots survive.
func ensureUnpricedSpecifications(ctx context.Context, database *sql.DB) error {
	var definition string
	if err := database.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='table' AND name='gocms_commerce_offer'`).Scan(&definition); err != nil {
		return err
	}
	if !strings.Contains(definition, "CHECK(amount > 0)") {
		return nil
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Save indexes and triggers before replacing the table. Nothing references
	// offers with a foreign key: order items deliberately retain independent IDs.
	rows, err := tx.QueryContext(ctx, `SELECT sql FROM sqlite_master WHERE tbl_name='gocms_commerce_offer' AND type IN ('index','trigger') AND sql IS NOT NULL`)
	if err != nil {
		return err
	}
	var objects []string
	for rows.Next() {
		var statement string
		if err := rows.Scan(&statement); err != nil {
			rows.Close()
			return err
		}
		objects = append(objects, statement)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var sequence int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT seq FROM sqlite_sequence WHERE name='gocms_commerce_offer'),0)`).Scan(&sequence); err != nil {
		return err
	}
	statements := []string{
		`CREATE TABLE gocms_commerce_offer_unpriced (
 id INTEGER PRIMARY KEY AUTOINCREMENT, site_id INTEGER NOT NULL REFERENCES gocms_site(id),
 name TEXT NOT NULL, kind TEXT NOT NULL CHECK(kind IN ('physical','digital','service')),
 amount INTEGER NOT NULL CHECK(amount >= 0), currency TEXT NOT NULL, image_url TEXT NOT NULL,
 content_id INTEGER, enabled INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
 fixed_prices_json TEXT NOT NULL DEFAULT '{}', sku TEXT NOT NULL DEFAULT '',
 specifications_json TEXT NOT NULL DEFAULT '[]', archived INTEGER NOT NULL DEFAULT 0)`,
		`INSERT INTO gocms_commerce_offer_unpriced(id,site_id,name,kind,amount,currency,image_url,content_id,enabled,created_at,fixed_prices_json,sku,specifications_json,archived)
 SELECT id,site_id,name,kind,amount,currency,image_url,content_id,enabled,created_at,fixed_prices_json,sku,specifications_json,archived FROM gocms_commerce_offer`,
		`DROP TABLE gocms_commerce_offer`,
		`ALTER TABLE gocms_commerce_offer_unpriced RENAME TO gocms_commerce_offer`,
	}
	for _, statement := range append(statements, objects...) {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("upgrade unpriced specifications: %w", err)
		}
	}
	// Preserve the high-water mark, including IDs of previously deleted offers.
	if _, err := tx.ExecContext(ctx, `UPDATE sqlite_sequence SET seq=MAX(seq,?) WHERE name='gocms_commerce_offer'`, sequence); err != nil {
		return err
	}
	return tx.Commit()
}

// Upgrade the original one-row-per-order snapshot without changing historical
// order IDs, amounts, credentials or payment attempts.
func ensureCommerceOrderLines(ctx context.Context, database *sql.DB) error {
	var present int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('gocms_commerce_order_item') WHERE name='line_number'`).Scan(&present); err != nil {
		return err
	}
	if present != 0 {
		return nil
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE gocms_commerce_order_item_v2 (
 order_id TEXT NOT NULL REFERENCES gocms_commerce_order(id), line_number INTEGER NOT NULL DEFAULT 1,
 offer_id INTEGER NOT NULL, name TEXT NOT NULL, kind TEXT NOT NULL, unit_price INTEGER NOT NULL,
 quantity INTEGER NOT NULL, image_url TEXT NOT NULL, content_id INTEGER, sku TEXT NOT NULL DEFAULT '',
 specifications_json TEXT NOT NULL DEFAULT '[]', PRIMARY KEY(order_id,line_number))`,
		`INSERT INTO gocms_commerce_order_item_v2(order_id,line_number,offer_id,name,kind,unit_price,quantity,image_url,content_id,sku,specifications_json)
 SELECT order_id,1,offer_id,name,kind,unit_price,quantity,image_url,content_id,sku,specifications_json FROM gocms_commerce_order_item`,
		`DROP TABLE gocms_commerce_order_item`,
		`ALTER TABLE gocms_commerce_order_item_v2 RENAME TO gocms_commerce_order_item`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("upgrade commerce order lines: %w", err)
		}
	}
	return tx.Commit()
}
