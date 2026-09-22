package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"gocms/internal/db"
	"gocms/internal/generator"
	"gocms/internal/theme"
)

func main() {
	database := flag.String("db", "../data/site.db", "SQLite database")
	web := flag.String("web", "../web", "published web root")
	templates := flag.String("templates", "templates", "template directory")
	data := flag.String("data", "../data", "publication state directory")
	assets := flag.String("assets", "../assets", "public resource directory")
	themeOverride := flag.String("theme", "", "theme resource directory override")
	siteFlag := flag.Int64("site", 0, "site ID to generate (0 for all active sites)")
	flag.Parse()

	d, e := db.Open(*database)
	if e != nil {
		log.Fatal(e)
	}
	defer d.Close()

	ctx := context.Background()
	if e = db.CreateSchema(ctx, d); e != nil {
		log.Fatal(e)
	}

	themeDir := filepath.Join(*assets, "1", "themes")
	if _, err := os.Stat(themeDir); os.IsNotExist(err) {
		themeDir = filepath.Join(*assets, "theme")
	}
	defaultTheme, e := theme.Resolve(themeDir, *data, *themeOverride, *templates)
	if e != nil {
		log.Fatal(e)
	}

	var targets []db.Site
	if *siteFlag > 0 {
		target, err := db.GetSiteByID(ctx, d, *siteFlag)
		if err != nil {
			log.Fatalf("site %d not found: %v", *siteFlag, err)
		}
		targets = append(targets, *target)
	} else {
		allSites, err := db.ListSites(ctx, d)
		if err != nil || len(allSites) == 0 {
			targets = append(targets, db.Site{ID: 1, Name: "默认站点"})
		} else {
			for _, s := range allSites {
				if s.Status == "active" {
					targets = append(targets, s)
				}
			}
		}
	}

	reports := make(map[int64]generator.Report)
	for _, target := range targets {
		tplRoot := defaultTheme.TemplatesRoot
		assetsRoot := defaultTheme.AssetsRoot
		homeTpl := defaultTheme.HomeTemplate()

		themeID := target.ThemeID
		if themeID == "" {
			themeID = defaultTheme.Manifest.ID
		}
		if themeID != "" {
			siteThemeDir := filepath.Join(*assets, fmt.Sprintf("%d", target.ID), "themes", themeID)
			if _, err := os.Stat(siteThemeDir); os.IsNotExist(err) {
				siteThemeDir = filepath.Join(*assets, "1", "themes", themeID)
			}
			if _, err := os.Stat(siteThemeDir); os.IsNotExist(err) {
				siteThemeDir = filepath.Join(*assets, "theme", themeID)
			}
			tTpl := filepath.Join(siteThemeDir, "templates")
			tAssets := filepath.Join(siteThemeDir, "assets")
			if stat, err := os.Stat(tTpl); err == nil && stat.IsDir() {
				tplRoot = tTpl
			}
			if stat, err := os.Stat(tAssets); err == nil && stat.IsDir() {
				assetsRoot = tAssets
			}
			if manifestBytes, err := os.ReadFile(filepath.Join(siteThemeDir, "theme.json")); err == nil {
				var m theme.Manifest
				if json.Unmarshal(manifestBytes, &m) == nil {
					homeTpl = theme.Definition{Manifest: m}.HomeTemplate()
				}
			}
		}

		pub := generator.Publisher{
			DB:           d,
			Web:          *web,
			Templates:    tplRoot,
			Data:         *data,
			Assets:       *assets,
			Theme:        assetsRoot,
			HomeTemplate: homeTpl,
			SiteID:       target.ID,
			OutputDir:    target.OutputDir,
		}

		r, err := pub.Generate(ctx)
		if err != nil {
			log.Printf("generate site %d (%s) failed: %v", target.ID, target.Name, err)
		} else {
			log.Printf("generated site %d (%s) to %s (%d files, %d contents)", target.ID, target.Name, pub.TargetWeb(), r.Files, r.Contents)
		}
		reports[target.ID] = r
	}

	if len(reports) == 1 {
		for _, r := range reports {
			_ = json.NewEncoder(os.Stdout).Encode(r)
		}
	} else {
		_ = json.NewEncoder(os.Stdout).Encode(reports)
	}
}
