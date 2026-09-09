package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"gocms/internal/db"
)

func main() {
	accessPath := flag.String("access", "../legacy/database/kerfm!!@@##.asa", "path to the Access database")
	sqlitePath := flag.String("sqlite", "../data/site.db", "output SQLite database path")
	force := flag.Bool("force", false, "replace an existing SQLite database")
	flag.Parse()

	report, err := db.ImportAccess(context.Background(), *accessPath, *sqlitePath, *force)
	if err != nil {
		log.Fatal(err)
	}
	for _, table := range db.AccessTables {
		fmt.Printf("%-32s %d rows\n", table.Name, report.Tables[table.Name])
	}
	fmt.Printf("Total: %d rows -> %s\n", report.Total, *sqlitePath)
	_ = os.Stdout.Sync()
}
