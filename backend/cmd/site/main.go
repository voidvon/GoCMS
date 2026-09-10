package main

import (
	"context"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"gocms/internal/db"
	"gocms/internal/embedded"
	"gocms/internal/site"
	"gocms/internal/theme"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "__apply-update" {
		if err := site.ApplyUpdate(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}

	defaults := runtimeDefaults()
	root := flag.String("root", defaults.root, "published public root")
	databasePath := flag.String("db", defaults.database, "SQLite database path")
	address := flag.String("addr", ":8080", "HTTP listen address")
	templates := flag.String("templates", defaults.templates, "Go template directory")
	data := flag.String("data", defaults.data, "publication state directory")
	frontend := flag.String("frontend", defaults.frontend, "built admin SPA; empty uses the embedded SPA")
	assets := flag.String("assets", defaults.assets, "public resource directory")
	themeOverride := flag.String("theme", "", "theme resource directory override")
	flag.Parse()

	if *templates == "" {
		log.Fatal("template directory is empty")
	}
	if err := embedded.EnsureTemplates(filepath.Clean(*templates)); err != nil {
		log.Fatal(err)
	}
	var embeddedFrontend fs.FS
	if *frontend == "" {
		prepared, err := embedded.FrontendFS()
		if err != nil {
			log.Fatal(err)
		}
		embeddedFrontend = prepared
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
	themeDefinition, err := theme.Resolve(filepath.Join(*assets, "theme"), *data, *themeOverride, *templates)
	if err != nil {
		log.Fatal(err)
	}

	server, err := site.New(database, filepath.Clean(*root))
	if err != nil {
		log.Fatal(err)
	}
	server.ConfigurePublishing(themeDefinition.TemplatesRoot, *data, *frontend, *assets, themeDefinition.AssetsRoot)
	server.ConfigureThemeCatalog(filepath.Join(*assets, "theme"), *data, themeDefinition, *templates)
	if embeddedFrontend != nil {
		server.ConfigureEmbeddedFrontend(embeddedFrontend)
	}
	log.Printf("Go site listening on http://%s", *address)
	log.Fatal(http.ListenAndServe(*address, server.Handler()))
}

type runtimePathDefaults struct {
	root, database, templates, data, frontend, assets string
}

func runtimeDefaults() runtimePathDefaults {
	workingDirectory, err := os.Getwd()
	if err == nil && isDirectory(filepath.Join(workingDirectory, "templates")) &&
		isDirectory(filepath.Join(workingDirectory, "..", "assets")) {
		return runtimePathDefaults{
			root:      filepath.Join("..", "web"),
			database:  filepath.Join("..", "data", "site.db"),
			templates: "templates",
			data:      filepath.Join("..", "data"),
			frontend:  filepath.Join("..", "frontend", "dist"),
			assets:    filepath.Join("..", "assets"),
		}
	}

	installRoot := workingDirectory
	if executable, err := os.Executable(); err == nil {
		executableDirectory := filepath.Dir(executable)
		if filepath.Base(executableDirectory) == "bin" {
			installRoot = filepath.Dir(executableDirectory)
		} else {
			installRoot = executableDirectory
		}
	}
	return runtimePathDefaults{
		root:      filepath.Join(installRoot, "web"),
		database:  filepath.Join(installRoot, "data", "site.db"),
		templates: filepath.Join(installRoot, "templates"),
		data:      filepath.Join(installRoot, "data"),
		frontend:  "",
		assets:    filepath.Join(installRoot, "assets"),
	}
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
