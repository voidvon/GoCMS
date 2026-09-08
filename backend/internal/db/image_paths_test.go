package db

import (
	"context"
	"testing"
)

func TestNormalizeImagePaths(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := CreateSchema(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	_, err = database.Exec(`
		INSERT INTO "benming_ch_prod" ("id", "smallpic", "bigpic", "itemize")
		VALUES (1, '/UploadFile/produppic/example.jpg', '/skin/dfpic.gif', '<img src="http://www.bilvie.com/UploadFile/body.png">')`)
	if err != nil {
		t.Fatal(err)
	}
	if err := NormalizeImagePaths(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	var small, big, content string
	if err := database.QueryRow(`SELECT "smallpic", "bigpic", "itemize" FROM "benming_ch_prod" WHERE "id" = 1`).Scan(&small, &big, &content); err != nil {
		t.Fatal(err)
	}
	if small != "/images/example.jpg" || big != "" || content != `<img src="/images/body.png">` {
		t.Fatalf("unexpected normalized paths: %q %q %q", small, big, content)
	}
}
