package generator

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"text/template"
	"time"
	"unicode/utf8"

	"gocms/internal/routing"
	"gocms/internal/templateconfig"
)

type Row map[string]string

func (r Row) n(k string) int { v, _ := strconv.Atoi(r[k]); return v }

type Report struct {
	State    string    `json:"state"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
	Files    int       `json:"files"`
	Contents int       `json:"contents"`
	Error    string    `json:"error,omitempty"`
}
type Publisher struct {
	DB                   *sql.DB
	Web, Templates, Data string
	Assets               string
	Theme                string
}
type content struct {
	tables      map[string][]Row
	labels      map[string]string
	templates   map[string]*template.Template
	assignments map[string]string
	pages       map[string][]byte
	publicHost  string
}

var oldTag = regexp.MustCompile(`#[A-Za-z_][A-Za-z_0-9]*(?:\([^#]*?\))?#`)

const defaultContentImage = "/images/content-placeholder.jpg"

func esc(s string) string           { return html.EscapeString(s) }
func link(url, title string) string { return `<a href="` + esc(url) + `">` + esc(title) + `</a>` }
func readTable(ctx context.Context, tx *sql.Tx, name string) ([]Row, error) {
	rows, e := tx.QueryContext(ctx, `SELECT * FROM "`+name+`"`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	cols, e := rows.Columns()
	if e != nil {
		return nil, e
	}
	out := []Row{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptr := make([]any, len(cols))
		for i := range vals {
			ptr[i] = &vals[i]
		}
		if e = rows.Scan(ptr...); e != nil {
			return nil, e
		}
		r := Row{}
		for i, k := range cols {
			v := ""
			if vals[i] != nil {
				v = fmt.Sprint(vals[i])
				if b, ok := vals[i].([]byte); ok {
					v = string(b)
				}
			}
			r[strings.ToLower(k)] = v
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (p Publisher) Generate(ctx context.Context) (report Report, err error) {
	report = Report{State: "running", Started: time.Now()}
	if err = os.MkdirAll(p.Data, 0755); err != nil {
		return
	}
	lock, e := os.OpenFile(filepath.Join(p.Data, "publish.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return report, e
	}
	defer lock.Close()
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		return report, fmt.Errorf("已有发布任务正在运行")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	defer func() {
		report.Finished = time.Now()
		report.State = "success"
		if err != nil {
			report.State = "failed"
			report.Error = err.Error()
		}
		b, _ := json.MarshalIndent(report, "", "  ")
		_ = atomicWrite(filepath.Join(p.Data, "publish.json"), b)
	}()
	c := &content{tables: map[string][]Row{}, labels: map[string]string{}, templates: map[string]*template.Template{}, assignments: map[string]string{}, pages: map[string][]byte{}}
	tx, e := p.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return report, e
	}
	defer tx.Rollback()
	names := []string{"benming_ch_config", "benming_ch_cuslabel", "benming_ch_MetaType", "gocms_content", "gocms_category", "benming_ch_Cocat", "benming_ch_Contact", "benming_ch_job"}
	for _, n := range names {
		rs, e := readTable(ctx, tx, n)
		if e != nil {
			return report, e
		}
		c.tables[strings.ToLower(n)] = rs
	}
	c.publicHost = configuredPublicHost(c.tables["benming_ch_config"])
	assignments, e := templateconfig.Load(ctx, tx)
	if e != nil {
		return report, e
	}
	c.assignments = assignments
	if e = tx.Commit(); e != nil {
		return report, e
	}
	for _, r := range c.tables["benming_ch_cuslabel"] {
		c.labels[strings.ToLower(strings.Trim(r["lname"], "#"))] = r["lcontent"]
	}
	for _, n := range []string{"gocms_content", "gocms_category", "benming_ch_cocat", "benming_ch_job"} {
		sort.SliceStable(c.tables[n], func(i, j int) bool {
			a, b := c.tables[n][i], c.tables[n][j]
			if a.n("orderid") == b.n("orderid") {
				return a.n("id") < b.n("id")
			}
			return a.n("orderid") < b.n("orderid")
		})
	}
	e = filepath.WalkDir(p.Templates, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".html") {
			return nil
		}
		relative, err := filepath.Rel(p.Templates, filePath)
		if err != nil {
			return err
		}
		templatePath := filepath.ToSlash(relative)
		b, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		if !utf8.Valid(b) {
			return fmt.Errorf("模板非 UTF-8: %s", templatePath)
		}
		t, err := template.New(templatePath).Funcs(template.FuncMap{"tag": func(k string, r Row) (string, error) { return c.tag(k, r, 0) }}).Parse(string(b))
		if err != nil {
			return err
		}
		c.templates[templatePath] = t
		return nil
	})
	if e != nil {
		return report, e
	}
	if e = c.build(); e != nil {
		return report, e
	}
	for _, r := range c.tables["gocms_content"] {
		if r.n("visible") == 1 {
			report.Contents++
		}
	}
	report.Files = len(c.pages)
	web, e := filepath.Abs(p.Web)
	if e != nil {
		return report, e
	}
	if web == filepath.Dir(web) {
		return report, fmt.Errorf("invalid web root")
	}
	stage, e := os.MkdirTemp(filepath.Dir(web), ".web-stage-")
	if e != nil {
		return report, e
	}
	defer os.RemoveAll(stage)
	// Resource trees are served independently; publishing never copies their bytes.
	if e = c.normalizeLinks(p.Assets, p.Theme); e != nil {
		return report, e
	}
	for rel, b := range c.pages {
		if e = ctx.Err(); e != nil {
			return report, e
		}
		if e = atomicWrite(filepath.Join(stage, filepath.FromSlash(rel)), b); e != nil {
			return report, e
		}
	}
	// Both renames stay on the same filesystem. Roll back if installing the staged site fails.
	backup := web + ".previous"
	if _, e = os.Stat(backup); e == nil {
		return report, fmt.Errorf("上次发布备份仍存在: %s，请先检查恢复", backup)
	}
	hadWeb := false
	if _, e = os.Stat(web); e == nil {
		if e = os.Rename(web, backup); e != nil {
			return report, e
		}
		hadWeb = true
	}
	if e = os.Rename(stage, web); e != nil {
		if hadWeb {
			_ = os.Rename(backup, web)
		}
		return report, e
	}
	if hadWeb {
		_ = os.RemoveAll(backup)
	}
	return report, nil
}
func atomicWrite(p string, b []byte) error {
	if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(p), ".publish-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	if e = f.Chmod(0644); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(f.Name(), p)
}
func (c *content) page(path, role string, r Row) error {
	templatePath, assigned := c.assignments[role]
	if !assigned {
		return fmt.Errorf("缺少模板配置: %s", role)
	}
	return c.pageTemplate(path, templatePath, r)
}

func (c *content) pageTemplate(path, templatePath string, r Row) error {
	normalized, err := templateconfig.NormalizePath(templatePath)
	if err != nil {
		return fmt.Errorf("模板路径无效: %s: %w", templatePath, err)
	}
	templatePath = normalized
	t, ok := c.templates[templatePath]
	if !ok {
		return fmt.Errorf("缺少模板: %s", templatePath)
	}
	var b strings.Builder
	if e := t.Execute(&b, r); e != nil {
		return fmt.Errorf("生成 %s: %w", path, e)
	}
	s := b.String()
	if !utf8.ValidString(s) {
		return fmt.Errorf("生成页面非 UTF-8: %s", path)
	}
	c.pages[path] = []byte(s)
	return nil
}
func (c *content) cat(id int) Row {
	for _, r := range c.tables["gocms_category"] {
		if r.n("id") == id {
			return r
		}
	}
	return Row{}
}
func (c *content) under(id, root int) bool {
	seen := map[int]bool{}
	for id > 0 && !seen[id] {
		if id == root {
			return true
		}
		seen[id] = true
		id = c.cat(id).n("parent_id")
	}
	return false
}
func (c *content) categoryListDir(r Row) string {
	dir := strings.TrimSpace(r["list_path"])
	if dir == "" {
		dir = routing.DefaultListPath(int64(r.n("parent_id")))
	}
	if normalized, err := routing.NormalizeDirectory(dir); err == nil {
		return normalized
	}
	return routing.DefaultListPath(int64(r.n("parent_id")))
}

func (c *content) categoryListPattern(r Row) string {
	pattern := strings.TrimSpace(r["list_file_pattern"])
	if pattern == "" {
		pattern = routing.DefaultListPattern
	}
	if normalized, err := routing.NormalizeFilePattern(pattern, false); err == nil {
		return normalized
	}
	return routing.DefaultListPattern
}

func (c *content) categoryDetailDir(r Row) string {
	dir := strings.TrimSpace(r["detail_path"])
	if dir == "" {
		dir = routing.DefaultDetailPath
	}
	if normalized, err := routing.NormalizeDirectory(dir); err == nil {
		return normalized
	}
	return routing.DefaultDetailPath
}

func (c *content) categoryDetailPattern(r Row) string {
	pattern := strings.TrimSpace(r["detail_file_pattern"])
	if pattern == "" {
		pattern = routing.DefaultDetailPattern
	}
	if normalized, err := routing.NormalizeFilePattern(pattern, false); err == nil {
		return normalized
	}
	return routing.DefaultDetailPattern
}

func (c *content) categoryListURL(r Row, page int) string {
	routeID := int64(r.n("route_id"))
	if routeID == 0 {
		routeID = int64(r.n("id"))
	}
	filename, err := routing.RenderListFilename(c.categoryListPattern(r), routeID, page)
	if err != nil {
		filename = fmt.Sprintf("%d.html", routeID)
	}
	return "/" + strings.Trim(c.categoryListDir(r)+"/"+filename, "/")
}

func (c *content) categoryListPagePath(r Row, page int) string {
	routeID := int64(r.n("route_id"))
	if routeID == 0 {
		routeID = int64(r.n("id"))
	}
	filename, err := routing.RenderListPageFilename(c.categoryListPattern(r), routeID, page)
	if err != nil {
		filename = fmt.Sprintf("%d-%d.html", routeID, page)
	}
	return strings.Trim(c.categoryListDir(r)+"/"+filename, "/")
}

func (c *content) rootCategoryURL() string {
	for _, category := range c.tables["gocms_category"] {
		if category.n("parent_id") == 0 {
			return "/" + c.categoryListDir(category) + "/"
		}
	}
	return "/" + routing.DefaultCategoryPath + "/"
}

func (c *content) contentURL(r Row) string {
	category := c.cat(r.n("category_id"))
	filename, err := routing.RenderDetailFilenameValue(c.categoryDetailPattern(category), r["route_key"])
	if err != nil {
		filename = r["route_key"] + ".html"
	}
	return "/" + strings.Trim(c.categoryDetailDir(category)+"/"+filename, "/")
}

func (c *content) cats(root int, plain bool) string {
	var b strings.Builder
	for _, r := range c.tables["gocms_category"] {
		if r.n("parent_id") != root {
			continue
		}
		a := link(c.categoryListURL(r, 1), r["name"])
		if plain {
			b.WriteString(a + " | ")
		} else {
			b.WriteString("<li>" + a + "</li>")
		}
	}
	return b.String()
}

func (c *content) contentList(rs []Row) string {
	var b strings.Builder
	for _, r := range rs {
		image := strings.TrimSpace(r["cover_image"])
		if image == "" {
			image = defaultContentImage
		}
		b.WriteString(`<li><a href="` + esc(c.contentURL(r)) + `"><img loading="lazy" src="` + esc(image) + `" alt="` + esc(r["title"]) + `" width="140" height="120"><span>` + esc(r["title"]) + `</span></a></li>`)
	}
	return b.String()
}

func (c *content) categoryListTemplate(category Row) string {
	if templatePath := strings.TrimSpace(category["list_template"]); templatePath != "" {
		return templatePath
	}
	return templateconfig.DefaultListTemplate
}

func (c *content) categoryDetailTemplate(category Row) string {
	if templatePath := strings.TrimSpace(category["detail_template"]); templatePath != "" {
		return templatePath
	}
	return templateconfig.DefaultDetailTemplate
}

func (c *content) contentView(r Row) Row {
	category := c.cat(r.n("category_id"))
	image := strings.TrimSpace(r["cover_image"])
	if image == "" {
		image = defaultContentImage
	}
	categoryRoute := c.categoryListURL(category, 1)
	return Row{
		"id": esc(r["id"]), "route_key": esc(r["route_key"]),
		"title": esc(r["title"]), "body": r["body"], "content": r["body"],
		"code": esc(r["code"]), "summary": esc(r["summary"]),
		"cover_image": esc(image), "image": esc(image),
		"published_at": esc(r["published_at"]), "date": esc(r["published_at"]),
		"source": esc(r["source"]), "keywords": esc(r["keywords"]),
		"description": esc(r["description"]),
		"category_id": esc(category["id"]), "category_name": esc(category["name"]),
		"category_url": categoryRoute, "content_url": c.contentURL(r),
		"root_category_url": c.rootCategoryURL(),
		"categories":        c.cats(0, false),
		"category_children": c.cats(category.n("id"), false),
	}
}

func (c *content) tag(k string, r Row, depth int) (string, error) {
	k = strings.ToLower(k)
	if depth > 12 {
		return "", fmt.Errorf("标签循环引用: %s", k)
	}
	if v, ok := r[k]; ok {
		return v, nil
	}
	if v, ok := c.labels[k]; ok {
		var err error
		s := oldTag.ReplaceAllStringFunc(v, func(t string) string {
			x, e := c.tag(t[1:len(t)-1], r, depth+1)
			if e != nil {
				err = e
			}
			return x
		})
		return s, err
	}
	for _, cfg := range c.tables["benming_ch_config"] {
		m := map[string]string{"hope_webname": "webname", "hope_weburl": "weburl", "hope_address": "coadd", "hope_tel": "cophone", "hope_fax": "cofax", "hope_email": "coemail", "hope_webqq": "webqq", "hope_webmsn": "webmsn", "hope_ren": "coren", "hope_post": "copost"}
		if col, ok := m[k]; ok {
			return esc(cfg[col]), nil
		}
		break
	}
	if strings.HasPrefix(k, "hope_meta_") {
		for _, meta := range c.tables["benming_ch_metatype"] {
			for name, col := range map[string]string{"title": "title", "keywords": "meta_keywords", "description": "meta_descriptions"} {
				if k == "hope_meta_"+name+"("+meta["id"]+")" {
					return esc(meta[col]), nil
				}
			}
		}
		return "", nil
	}
	switch {
	case k == "categories()":
		return c.cats(0, false), nil
	case k == "categories_plain()":
		return c.cats(0, true), nil
	case k == "category_children()":
		return c.cats(r.n("category_id"), false), nil
	case strings.HasPrefix(k, "featured_content"):
		limit, ok := callLimit(k, "featured_content", 8)
		if !ok {
			break
		}
		return c.featuredContent(limit), nil
	case k == "content_index()":
		return c.featuredContent(32), nil
	case k == "hope_aboutcat(32)":
		var b strings.Builder
		for _, v := range c.tables["benming_ch_cocat"] {
			if v.n("root") == 32 {
				b.WriteString("<li>" + link("/about/About-"+v["id"]+".html", v["coname"]) + "</li>")
			}
		}
		return b.String(), nil
	case k == "hope_contact()":
		var b strings.Builder
		for _, v := range c.tables["benming_ch_contact"] {
			b.WriteString("<p>" + esc(v["offname"]) + " " + esc(v["address"]) + " " + esc(v["phone"]) + "</p>")
		}
		return b.String(), nil
	}
	return "", fmt.Errorf("未实现的模板标签: %s", k)
}

func callLimit(value, name string, fallback int) (int, bool) {
	if value == name+"()" {
		return fallback, true
	}
	prefix := name + "("
	if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, ")") {
		return 0, false
	}
	limit, err := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, prefix), ")")))
	if err != nil || limit < 1 || limit > 200 {
		return 0, false
	}
	return limit, true
}

