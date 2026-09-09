package main

import (
	"bilvie/internal/db"
	"bilvie/internal/generator"
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"path/filepath"
)

func main() {
	database := flag.String("db", "../data/site.db", "SQLite database")
	web := flag.String("web", "../web", "published web root")
	templates := flag.String("templates", "templates", "template directory")
	data := flag.String("data", "../data", "publication state directory")
	assets := flag.String("assets", "../assets", "public resource directory")
	theme := flag.String("theme", "", "active theme resource directory")
	flag.Parse()
	themeRoot := *theme
	if themeRoot == "" {
		themeRoot = filepath.Join(*assets, "theme", "blue")
	}
	d, e := db.Open(*database)
	if e != nil {
		log.Fatal(e)
	}
	defer d.Close()
	if e = db.EnsureUnifiedCategories(context.Background(), d); e != nil {
		log.Fatal(e)
	}
	if e = db.EnsureContent(context.Background(), d); e != nil {
		log.Fatal(e)
	}
	if e = db.EnsureMessages(context.Background(), d); e != nil {
		log.Fatal(e)
	}
	if e = db.EnsureTemplateAssignments(context.Background(), d); e != nil {
		log.Fatal(e)
	}
	r, e := (generator.Publisher{DB: d, Web: *web, Templates: *templates, Data: *data, Assets: *assets, Theme: themeRoot}).Generate(context.Background())
	_ = json.NewEncoder(os.Stdout).Encode(r)
	if e != nil {
		log.Fatal(e)
	}
}
