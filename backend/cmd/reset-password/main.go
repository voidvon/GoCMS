package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"

	"gocms/internal/auth"
	"gocms/internal/db"
)

func main() {
	databasePath := flag.String("db", "../data/site.db", "SQLite database path")
	username := flag.String("username", "", "administrator username")
	password := flag.String("password", "", "new administrator password")
	flag.Parse()

	if strings.TrimSpace(*username) == "" || *password == "" {
		log.Fatal("-username and -password are required")
	}

	database, err := db.Open(*databasePath)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()

	ctx := context.Background()
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer transaction.Rollback()

	var id int64
	if err := transaction.QueryRowContext(ctx, `
		SELECT "Id" FROM "benming_master" WHERE "UserName" = ?`, strings.TrimSpace(*username)).Scan(&id); err != nil {
		log.Fatalf("find administrator: %v", err)
	}

	hash, err := auth.HashPassword(*password)
	if err != nil {
		log.Fatalf("hash administrator password: %v", err)
	}
	if _, err := transaction.ExecContext(ctx, `
		UPDATE "benming_master" SET "PassWord" = ?, "LastLogin" = NULL, "LastLoginIp" = NULL
		WHERE "Id" = ?`, hash, id); err != nil {
		log.Fatalf("update administrator password: %v", err)
	}
	if err := transaction.Commit(); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("password reset for %s using Argon2id\n", strings.TrimSpace(*username))
}
