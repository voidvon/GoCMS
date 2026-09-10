package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"gocms/internal/db"
	"gocms/internal/legacyimport"
)

func main() {
	databasePath := flag.String("db", "../data/site.db", "SQLite database path")
	flag.Parse()

	database, err := db.Open(*databasePath)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := db.CreateSchema(ctx, database); err != nil {
		log.Fatal(err)
	}
	if err := legacyimport.MigrateExistingSettings(ctx, database); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("migrated existing site settings into %s\n", *databasePath)
}
