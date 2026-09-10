package main

import (
	"context"
	"encoding/json"
	"flag"
	"gocms/internal/db"
	"gocms/internal/generator"
	"gocms/internal/theme"
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
	themeOverride := flag.String("theme", "", "theme resource directory override")
	flag.Parse()
	d, e := db.Open(*database)
	if e != nil {
		log.Fatal(e)
	}
	defer d.Close()
	if e = db.CreateSchema(context.Background(), d); e != nil {
		log.Fatal(e)
	}
	themeDefinition, e := theme.Resolve(filepath.Join(*assets, "theme"), *data, *themeOverride, *templates)
	if e != nil {
		log.Fatal(e)
	}
	r, e := (generator.Publisher{DB: d, Web: *web, Templates: themeDefinition.TemplatesRoot, Data: *data, Assets: *assets, Theme: themeDefinition.AssetsRoot}).Generate(context.Background())
	_ = json.NewEncoder(os.Stdout).Encode(r)
	if e != nil {
		log.Fatal(e)
	}
}
