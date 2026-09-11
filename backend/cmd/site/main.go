package main

import (
	"context"
	"errors"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

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
	frontendDev := flag.String("frontend-dev", defaults.frontendDev, "Vite dev server URL to proxy /admin/ (e.g. http://127.0.0.1:5173); empty disables dev proxy")
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
	if err := db.CreateSchema(context.Background(), database); err != nil {
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
	server.ConfigureThemeCatalog(filepath.Join(*assets, "theme"), *data, themeDefinition)
	if embeddedFrontend != nil {
		server.ConfigureEmbeddedFrontend(embeddedFrontend)
	}
	if *frontendDev != "" {
		if err := server.ConfigureFrontendDev(*frontendDev); err != nil {
			log.Fatalf("invalid frontend-dev URL %q: %v", *frontendDev, err)
		}
	}
	httpServer := &http.Server{
		Addr:    *address,
		Handler: server.Handler(),
	}

	shutdownCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-shutdownCtx.Done()
		timeoutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(timeoutCtx)
	}()

	log.Printf("Go site listening on http://%s", *address)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

type runtimePathDefaults struct {
	root, database, templates, data, frontend, frontendDev, assets string
}

func runtimeDefaults() runtimePathDefaults {
	workingDirectory, err := os.Getwd()
	if err == nil && isDirectory(filepath.Join(workingDirectory, "templates")) &&
		isDirectory(filepath.Join(workingDirectory, "..", "assets")) {
		return runtimePathDefaults{
			root:        filepath.Join("..", "web"),
			database:    filepath.Join("..", "data", "site.db"),
			templates:   "templates",
			data:        filepath.Join("..", "data"),
			frontend:    filepath.Join("..", "frontend", "dist"),
			frontendDev: "http://127.0.0.1:5173",
			assets:      filepath.Join("..", "assets"),
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
		root:        filepath.Join(installRoot, "web"),
		database:    filepath.Join(installRoot, "data", "site.db"),
		templates:   filepath.Join(installRoot, "templates"),
		data:        filepath.Join(installRoot, "data"),
		frontend:    "",
		frontendDev: "",
		assets:      filepath.Join(installRoot, "assets"),
	}
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
