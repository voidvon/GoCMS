package db

import (
	"context"
	"testing"
)

func TestMigrateLegacyCategories(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	if err := CreateSchema(ctx, database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO "benming_ch_ProdCat" ("id", "CatName", "Root", "Orderid", "ListPath", "ListFilePattern", "ListTemplate", "DetailPath", "DetailFilePattern")
		VALUES (2, '产品', 0, 1, 'valve', '{id}.html', 'produts_sort.html', 'Product', '{id}.html'),
		       (10, '子产品', 2, 1, 'Products', '{id}.html', 'produts_sort2.html', 'Product', '{id}.html')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO "benming_ch_NewsCat" ("id", "CatName", "Root", "ORderID")
		VALUES (4, '新闻资讯', 0, 1), (6, '行业新闻', 4, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO "benming_ch_prod" ("id", "prodName", "CatId") VALUES (1, '产品', 10);
		INSERT INTO "benming_ch_news" ("newsid", "Title", "Typeid") VALUES (1, '新闻', 6);`); err != nil {
		t.Fatal(err)
	}

	if err := MigrateLegacyCategories(ctx, database); err != nil {
		t.Fatal(err)
	}
	var newsName, newsPath string
	if err := database.QueryRow(`SELECT "name", "list_path" FROM "bilvie_category" WHERE "route_id" = 6`).Scan(&newsName, &newsPath); err != nil {
		t.Fatal(err)
	}
	if newsName != "行业新闻" || newsPath != "news" {
		t.Fatalf("unexpected migrated categories: %q %q", newsName, newsPath)
	}
	var productCategory, newsCategory int64
	if err := database.QueryRow(`SELECT "CatId" FROM "benming_ch_prod" WHERE "id" = 1`).Scan(&productCategory); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT "Typeid" FROM "benming_ch_news" WHERE "newsid" = 1`).Scan(&newsCategory); err != nil {
		t.Fatal(err)
	}
	if productCategory == 0 || newsCategory == 0 || productCategory == newsCategory {
		t.Fatalf("content references were not separated: product=%d news=%d", productCategory, newsCategory)
	}
	var newsURLID int64
	if err := database.QueryRow(`SELECT "route_id" FROM "bilvie_category" WHERE "id" = ?`, newsCategory).Scan(&newsURLID); err != nil {
		t.Fatal(err)
	}
	if newsURLID != 6 {
		t.Fatalf("news route id changed: %d", newsURLID)
	}

	if err := MigrateLegacyCategories(ctx, database); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM "bilvie_category"`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 4 {
		t.Fatalf("migration was not idempotent: %d categories", count)
	}
}
