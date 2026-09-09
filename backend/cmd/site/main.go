package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"gocms/internal/db"
	"gocms/internal/site"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "__apply-update" {
		if err := site.ApplyUpdate(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}

	root := flag.String("root", "../web", "published public root")
	databasePath := flag.String("db", "../data/site.db", "SQLite database path")
	address := flag.String("addr", ":8080", "HTTP listen address")
	templates := flag.String("templates", "templates", "Go template directory")
	data := flag.String("data", "../data", "publication state directory")
	frontend := flag.String("frontend", "../frontend/dist", "built admin SPA")
	assets := flag.String("assets", "../assets", "public resource directory")
	theme := flag.String("theme", "", "active theme resource directory")
	flag.Parse()
	themeRoot := *theme
	if themeRoot == "" {
		themeRoot = filepath.Join(*assets, "theme", "blue")
	}

	database, err := db.Open(*databasePath)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()
	if err := db.EnsureUnifiedCategories(context.Background(), database); err != nil {
		log.Fatal(err)
	}
	if err := db.EnsureContent(context.Background(), database); err != nil {
		log.Fatal(err)
	}
	if err := db.EnsureMessages(context.Background(), database); err != nil {
		log.Fatal(err)
	}
	if err := db.EnsureTemplateAssignments(context.Background(), database); err != nil {
		log.Fatal(err)
	}
	if err := database.Ping(); err != nil {
		log.Fatalf("cannot use SQLite database %s: %v; run cmd/migrate first", *databasePath, err)
	}

	server, err := site.New(database, filepath.Clean(*root))
	if err != nil {
		log.Fatal(err)
	}
	server.ConfigurePublishing(*templates, *data, *frontend, *assets, themeRoot)
	log.Printf("Go site listening on http://%s", *address)
	log.Fatal(http.ListenAndServe(*address, server.Handler()))
}
