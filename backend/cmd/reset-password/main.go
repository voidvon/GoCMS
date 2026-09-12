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
	if err := db.CreateSchema(ctx, database); err != nil {
		log.Fatal(err)
	}
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer transaction.Rollback()

	hash, err := auth.HashPassword(*password)
	if err != nil {
		log.Fatalf("hash administrator password: %v", err)
	}
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO "gocms_admin_user"
		("username", "password_hash", "flags", "last_login", "last_login_ip", "is_super")
		VALUES (?, ?, '', NULL, NULL, 1)
		ON CONFLICT ("username") DO UPDATE SET
			"password_hash" = excluded."password_hash",
			"last_login" = NULL,
			"last_login_ip" = NULL`, strings.TrimSpace(*username), hash); err != nil {
		log.Fatalf("save administrator password: %v", err)
	}
	if err := transaction.Commit(); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("password reset for %s using Argon2id\n", strings.TrimSpace(*username))
}
