package generator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gocms/internal/db"
)

const (
	SitemapHTML = "html"
	SitemapXML  = "xml"
)

var ErrPublishBusy = errors.New("已有发布任务正在运行")

// GenerateSitemap updates one sitemap from the pages that are currently live.
// A full publication still regenerates both files from the new page snapshot.
func (p Publisher) GenerateSitemap(ctx context.Context, format string) (string, error) {
	return p.generateIndex(ctx, format)
}

func (p Publisher) generateIndex(ctx context.Context, format string) (string, error) {
	format = strings.ToLower(strings.TrimSpace(format))
	filename, ok := sitemapFilename(format)
	if !ok {
		return "", fmt.Errorf("不支持的网站地图格式: %s", format)
	}
	if err := os.MkdirAll(p.Data, 0755); err != nil {
		return "", err
	}
	lock, err := os.OpenFile(filepath.Join(p.Data, p.lockPath()), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	unlock, err := acquirePublishLock(lock)
	if err != nil {
		return "", ErrPublishBusy
	}
	defer unlock()

	targetWeb := p.TargetWeb()
	if p.SiteID <= 1 {
		if _, err := os.Stat(targetWeb); os.IsNotExist(err) && p.Web != "" {
			if _, errWeb := os.Stat(p.Web); errWeb == nil {
				targetWeb = p.Web
			}
		}
	}
	if targetWeb == "" {
		targetWeb = p.Web
	}
	web, err := filepath.Abs(targetWeb)


	if err != nil {
		return "", err
	}
	if web == filepath.Dir(web) {
		return "", fmt.Errorf("invalid web root")
	}
	if err := os.MkdirAll(web, 0755); err != nil {
		return "", err
	}
	urls, err := publishedURLs(ctx, web)
	if err != nil {
		return "", err
	}
	base, err := configuredSiteURL(ctx, p.DB, p.SiteID)
	if err != nil {
		return "", err
	}

	var body []byte
	if format == SitemapHTML {
		body = renderSitemapHTML(urls)
	} else if format == "llms" {
		body, err = renderLLMS(ctx, base, urls, func(path string) (io.ReadCloser, error) { return os.Open(filepath.Join(web, filepath.FromSlash(path))) })
		if err != nil {
			return "", err
		}
	} else {
		body = renderSitemapXML(base, urls)
	}
	if err := atomicWrite(filepath.Join(web, filename), body); err != nil {
		return "", err
	}
	return filename, nil
}

func sitemapFilename(format string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "llms":
		return "llms.txt", true
	case SitemapHTML:
		return "sitemap.html", true
	case SitemapXML:
		return "Sitemap.xml", true
	default:
		return "", false
	}
}

func publishedURLs(ctx context.Context, web string) ([]string, error) {
	if _, err := os.Stat(web); os.IsNotExist(err) {
		return []string{}, nil
	}
	urls := make([]string, 0)
	err := filepath.WalkDir(web, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(web, filePath)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if strings.EqualFold(relative, "sitemap.html") || strings.EqualFold(relative, "sitemap.xml") {
			return nil
		}
		extension := strings.ToLower(filepath.Ext(relative))
		if extension != ".html" && extension != ".htm" {
			return nil
		}
		urls = append(urls, relative)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(urls)
	return urls, nil
}

func configuredSiteURL(ctx context.Context, database *sql.DB, siteID int64) (string, error) {
	if database == nil {
		return "", nil
	}
	if siteID <= 0 {
		siteID = 1
	}
	settings, err := db.LoadSiteSettingsForSite(ctx, database, siteID)
	if err != nil {
		return "", fmt.Errorf("读取网站地址失败: %w", err)
	}
	siteURL := strings.TrimRight(strings.TrimSpace(settings["site_url"]), "/")
	if siteURL != "" {
		return siteURL, nil
	}
	var domain string
	_ = database.QueryRowContext(ctx, `SELECT "domain" FROM "`+db.SiteTable+`" WHERE "id" = ?`, siteID).Scan(&domain)
	domain = db.NormalizeDomain(domain)
	if domain != "" {
		return "https://" + domain, nil
	}
	return "", nil
}

func renderSitemapHTML(urls []string) []byte {
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>网站地图</title><ul>`)
	for _, value := range urls {
		b.WriteString(`<li><a href="` + esc("/"+value) + `">` + esc(value) + `</a></li>`)
	}
	b.WriteString("</ul></html>")
	return []byte(b.String())
}

func renderSitemapXML(base string, urls []string) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
	for _, value := range urls {
		b.WriteString("<url><loc>" + esc(base+"/"+value) + "</loc></url>")
	}
	b.WriteString("</urlset>")
	return []byte(b.String())
}
