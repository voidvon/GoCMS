package db

import (
	"context"
	"strings"
	"testing"
)

func TestUnpricedSpecificationsMigrationPreservesOffersAndOrders(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	if err := CreateSchema(ctx, database); err != nil {
		t.Fatal(err)
	}
	var definition string
	if err := database.QueryRow(`SELECT sql FROM sqlite_master WHERE name='gocms_commerce_offer'`).Scan(&definition); err != nil {
		t.Fatal(err)
	}
	// Reproduce the prior schema, including an ID that was allocated and deleted.
	legacy := strings.Replace(definition, "CHECK(amount >= 0)", "CHECK(amount > 0)", 1)
	exec := func(statement string, args ...any) {
		t.Helper()
		if _, err := database.Exec(statement, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`DROP TABLE gocms_commerce_offer`)
	exec(legacy)
	exec(`CREATE UNIQUE INDEX idx_commerce_variant_sku ON gocms_commerce_offer(site_id,content_id,sku COLLATE NOCASE) WHERE content_id IS NOT NULL AND sku<>'' AND archived=0`)
	exec(`INSERT INTO gocms_commerce_offer(id,site_id,name,kind,amount,currency,image_url,content_id,enabled,created_at,fixed_prices_json,sku,specifications_json,archived)
 VALUES(42,1,'Display','service',1500,'USD','https://example.com/image.png',7,1,'2026-01-02 03:04:05','{"EUR":1400}','ORIGINAL','[{"name":"Capacity","value":"10","unit":"L"}]',0),
 (99,1,'Archived','physical',2500,'USD','',7,0,'2026-01-01 00:00:00','{}','ARCHIVED','[]',1),
 (200,1,'Deleted','service',100,'USD','',NULL,0,'2026-01-01 00:00:00','{}','DELETED','[]',0)`)
	exec(`DELETE FROM gocms_commerce_offer WHERE id=200`)
	exec(`INSERT INTO gocms_commerce_order(id,site_id,buyer_id,idempotency_key,request_hash,amount,currency,email,customer_name)
 VALUES('historical',1,9,'historical-key','hash',1500,'USD','buyer@example.com','Buyer')`)
	exec(`INSERT INTO gocms_commerce_order_item(order_id,line_number,offer_id,name,kind,unit_price,quantity,image_url,sku,specifications_json)
 VALUES('historical',1,42,'Snapshot','service',1500,1,'','ORIGINAL','[{"name":"Capacity","value":"10","unit":"L"}]')`)
	readRows := func(query string) string {
		t.Helper()
		var raw string
		if err := database.QueryRow(query).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	const offerSnapshot = `SELECT json_group_array(json_object('id',id,'site',site_id,'name',name,'kind',kind,'amount',amount,'currency',currency,'image',image_url,'content',content_id,'enabled',enabled,'created',created_at,'fixed',fixed_prices_json,'sku',sku,'specs',specifications_json,'archived',archived)) FROM (SELECT * FROM gocms_commerce_offer ORDER BY id)`
	const orderSnapshot = `SELECT json_object('amount',o.amount,'currency',o.currency,'offer',i.offer_id,'price',i.unit_price,'sku',i.sku,'specs',i.specifications_json) FROM gocms_commerce_order o JOIN gocms_commerce_order_item i ON i.order_id=o.id WHERE o.id='historical'`
	offersBefore, orderBefore := readRows(offerSnapshot), readRows(orderSnapshot)
	for i := 0; i < 2; i++ {
		if err := EnsureCommerce(ctx, database); err != nil {
			t.Fatal(err)
		}
	}
	if readRows(offerSnapshot) != offersBefore || readRows(orderSnapshot) != orderBefore {
		t.Fatal("migration changed existing offers or order snapshots")
	}
	exec(`INSERT INTO gocms_commerce_offer(site_id,name,kind,amount,currency,image_url) VALUES(1,'Unpriced','physical',0,'USD','')`)
	var id int64
	if err := database.QueryRow(`SELECT id FROM gocms_commerce_offer WHERE name='Unpriced'`).Scan(&id); err != nil || id <= 200 {
		t.Fatalf("offer ID sequence was reset: %d %v", id, err)
	}
	for _, statement := range []string{
		`INSERT INTO gocms_commerce_offer(site_id,name,kind,amount,currency,image_url) VALUES(1,'Invalid','physical',-1,'USD','')`,
		`INSERT INTO gocms_commerce_offer(site_id,name,kind,amount,currency,image_url,content_id,sku) VALUES(1,'Duplicate','physical',0,'USD','',7,'original')`,
		`UPDATE gocms_commerce_order SET amount=0 WHERE id='historical'`,
	} {
		if _, err := database.Exec(statement); err == nil {
			t.Fatalf("migration lost a constraint: %s", statement)
		}
	}
	var indexCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name IN ('idx_commerce_offer_site','idx_commerce_variant_sku')`).Scan(&indexCount); err != nil || indexCount != 2 {
		t.Fatalf("offer indexes not preserved: %d %v", indexCount, err)
	}
}
