package generator

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
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

	"bilvie/internal/routing"
)

type Row map[string]string

func (r Row) n(k string) int { v, _ := strconv.Atoi(r[k]); return v }

type Report struct {
	State    string    `json:"state"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
	Files    int       `json:"files"`
	Products int       `json:"products"`
	News     int       `json:"news"`
	Error    string    `json:"error,omitempty"`
}
type Publisher struct {
	DB                   *sql.DB
	Web, Templates, Data string
	Assets               string
	Theme                string
}
type content struct {
	tables    map[string][]Row
	labels    map[string]string
	templates map[string]*template.Template
	pages     map[string][]byte
}

var oldTag = regexp.MustCompile(`#[A-Za-z_][A-Za-z_0-9]*(?:\([^#]*?\))?#`)

const defaultProductImage = "/images/index_NewsPic.jpg"

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
	c := &content{tables: map[string][]Row{}, labels: map[string]string{}, templates: map[string]*template.Template{}, pages: map[string][]byte{}}
	tx, e := p.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return report, e
	}
	defer tx.Rollback()
	names := []string{"benming_ch_config", "benming_ch_cuslabel", "benming_ch_MetaType", "benming_ch_prod", "benming_ch_ProdCat", "benming_ch_news", "benming_ch_NewsCat", "benming_ch_Cocat", "benming_ch_Contact", "benming_ch_job"}
	for _, n := range names {
		rs, e := readTable(ctx, tx, n)
		if e != nil {
			return report, e
		}
		c.tables[strings.ToLower(n)] = rs
	}
	if e = tx.Commit(); e != nil {
		return report, e
	}
	for _, r := range c.tables["benming_ch_cuslabel"] {
		c.labels[strings.ToLower(strings.Trim(r["lname"], "#"))] = r["lcontent"]
	}
	for _, n := range []string{"benming_ch_prod", "benming_ch_prodcat", "benming_ch_newscat", "benming_ch_cocat", "benming_ch_job"} {
		sort.SliceStable(c.tables[n], func(i, j int) bool {
			a, b := c.tables[n][i], c.tables[n][j]
			if a.n("orderid") == b.n("orderid") {
				return a.n("id") < b.n("id")
			}
			return a.n("orderid") < b.n("orderid")
		})
	}
	sort.Slice(c.tables["benming_ch_news"], func(i, j int) bool {
		return c.tables["benming_ch_news"][i].n("newsid") > c.tables["benming_ch_news"][j].n("newsid")
	})
	entries, e := os.ReadDir(p.Templates)
	if e != nil {
		return report, e
	}
	for _, f := range entries {
		if filepath.Ext(f.Name()) != ".html" {
			continue
		}
		b, e := os.ReadFile(filepath.Join(p.Templates, f.Name()))
		if e != nil {
			return report, e
		}
		if !utf8.Valid(b) {
			return report, fmt.Errorf("模板非 UTF-8: %s", f.Name())
		}
		t, e := template.New(f.Name()).Funcs(template.FuncMap{"tag": func(k string, r Row) (string, error) { return c.tag(k, r, 0) }}).Parse(string(b))
		if e != nil {
			return report, e
		}
		c.templates[f.Name()] = t
	}
	if e = c.build(); e != nil {
		return report, e
	}
	for _, r := range c.tables["benming_ch_prod"] {
		if r.n("show") == 1 {
			report.Products++
		}
	}
	report.News = len(c.tables["benming_ch_news"])
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
func (c *content) page(path, tpl string, r Row) error {
	t, ok := c.templates[tpl+".html"]
	if !ok {
		return fmt.Errorf("缺少模板: %s", tpl)
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
func (c *content) cat(id int, news bool) Row {
	table := "benming_ch_prodcat"
	if news {
		table = "benming_ch_newscat"
	}
	for _, r := range c.tables[table] {
		if r.n("id") == id {
			return r
		}
	}
	return Row{}
}
func (c *content) under(id, root int, news bool) bool {
	seen := map[int]bool{}
	for id > 0 && !seen[id] {
		if id == root {
			return true
		}
		seen[id] = true
		id = c.cat(id, news).n("root")
	}
	return false
}
func (c *content) newsURL(r Row) string {
	dir := "news"
	if c.under(r.n("typeid"), 12, true) {
		dir = "service"
	}
	return fmt.Sprintf("/%s/detail/%s.html", dir, r["newsid"])
}

func (c *content) categoryListDir(r Row) string {
	dir := strings.TrimSpace(r["listpath"])
	if dir == "" {
		dir = routing.DefaultListPath(int64(r.n("root")))
	}
	if normalized, err := routing.NormalizeDirectory(dir); err == nil {
		return normalized
	}
	return routing.DefaultListPath(int64(r.n("root")))
}

func (c *content) categoryListPattern(r Row) string {
	pattern := strings.TrimSpace(r["listfilepattern"])
	if pattern == "" {
		pattern = routing.DefaultListPattern
	}
	if normalized, err := routing.NormalizeFilePattern(pattern, false); err == nil {
		return normalized
	}
	return routing.DefaultListPattern
}

func (c *content) categoryDetailDir(r Row) string {
	dir := strings.TrimSpace(r["detailpath"])
	if dir == "" {
		dir = routing.DefaultDetailPath
	}
	if normalized, err := routing.NormalizeDirectory(dir); err == nil {
		return normalized
	}
	return routing.DefaultDetailPath
}

func (c *content) categoryDetailPattern(r Row) string {
	pattern := strings.TrimSpace(r["detailfilepattern"])
	if pattern == "" {
		pattern = routing.DefaultDetailPattern
	}
	if normalized, err := routing.NormalizeFilePattern(pattern, false); err == nil {
		return normalized
	}
	return routing.DefaultDetailPattern
}

func (c *content) categoryListURL(r Row, page int) string {
	filename, err := routing.RenderListFilename(c.categoryListPattern(r), int64(r.n("id")), page)
	if err != nil {
		filename = fmt.Sprintf("%d.html", r.n("id"))
	}
	return "/" + strings.Trim(c.categoryListDir(r)+"/"+filename, "/")
}

func (c *content) categoryListPagePath(r Row, page int) string {
	filename, err := routing.RenderListPageFilename(c.categoryListPattern(r), int64(r.n("id")), page)
	if err != nil {
		filename = fmt.Sprintf("%d-%d.html", r.n("id"), page)
	}
	return strings.Trim(c.categoryListDir(r)+"/"+filename, "/")
}

func (c *content) productRootURL() string {
	for _, category := range c.tables["benming_ch_prodcat"] {
		if category.n("root") == 0 {
			return "/" + c.categoryListDir(category) + "/"
		}
	}
	return "/" + routing.DefaultRootListPath + "/"
}

func (c *content) prodURL(r Row) string {
	cat := c.cat(r.n("catid"), false)
	filename, err := routing.RenderDetailFilename(c.categoryDetailPattern(cat), int64(r.n("id")))
	if err != nil {
		filename = r["id"] + ".html"
	}
	return "/" + strings.Trim(c.categoryDetailDir(cat)+"/"+filename, "/")
}

func (c *content) cats(root int, news bool, plain bool) string {
	var b strings.Builder
	table := "benming_ch_prodcat"
	if news {
		table = "benming_ch_newscat"
	}
	for _, r := range c.tables[table] {
		if r.n("root") != root {
			continue
		}
		a := ""
		if news {
			dir := "news"
			if root == 12 {
				dir = "service"
			}
			a = link("/"+dir+"/"+r["id"]+".html", r["catname"])
		} else {
			a = link(c.categoryListURL(r, 1), r["catname"])
		}
		if plain {
			b.WriteString(a + " | ")
		} else {
			b.WriteString("<li>" + a + "</li>")
		}
	}
	return b.String()
}
func (c *content) productList(rs []Row) string {
	var b strings.Builder
	b.WriteString(`<div class="page-products"><ul class="clearfix">`)
	for _, r := range rs {
		image := r["smallpic"]
		if image == "" {
			image = defaultProductImage
		}
		b.WriteString(`<li><a href="` + c.prodURL(r) + `"><img loading="lazy" src="` + esc(image) + `" alt="` + esc(r["prodname"]) + `" width="140" height="120"><span>` + esc(r["prodname"]) + `</span></a></li>`)
	}
	b.WriteString("</ul></div>")
	return b.String()
}
func (c *content) newsList(rs []Row) string {
	var b strings.Builder
	for _, r := range rs {
		b.WriteString(`<div class="news_bottom_line news_sp"><p>` + link(c.newsURL(r), r["title"]) + ` <time>` + esc(r["dateandtime"]) + `</time></p><p>` + esc(r["desc"]) + `</p></div>`)
	}
	return b.String()
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
	switch k {
	case "hope_productscat()":
		return c.cats(0, false, false), nil
	case "hope_productscat2()":
		return c.cats(0, false, true), nil
	case "hope_newscat(4,1)":
		return c.cats(4, true, false), nil
	case "hope_newscat(12,2)":
		return c.cats(12, true, false), nil
	case "prodindex()", "prodindex1()":
		rs := []Row{}
		for _, p := range c.tables["benming_ch_prod"] {
			if p.n("show") == 1 && p.n("tjhome") == 1 {
				rs = append(rs, p)
			}
		}
		limit := 32
		if k == "prodindex()" {
			limit = 8
			sort.Slice(rs, func(i, j int) bool { return rs[i].n("id") > rs[j].n("id") })
		}
		rs = rs[:min(limit, len(rs))]
		if k == "prodindex1()" {
			var b strings.Builder
			for _, p := range rs {
				b.WriteString("<li>" + link(c.prodURL(p), p["prodname"]) + "</li>")
			}
			return b.String(), nil
		}
		s := c.productList(rs)
		return strings.TrimSuffix(strings.TrimPrefix(s, `<div class="page-products"><ul class="clearfix">`), "</ul></div>"), nil
	case "newsindex()", "serviceindex()", "serviceindex2()":
		var b strings.Builder
		n := 0
		for _, v := range c.tables["benming_ch_news"] {
			service := c.under(v.n("typeid"), 12, true)
			if service != (k != "newsindex()") {
				continue
			}
			b.WriteString("<li>" + link(c.newsURL(v), v["title"]) + "</li>")
			n++
			if n == 8 {
				break
			}
		}
		return b.String(), nil
	case "hope_random":
		return "", nil
	case "hope_aboutcat(32)":
		var b strings.Builder
		for _, v := range c.tables["benming_ch_cocat"] {
			if v.n("root") == 32 {
				b.WriteString("<li>" + link("/about/About-"+v["id"]+".html", v["coname"]) + "</li>")
			}
		}
		return b.String(), nil
	case "hope_contact()":
		var b strings.Builder
		for _, v := range c.tables["benming_ch_contact"] {
			b.WriteString("<p>" + esc(v["offname"]) + " " + esc(v["address"]) + " " + esc(v["phone"]) + "</p>")
		}
		return b.String(), nil
	case "msgindex()":
		var b strings.Builder
		for _, v := range c.tables["benming_ch_prod"] {
			if v.n("show") == 1 {
				b.WriteString("<tr><td>" + link(c.prodURL(v), v["prodname"]) + "</td></tr>")
				if b.Len() > 2000 {
					break
				}
			}
		}
		return b.String(), nil
	case "hope_title":
		return "", nil
	}
	return "", fmt.Errorf("未实现的模板标签: %s", k)
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

func fixedPagination(dir, id string, page, pages, total int) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<nav aria-label="分页">共 %d 条，第 %d / %d 页 `, total, page, pages)
	for i := 1; i <= pages; i++ {
		filename := id + ".html"
		if i > 1 {
			filename = fmt.Sprintf("%s-%d.html", id, i)
		}
		b.WriteString(link("/"+dir+"/"+filename, strconv.Itoa(i)) + " ")
	}
	b.WriteString("</nav>")
	return b.String()
}

func (c *content) build() error {
	if e := c.page("index.html", "index", Row{"hope_productrooturl": c.productRootURL()}); e != nil {
		return e
	}
	products := []Row{}
	for _, r := range c.tables["benming_ch_prod"] {
		if r.n("show") != 1 {
			continue
		}
		products = append(products, r)
		v := Row{"hope_title": esc(r["prodname"]), "hope_prodcode": esc(r["prodcode"]), "hope_img": esc(r["bigpic"]), "hope_body": r["itemize"], "hope_prodkeywords": esc(r["key"]), "hope_proddescription": esc(r["remark"])}
		if v["hope_img"] == "" || strings.Contains(strings.ToLower(r["bigpic"]), "dfpic.gif") {
			v["hope_img"] = esc(r["smallpic"])
		}
		if v["hope_img"] == "" {
			v["hope_img"] = defaultProductImage
		}
		v["hope_productcaturl"] = c.categoryListURL(c.cat(r.n("catid"), false), 1)
		if e := c.page(strings.TrimPrefix(c.prodURL(r), "/"), "produts_detail", v); e != nil {
			return e
		}
	}
	for _, cat := range c.tables["benming_ch_prodcat"] {
		root := cat.n("root")
		tpl := "produts_sort2"
		if root == 0 {
			tpl = "produts_sort"
		}
		rs := []Row{}
		for _, p := range products {
			if c.under(p.n("catid"), cat.n("id"), false) {
				rs = append(rs, p)
			}
		}
		pages := max(1, (len(rs)+13)/14)
		for page := 1; page <= pages; page++ {
			start := min((page-1)*14, len(rs))
			end := min(start+14, len(rs))
			v := Row{"hope_title": esc(cat["catname"]), "hope_catname": esc(cat["catname"]), "hope_smallname": esc(cat["catname"]), "hope_bigid": esc(cat["root"]), "hope_bigname": esc(c.cat(root, false)["catname"]), "hope_bigurl": c.categoryListURL(c.cat(root, false), 1), "hope_productrooturl": c.productRootURL(), "hope_productssmallcat": c.cats(root, false, true), "hope_prodkeywords": esc(cat["key"]), "hope_body": c.productList(rs[start:end]) + c.pagination(cat, page, pages, len(rs))}
			if root == 0 {
				v["hope_productssmallcat"] = c.cats(cat.n("id"), false, true)
			}
			pagePath := c.categoryListPagePath(cat, page)
			if e := c.page(pagePath, tpl, v); e != nil {
				return e
			}
			if page == 1 {
				alias := strings.TrimPrefix(c.categoryListURL(cat, 1), "/")
				c.pages[alias] = c.pages[pagePath]
				dir := c.categoryListDir(cat)
				if _, ok := c.pages[dir+"/index.html"]; !ok {
					c.pages[dir+"/index.html"] = c.pages[pagePath]
				}
			}
		}
	}
	for _, dir := range []string{routing.DefaultChildListPath, routing.DefaultRootListPath} {
		if _, ok := c.pages[dir+"/index.html"]; !ok {
			c.pages[dir+"/index.html"] = []byte(`<!doctype html><meta charset="utf-8"><p>暂无产品</p>`)
		}
	}
	for _, r := range c.tables["benming_ch_news"] {
		dir, tpl := "news", "news_news"
		if c.under(r.n("typeid"), 12, true) {
			dir, tpl = "service", "service_service"
		}
		v := Row{"hope_title": esc(r["title"]), "hope_body": r["content"], "hope_newskeywords": esc(r["key"]), "hope_newsdescription": esc(r["desc"]), "hope_typeid": esc(r["typeid"]), "hope_catname": esc(c.cat(r.n("typeid"), true)["catname"]), "hope_previous": "没有了", "hope_next": "没有了"}
		same := []Row{}
		for _, n := range c.tables["benming_ch_news"] {
			if n["typeid"] == r["typeid"] {
				same = append(same, n)
			}
		}
		for i, n := range same {
			if n["newsid"] == r["newsid"] {
				if i > 0 {
					v["hope_previous"] = link(c.newsURL(same[i-1]), same[i-1]["title"])
				}
				if i+1 < len(same) {
					v["hope_next"] = link(c.newsURL(same[i+1]), same[i+1]["title"])
				}
			}
		}
		if e := c.page(dir+"/detail/"+r["newsid"]+".html", tpl, v); e != nil {
			return e
		}
	}
	for _, cat := range c.tables["benming_ch_newscat"] {
		if cat.n("root") == 0 {
			continue
		}
		dir, tpl := "news", "news_sort"
		if c.under(cat.n("id"), 12, true) {
			dir, tpl = "service", "service_sort"
		}
		rs := []Row{}
		for _, n := range c.tables["benming_ch_news"] {
			if c.under(n.n("typeid"), cat.n("id"), true) {
				rs = append(rs, n)
			}
		}
		pages := max(1, (len(rs)+5)/6)
		for page := 1; page <= pages; page++ {
			start := min((page-1)*6, len(rs))
			end := min(start+6, len(rs))
			v := Row{"hope_title": esc(cat["catname"]), "hope_catid": esc(cat["id"]), "hope_body": c.newsList(rs[start:end]) + fixedPagination(dir, cat["id"], page, pages, len(rs))}
			path := fmt.Sprintf("%s/%s-%d.html", dir, cat["id"], page)
			if e := c.page(path, tpl, v); e != nil {
				return e
			}
			if page == 1 {
				c.pages[dir+"/"+cat["id"]+".html"] = c.pages[path]
				if _, ok := c.pages[dir+"/index.html"]; !ok {
					c.pages[dir+"/index.html"] = c.pages[path]
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
		if e := c.page(p, "corporation", v); e != nil {
			return e
		}
		if _, ok := c.pages["about/index.html"]; !ok {
			c.pages["about/index.html"] = c.pages[p]
		}
	}
	if e := c.page("contact.html", "contact", Row{}); e != nil {
		return e
	}
	if e := c.page("msg.html", "msg", Row{}); e != nil {
		return e
	}
	var jobs strings.Builder
	for _, r := range c.tables["benming_ch_job"] {
		if r.n("state") != 1 {
			continue
		}
		p := "job/detail/" + r["id"] + ".html"
		v := Row{"hope_title": esc(r["jobname"]), "hope_address": esc(r["address"]), "hope_date": esc(r["date"]), "hope_jobneed": r["jobneed"], "hope_jobnob": esc(r["jobnob"]), "hope_linkren": esc(r["linkren"]), "hope_phone": esc(r["phone"])}
		if e := c.page(p, "job_detail", v); e != nil {
			return e
		}
		jobs.WriteString("<tr><td>" + link("/"+p, r["jobname"]) + "</td></tr>")
	}
	if e := c.page("job/index.html", "job_sort", Row{"hope_body": jobs.String()}); e != nil {
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
