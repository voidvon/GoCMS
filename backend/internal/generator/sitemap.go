package generator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	format = strings.ToLower(strings.TrimSpace(format))
	filename, ok := sitemapFilename(format)
	if !ok {
		return "", fmt.Errorf("不支持的网站地图格式: %s", format)
	}
	if err := os.MkdirAll(p.Data, 0755); err != nil {
		return "", err
	}
	lock, err := os.OpenFile(filepath.Join(p.Data, "publish.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	unlock, err := acquirePublishLock(lock)
	if err != nil {
		return "", ErrPublishBusy
	}
	defer unlock()

	web, err := filepath.Abs(p.Web)
	if err != nil {
		return "", err
	}
	if web == filepath.Dir(web) {
		return "", fmt.Errorf("invalid web root")
	}
	urls, err := publishedURLs(ctx, web)
	if err != nil {
		return "", err
	}
	base, err := configuredSiteURL(ctx, p.DB)
	if err != nil {
		return "", err
	}

	var body []byte
	if format == SitemapHTML {
		body = renderSitemapHTML(urls)
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
	case SitemapHTML:
		return "sitemap.html", true
	case SitemapXML:
		return "Sitemap.xml", true
	default:
		return "", false
	}
}

func publishedURLs(ctx context.Context, web string) ([]string, error) {
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

func configuredSiteURL(ctx context.Context, database *sql.DB) (string, error) {
	if database == nil {
		return "", nil
	}
	settings, err := db.LoadSiteSettings(ctx, database)
	if err != nil {
		return "", fmt.Errorf("读取网站地址失败: %w", err)
	}
	return strings.TrimRight(strings.TrimSpace(settings["site_url"]), "/"), nil
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