func (c *content) featuredContent(limit int) string {
	items := make([]Row, 0, len(c.tables["gocms_content"]))
	for _, item := range c.tables["gocms_content"] {
		if item.n("visible") == 1 && item.n("featured") == 1 {
			items = append(items, item)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].n("sort_order") == items[j].n("sort_order") {
			return items[i].n("id") > items[j].n("id")
		}
		return items[i].n("sort_order") < items[j].n("sort_order")
	})
	items = items[:min(limit, len(items))]
	var b strings.Builder
	for _, item := range items {
		b.WriteString("<li>" + link(c.contentURL(item), item["title"]) + "</li>")
	}
	return b.String()
}
func (c *content) pagination(category Row, page, pages, total int) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<nav aria-label="分页">共 %d 条，第 %d / %d 页 `, total, page, pages)
	for i := 1; i <= pages; i++ {
		b.WriteString(link(c.categoryListURL(category, i), strconv.Itoa(i)) + " ")
	}
	b.WriteString("</nav>")
	return b.String()
}

func (c *content) build() error {
	if e := c.page("index.html", templateconfig.RoleHomeIndex, Row{
		"root_category_url": c.rootCategoryURL(),
		"categories":        c.cats(0, false),
	}); e != nil {
		return e
	}

	visible := make([]Row, 0, len(c.tables["gocms_content"]))
	for _, item := range c.tables["gocms_content"] {
		if item.n("visible") == 1 {
			visible = append(visible, item)
		}
	}
	sort.SliceStable(visible, func(i, j int) bool {
		if visible[i].n("sort_order") == visible[j].n("sort_order") {
			return visible[i].n("id") < visible[j].n("id")
		}
		return visible[i].n("sort_order") < visible[j].n("sort_order")
	})

	for _, item := range visible {
		category := c.cat(item.n("category_id"))
		view := c.contentView(item)
		view["previous"], view["next"] = "没有了", "没有了"
		sameCategory := make([]Row, 0)
		for _, candidate := range visible {
			if candidate.n("category_id") == item.n("category_id") {
				sameCategory = append(sameCategory, candidate)
			}
		}
		for index, candidate := range sameCategory {
			if candidate.n("id") != item.n("id") {
				continue
			}
			if index > 0 {
				view["previous"] = link(c.contentURL(sameCategory[index-1]), sameCategory[index-1]["title"])
			}
			if index+1 < len(sameCategory) {
				view["next"] = link(c.contentURL(sameCategory[index+1]), sameCategory[index+1]["title"])
			}
		}
		if e := c.pageTemplate(strings.TrimPrefix(c.contentURL(item), "/"), c.categoryDetailTemplate(category), view); e != nil {
			return e
		}
	}

	for _, category := range c.tables["gocms_category"] {
		items := make([]Row, 0)
		for _, item := range visible {
			if c.under(item.n("category_id"), category.n("id")) {
				items = append(items, item)
			}
		}
		pageSize := category.n("list_page_size")
		if pageSize < 1 {
			pageSize = 14
		}
		pages := max(1, (len(items)+pageSize-1)/pageSize)
		parent := c.cat(category.n("parent_id"))
		for page := 1; page <= pages; page++ {
			start := min((page-1)*pageSize, len(items))
			end := min(start+pageSize, len(items))
			childrenRoot := category.n("parent_id")
			if childrenRoot == 0 {
				childrenRoot = category.n("id")
			}
			view := Row{
				"title":             esc(category["name"]),
				"category_name":     esc(category["name"]),
				"category_id":       esc(category["id"]),
				"parent_name":       esc(parent["name"]),
				"parent_url":        c.categoryListURL(parent, 1),
				"root_category_url": c.rootCategoryURL(),
				"category_children": c.cats(childrenRoot, true),
				"categories":        c.cats(0, false),
				"keywords":          esc(category["keywords"]),
				"description":       esc(category["description"]),
				"body":              c.contentList(items[start:end]) + c.pagination(category, page, pages, len(items)),
				"content_list":      c.contentList(items[start:end]),
				"content_count":     strconv.Itoa(len(items)),
				"content_page_size": strconv.Itoa(pageSize),
			}
			pagePath := c.categoryListPagePath(category, page)
			if e := c.pageTemplate(pagePath, c.categoryListTemplate(category), view); e != nil {
				return e
			}
			if page == 1 {
				alias := strings.TrimPrefix(c.categoryListURL(category, 1), "/")
				c.pages[alias] = c.pages[pagePath]
				dir := c.categoryListDir(category)
				if _, ok := c.pages[dir+"/index.html"]; !ok {
					c.pages[dir+"/index.html"] = c.pages[pagePath]
				}
			}
		}
	}
	for _, r := range c.tables["benming_ch_cocat"] {
		if r.n("root") != 32 {
			continue
		}
		v := Row{"hope_title": esc(r["coname"]), "hope_co_centern": r["centern"]}
		p := "about/About-" + r["id"] + ".html"
		if e := c.page(p, templateconfig.RoleAboutDetail, v); e != nil {
			return e
		}
		if _, ok := c.pages["about/index.html"]; !ok {
			c.pages["about/index.html"] = c.pages[p]
		}
	}
	if e := c.page("contact.html", templateconfig.RoleContact, Row{}); e != nil {
		return e
	}
	if e := c.page("msg.html", templateconfig.RoleMessage, Row{}); e != nil {
		return e
	}
	var jobs strings.Builder
	for _, r := range c.tables["benming_ch_job"] {
		if r.n("state") != 1 {
			continue
		}
		p := "job/detail/" + r["id"] + ".html"
		v := Row{"hope_title": esc(r["jobname"]), "hope_address": esc(r["address"]), "hope_date": esc(r["date"]), "hope_jobneed": r["jobneed"], "hope_jobnob": esc(r["jobnob"]), "hope_linkren": esc(r["linkren"]), "hope_phone": esc(r["phone"])}
		if e := c.page(p, templateconfig.RoleJobDetail, v); e != nil {
			return e
		}
		jobs.WriteString("<tr><td>" + link("/"+p, r["jobname"]) + "</td></tr>")
	}
	if e := c.page("job/index.html", templateconfig.RoleJobList, Row{"hope_body": jobs.String()}); e != nil {
		return e
	}
	urls := []string{}
	for p := range c.pages {
		urls = append(urls, p)
	}
	sort.Strings(urls)
	var sitemap strings.Builder
	sitemap.WriteString(`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>网站地图</title><ul>`)
	for _, p := range urls {
		sitemap.WriteString("<li>" + link("/"+p, p) + "</li>")
	}
	sitemap.WriteString("</ul></html>")
	c.pages["sitemap.html"] = []byte(sitemap.String())
	var xml strings.Builder
	xml.WriteString(`<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
	base := ""
	if cfg := c.tables["benming_ch_config"]; len(cfg) > 0 {
		base = strings.TrimRight(cfg[0]["weburl"], "/")
	}
	for _, p := range urls {
		xml.WriteString("<url><loc>" + esc(base+"/"+p) + "</loc></url>")
	}
	xml.WriteString("</urlset>")
	c.pages["Sitemap.xml"] = []byte(xml.String())
	return nil
}
