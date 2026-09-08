package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"bilvie/internal/db"
)

func main() {
	databasePath := flag.String("db", "../data/site.db", "SQLite database path")
	flag.Parse()

	database, err := db.Open(*databasePath)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()
	if err := db.NormalizeImagePaths(context.Background(), database); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("normalized image paths in %s\n", *databasePath)
}
