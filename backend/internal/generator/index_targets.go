package generator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gocms/internal/consent"
	"gocms/internal/db"
)

var (
	ErrIndexTargetNotFound = errors.New("发布目标不存在或未启用")
	ErrIndexNotPublished   = errors.New("请先执行全站发布，再生成索引")
	ErrIndexConfiguration  = errors.New("发布目标配置不完整")
	ErrIndexFormat         = errors.New("不支持的索引格式")
)

type IndexLanguage struct {
	Code string `json:"code"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type IndexFile struct {
	Format    string       `json:"format"`
	Path      string       `json:"path"`
	URL       string       `json:"url"`
	Exists    bool         `json:"exists"`
	UpdatedAt *time.Time   `json:"updated_at"`
	Aliases   []IndexAlias `json:"aliases,omitempty"`
}

type IndexAlias struct {
	Path   string `json:"path"`
	URL    string `json:"url"`
	Exists bool   `json:"exists"`
}

// IndexTarget is one public origin and output directory. Subdirectory languages
// share the main target; standalone languages each own a separate target.
// Filesystem paths remain private to the publisher.
type IndexTarget struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Mode      string          `json:"mode"`
	URL       string          `json:"url"`
	Languages []IndexLanguage `json:"languages"`
	Files     []IndexFile     `json:"files"`
	Published bool            `json:"published"`
	Issue     string          `json:"issue,omitempty"`
	web       string
}

func standaloneIndexURL(domain string) (string, error) {
	domain = strings.TrimRight(strings.TrimSpace(domain), "/")
	if domain == "" {
		return "", fmt.Errorf("%w：请为独立语言站点配置域名", ErrIndexConfiguration)
	}
	if !strings.Contains(domain, "://") {
		domain = "https://" + domain
	}
	u, err := url.Parse(domain)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return "", fmt.Errorf("%w：独立语言站点需要有效的 HTTP(S) 域名", ErrIndexConfiguration)
	}
	return domain, nil
}

func validIndexBase(base string) bool {
	u, err := url.Parse(base)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

func (p Publisher) mainIndexWeb() string {
	web := p.TargetWeb()
	if p.SiteID <= 1 {
		if _, err := os.Stat(web); os.IsNotExist(err) && p.Web != "" {
			if info, rootErr := os.Stat(filepath.Join(p.Web, "index.html")); rootErr == nil && info.Mode().IsRegular() {
				web = p.Web
			}
		}
	}
	return web
}

func (p Publisher) indexTargets(ctx context.Context) ([]IndexTarget, error) {
	base, err := configuredSiteURL(ctx, p.DB, p.SiteID)
	if err != nil {
		return nil, err
	}
	targets := []IndexTarget{{ID: "main", Name: "主站", Mode: "subdir", URL: base, Languages: []IndexLanguage{}, web: p.mainIndexWeb()}}
	if base != "" && !validIndexBase(base) {
		targets[0].URL = ""
		targets[0].Issue = "请在站点配置中设置有效的 HTTP(S) 网站地址"
	}
	if p.DB == nil {
		return targets, nil
	}
	siteID := p.SiteID
	if siteID <= 0 {
		siteID = 1
	}
	rows, err := p.DB.QueryContext(ctx, `SELECT id, code, name, is_default, path_prefix, site_mode, domain, output_dir
		FROM "`+db.LanguageTable+`" WHERE site_id = ? AND is_enabled = 1
		ORDER BY is_default DESC, sort_order ASC, id ASC`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var code, name, prefix, mode, domain, output string
		var isDefault int
		if err := rows.Scan(&id, &code, &name, &isDefault, &prefix, &mode, &domain, &output); err != nil {
			return nil, err
		}
		language := IndexLanguage{Code: code, Name: name, Path: "/"}
		if isDefault != 1 && strings.EqualFold(strings.TrimSpace(mode), db.LanguageSiteModeStandalone) {
			base, domainErr := standaloneIndexURL(domain)
			target := IndexTarget{ID: "language:" + strconv.FormatInt(id, 10), Name: name, Mode: "standalone", URL: base, Languages: []IndexLanguage{language}, web: p.LanguageTargetWeb(LanguageInfo{Code: code, SiteMode: db.LanguageSiteModeStandalone, OutputDir: output})}
			if domainErr != nil {
				target.Issue = domainErr.Error()
			}
			if err := db.ValidateOutputDir(output); err != nil {
				target.Issue = err.Error()
			}
			targets = append(targets, target)
		} else {
			if isDefault != 1 {
				prefix = strings.Trim(strings.TrimSpace(prefix), "/")
				if prefix == "" {
					prefix = strings.ToLower(code)
				}
				language.Path = "/" + prefix + "/"
			}
			targets[0].Languages = append(targets[0].Languages, language)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range targets {
		for j := 0; j < i; j++ {
			left, leftErr := filepath.Abs(targets[i].web)
			right, rightErr := filepath.Abs(targets[j].web)
			if leftErr == nil && rightErr == nil && left == right {
				targets[i].Issue = "输出目录与其他发布目标相同，请调整输出目录"
				targets[j].Issue = targets[i].Issue
			}
		}
	}
	return targets, nil
}

// IndexTargets reports actual files, including indexes created by full publishing.
func (p Publisher) IndexTargets(ctx context.Context) ([]IndexTarget, error) {
	targets, err := p.indexTargets(ctx)
	if err != nil {
		return nil, err
	}
	for i := range targets {
		target := &targets[i]
		if info, err := os.Stat(filepath.Join(target.web, "index.html")); err == nil && info.Mode().IsRegular() {
			target.Published = true
		}
		target.Files = make([]IndexFile, 0, 3)
		for _, format := range []string{SitemapHTML, SitemapXML, "llms"} {
			name, _ := sitemapFilename(format)
			if format == SitemapXML {
				name = "sitemap.xml"
			}
			file := IndexFile{Format: format, Path: "/" + name, URL: indexPublicURL(target.URL, name)}
			if info, err := os.Stat(filepath.Join(target.web, name)); err == nil && info.Mode().IsRegular() {
				modified := info.ModTime()
				file.Exists, file.UpdatedAt = true, &modified
			}
			if format == SitemapXML {
				alias := IndexAlias{Path: "/Sitemap.xml", URL: indexPublicURL(target.URL, "Sitemap.xml")}
				if info, err := os.Stat(filepath.Join(target.web, "Sitemap.xml")); err == nil && info.Mode().IsRegular() {
					alias.Exists = true
				}
				file.Aliases = []IndexAlias{alias}
			}
			target.Files = append(target.Files, file)
		}
	}
	return targets, nil
}

func indexPublicURL(base, filename string) string {
	return strings.TrimRight(base, "/") + "/" + filename
}

// GenerateIndexes updates a group, one file, or all groups from published pages.
// Render every selected file before writing, so validation/render failures leave
// all existing indexes intact. All groups share the site's publication lock.
func (p Publisher) GenerateIndexes(ctx context.Context, targetID, format string) ([]IndexTarget, error) {
	if err := p.generateIndexFiles(ctx, targetID, format, true); err != nil {
		return nil, err
	}
	return p.IndexTargets(ctx)
}

func (p Publisher) generateIndexFiles(ctx context.Context, targetID, format string, requirePublished bool) error {
	formats := []string{strings.ToLower(strings.TrimSpace(format))}
	if formats[0] == "all" {
		formats = []string{SitemapHTML, SitemapXML, "llms"}
	}
	for _, format := range formats {
		if _, ok := sitemapFilename(format); !ok {
			return fmt.Errorf("%w: %s", ErrIndexFormat, format)
		}
	}
	if err := os.MkdirAll(p.Data, 0755); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(p.Data, p.lockPath()), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	unlock, err := acquirePublishLock(lock)
	if err != nil {
		return ErrPublishBusy
	}
	defer unlock()
	targets, err := p.indexTargets(ctx)
	if err != nil {
		return err
	}
	privacy := consent.Config{}
	if p.DB != nil {
		settings, err := db.LoadSiteSettingsForSite(ctx, p.DB, p.SiteID)
		if err != nil {
			return err
		}
		privacy, err = consent.Parse(settings[consent.SettingKey])
		if err != nil {
			return err
		}
	}
	type pendingIndex struct {
		path string
		body []byte
	}
	var pending []pendingIndex
	matched := false
	for _, target := range targets {
		if targetID != "all" && target.ID != targetID {
			continue
		}
		matched = true
		if target.Issue != "" {
			return fmt.Errorf("%w：%s（%s）", ErrIndexConfiguration, target.Name, target.Issue)
		}
		web, err := filepath.Abs(target.web)
		if err != nil {
			return err
		}
		if web == filepath.Dir(web) {
			return fmt.Errorf("invalid web root")
		}
		// A configured standalone directory can sit below the main directory.
		// Its pages still belong exclusively to that standalone target.
		var excluded []string
		for _, other := range targets {
			if other.ID != target.ID {
				if directory, err := filepath.Abs(other.web); err == nil {
					excluded = append(excluded, directory)
				}
			}
		}
		urls, err := publishedURLsExcluding(ctx, web, excluded)
		if err != nil {
			return err
		}
		if requirePublished && len(urls) == 0 {
			return fmt.Errorf("%w：%s", ErrIndexNotPublished, target.Name)
		}
		for _, format := range formats {
			var body []byte
			if format == SitemapHTML {
				body = renderSitemapHTML(urls)
				language := ""
				if len(target.Languages) > 0 {
					language = target.Languages[0].Code
				}
				body, err = consent.Process(body, privacy.Resolve(p.SiteID, language))
			} else if format == "llms" {
				body, err = renderLLMS(ctx, target.URL, urls, func(path string) (io.ReadCloser, error) { return os.Open(filepath.Join(web, filepath.FromSlash(path))) })
			} else {
				body = renderSitemapXML(target.URL, urls)
			}
			if err != nil {
				return err
			}
			filename, _ := sitemapFilename(format)
			pending = append(pending, pendingIndex{filepath.Join(web, filename), body})
			if format == SitemapXML {
				pending = append(pending, pendingIndex{filepath.Join(web, "sitemap.xml"), body})
			}
		}
	}
	if !matched {
		return ErrIndexTargetNotFound
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, file := range pending {
		if err := atomicWrite(file.path, file.body); err != nil {
			return err
		}
	}
	return nil
}
