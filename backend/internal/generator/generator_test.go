package generator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"gocms/internal/db"
	"gocms/internal/templateconfig"
)

func TestPublishLifecycle(t *testing.T) {
	d, e := db.Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	ctx := context.Background()
	if e = db.CreateSchema(ctx, d); e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{
		`INSERT INTO benming_ch_config (id,WebUrl) VALUES (1,'http://www.example.com/')`,
		`INSERT INTO benming_ch_cuslabel (id,lname,lcontent) VALUES (1,'#BM_indextop#',''),(2,'#BM_indexfoot#',''),(3,'#BM_about#',''),(4,'#BM_botten#',''),(5,'#BM_top#','')`,
		`INSERT INTO gocms_category (id,name,parent_id,order_id,route_id,list_path,list_file_pattern,list_template,detail_path,detail_file_pattern,detail_template) VALUES
					(2,'总类',0,0,2,'catalog','{id}.html','category_list.html','entry','{id}.html','content_detail.html'),
					(10,'子类',2,0,10,'catalog/items','{id}.html','category_list.html','entry','{id}.html','content_detail.html'),
					(1004,'栏目',0,0,4,'articles','{id}.html','category_list.html','articles/detail','{id}.html','content_detail.html'),
					(1006,'子栏目',1004,0,6,'articles','{id}.html','category_list.html','articles/detail','{id}.html','content_detail.html')`,
		`INSERT INTO gocms_content (id,category_id,route_key,title,body,cover_image,visible,featured) VALUES
					(1,10,'1','阀门 & 新内容','<p>正文内容</p><img src="/UploadFile/produppic/cover.jpg">','/UploadFile/produppic/cover.jpg',1,1),
					(9,1006,'9','测试内容','<p>详情内容</p><img src="https://img05.jdzj.com/oledit/UploadFile/news2015a/external.jpg">','',1,0)`,
	} {
		if _, e = d.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	tmp := t.TempDir()
	web := filepath.Join(tmp, "web")
	if e = os.MkdirAll(web, 0755); e != nil {
		t.Fatal(e)
	}
	assets := filepath.Join(tmp, "assets")
	if e = os.MkdirAll(filepath.Join(assets, "images"), 0755); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(assets, "images", "photo.jpg"), []byte("asset"), 0644); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(assets, "images", "cover.jpg"), []byte("cover"), 0644); e != nil {
		t.Fatal(e)
	}
	templates := filepath.Join(tmp, "templates")
	if e = os.CopyFS(templates, os.DirFS("../../templates")); e != nil {
		t.Fatal(e)
	}
	p := Publisher{DB: d, Web: web, Templates: templates, Data: filepath.Join(tmp, "data"), Assets: assets}
	report, e := p.Generate(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if report.Contents != 2 {
		t.Fatalf("bad report: %+v", report)
	}
	for _, n := range []string{"index.html", "entry/1.html", "catalog/items/10.html", "catalog/items/10-1.html", "catalog/2.html", "articles/detail/9.html", "articles/4.html", "articles/6.html", "Sitemap.xml"} {
		if _, e = os.Stat(filepath.Join(web, n)); e != nil {
			t.Fatal(n, e)
		}
	}
	body, _ := os.ReadFile(filepath.Join(web, "entry/1.html"))
	if !strings.Contains(string(body), "阀门 &amp; 新内容") || !strings.Contains(string(body), "<p>正文内容</p>") || !strings.Contains(string(body), `src="/images/cover.jpg"`) {
		t.Fatal("escaping or body rendering failed")
	}
	homeBody, _ := os.ReadFile(filepath.Join(web, "index.html"))
	if !strings.Contains(string(homeBody), `position:fixed`) || !strings.Contains(string(homeBody), `src="/images/wx.jpg"`) {
		t.Fatal("homepage floating QR code was not rendered")
	}
	contentBody, _ := os.ReadFile(filepath.Join(web, "articles/detail/9.html"))
	if !strings.Contains(string(contentBody), `src="https://img05.jdzj.com/oledit/UploadFile/news2015a/external.jpg"`) {
		t.Fatal("third-party image URL was rewritten")
	}
	if e = os.WriteFile(filepath.Join(templates, "custom-content-detail.html"), []byte(`<html><body>custom detail {{tag "title" .}}</body></html>`), 0644); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(templates, "custom-content-list.html"), []byte(`<html><body>custom list {{tag "title" .}}{{range listItems .}}<a href="{{.URL}}">{{.Title}}</a>{{end}}</body></html>`), 0644); e != nil {
		t.Fatal(e)
	}
	if _, e = d.Exec(`UPDATE gocms_category SET list_template='custom-content-list.html', detail_template='custom-content-detail.html' WHERE id=10`); e != nil {
		t.Fatal(e)
	}
	if _, e = p.Generate(ctx); e != nil {
		t.Fatal(e)
	}
	customTemplateBody, _ := os.ReadFile(filepath.Join(web, "entry/1.html"))
	if !strings.Contains(string(customTemplateBody), "custom detail 阀门 &amp; 新内容") {
		t.Fatal("category detail template assignment was not used")
	}
	customListBody, _ := os.ReadFile(filepath.Join(web, "catalog/items/10.html"))
	if !strings.Contains(string(customListBody), "custom list 子类") {
		t.Fatal("category list template assignment was not used")
	}
	if _, e = d.Exec(`UPDATE gocms_category SET list_path='custom-catalog', list_file_pattern='{id}.htm', detail_path='custom-entry', detail_file_pattern='{id}-detail.html' WHERE id=10`); e != nil {
		t.Fatal(e)
	}
	if _, e = p.Generate(ctx); e != nil {
		t.Fatal(e)
	}
	for _, n := range []string{"custom-catalog/10.htm", "custom-catalog/10-1.htm", "custom-entry/1-detail.html"} {
		if _, e = os.Stat(filepath.Join(web, n)); e != nil {
			t.Fatal(n, e)
		}
	}
	customBody, _ := os.ReadFile(filepath.Join(web, "custom-catalog/10.htm"))
	if !strings.Contains(string(customBody), `href="/custom-entry/1-detail.html"`) {
		t.Fatal("custom detail route was not used in content list")
	}
	if _, e = os.Stat(filepath.Join(web, "entry/1.html")); !os.IsNotExist(e) {
		t.Fatal("old content route survived custom publication")
	}
	if _, e = d.Exec(`UPDATE gocms_content SET visible=0 WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	if _, e = p.Generate(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(web, "entry/1.html")); !os.IsNotExist(e) {
		t.Fatal("hidden content HTML survived")
	}
	if b, e := os.ReadFile(filepath.Join(assets, "images", "photo.jpg")); e != nil || string(b) != "asset" {
		t.Fatal("asset changed")
	}
	if _, e = os.Stat(filepath.Join(web, "photo.jpg")); !os.IsNotExist(e) {
		t.Fatal("resource copied into web")
	}
	if b, err := os.ReadFile(filepath.Join(assets, "images", "cover.jpg")); err != nil || string(b) != "cover" {
		t.Fatal("image changed")
	}
	before, _ := os.ReadFile(filepath.Join(web, "index.html"))
	if e = os.WriteFile(filepath.Join(templates, "index.html"), []byte(`{{tag "unsupported" .}}`), 0644); e != nil {
		t.Fatal(e)
	}
	if _, e = p.Generate(ctx); e == nil {
		t.Fatal("invalid template accepted")
	}
	after, _ := os.ReadFile(filepath.Join(web, "index.html"))
	if string(before) != string(after) {
		t.Fatal("failed publication changed live page")
	}
}

func TestContentURLUsesConfiguredDetailPath(t *testing.T) {
	c := &content{tables: map[string][]Row{
		"gocms_category": {{
			"id": "10", "detail_path": "entry", "detail_file_pattern": "item-{id}.html",
		}},
	}}
	if got := c.contentURL(Row{"category_id": "10", "route_key": "185"}); got != "/entry/item-185.html" {
		t.Fatalf("content detail URL = %q", got)
	}
}

func TestCoverCategoryUsesConfiguredTemplateAndRoute(t *testing.T) {
	c := &content{
		tables: map[string][]Row{
			"gocms_category": {
				{"id": "500", "name": "关于我们", "parent_id": "0", "order_id": "0", "route_id": "500", "page_type": "cover", "list_path": "about", "list_file_pattern": "index.html", "cover_template": "cover.html", "list_template": "category_list.html", "detail_path": "content", "detail_file_pattern": "{id}.html", "detail_template": "content_detail.html"},
				{"id": "501", "name": "联系我们", "parent_id": "500", "order_id": "0", "route_id": "501", "page_type": "cover", "list_path": "", "list_file_pattern": "contact.html", "cover_template": "cover.html", "list_template": "category_list.html", "detail_path": "content", "detail_file_pattern": "{id}.html", "detail_template": "content_detail.html"},
			},
			"gocms_content":       {},
			"benming_ch_config":   {},
			"benming_ch_cuslabel": {},
			"benming_ch_metatype": {},
			"benming_ch_cocat":    {},
			"benming_ch_job":      {},
		},
		assignments: map[string]string{
			templateconfig.RoleHomeIndex: "index.html",
			templateconfig.RoleMessage:   "msg.html",
			templateconfig.RoleJobList:   "job.html",
		},
		templates: map[string]*template.Template{},
		pages:     map[string][]byte{},
	}
	for name, source := range map[string]string{
		"index.html": "home",
		"msg.html":   "message",
		"job.html":   "jobs",
		"cover.html": `{{tag "category_name" .}}:{{range listChildren .}}{{.Name}}={{.URL}}{{end}}`,
	} {
		parsed, err := template.New(name).Funcs(c.templateFuncs()).Parse(source)
		if err != nil {
			t.Fatal(err)
		}
		c.templates[name] = parsed
	}
	if err := c.build(); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.pages["contact.html"]; !ok {
		t.Fatal("cover category did not generate fixed root page")
	}
	if _, ok := c.pages["contact-1.html"]; ok {
		t.Fatal("cover category generated a pagination page")
	}
	if got := string(c.pages["contact.html"]); got != "联系我们:" {
		t.Fatalf("cover template data = %q", got)
	}
	if got := string(c.pages["about/index.html"]); got != "关于我们:联系我们=/contact.html" {
		t.Fatalf("cover child data = %q", got)
	}
	if got := c.categoryListURL(c.cat(500), 1); got != "/about/" {
		t.Fatalf("cover index URL = %q", got)
	}
}

func TestHomepageTagsUseUnifiedContent(t *testing.T) {
	c := &content{tables: map[string][]Row{
		"gocms_category": {
			{"id": "1", "route_id": "1", "source_table": "benming_ch_ProdCat", "source_id": "1", "detail_path": "Product", "detail_file_pattern": "{id}.html"},
			{"id": "575", "source_table": "benming_ch_NewsCat", "source_id": "4", "detail_path": "news/detail", "detail_file_pattern": "{id}.html"},
			{"id": "576", "parent_id": "575", "source_table": "benming_ch_NewsCat", "source_id": "6", "detail_path": "news/detail", "detail_file_pattern": "{id}.html"},
			{"id": "577", "source_table": "benming_ch_NewsCat", "source_id": "12", "detail_path": "service/detail", "detail_file_pattern": "{id}.html"},
			{"id": "579", "parent_id": "577", "source_table": "benming_ch_NewsCat", "source_id": "14", "detail_path": "service/detail", "detail_file_pattern": "{id}.html"},
		},
		"gocms_content": {
			{"id": "100", "category_id": "1", "route_key": "100", "title": "产品一", "cover_image": "/images/one.jpg", "visible": "1", "featured": "1", "sort_order": "1"},
			{"id": "101", "category_id": "1", "route_key": "101", "title": "产品二", "cover_image": "/images/two.jpg", "visible": "1", "featured": "1", "sort_order": "2"},
			{"id": "200", "category_id": "576", "route_key": "200", "title": "新闻一", "visible": "1", "sort_order": "1"},
			{"id": "300", "category_id": "579", "route_key": "300", "title": "技术一", "visible": "1", "sort_order": "1"},
		},
	}}

	rolling, err := c.tag("prodindex()", Row{}, 0)
	if err != nil || !strings.Contains(rolling, `href="/Product/101.html"`) || !strings.Contains(rolling, `src="/images/two.jpg"`) {
		t.Fatalf("product carousel tag = %q, err=%v", rolling, err)
	}
	products, err := c.tag("prodindex1()", Row{}, 0)
	if err != nil || !strings.Contains(products, `href="/Product/100.html"`) || !strings.Contains(products, "/Product/101.html") {
		t.Fatalf("product list tag = %q, err=%v", products, err)
	}
	news, err := c.tag("newsindex()", Row{}, 0)
	if err != nil || !strings.Contains(news, `href="/news/detail/200.html"`) || strings.Contains(news, "/service/detail/") {
		t.Fatalf("news tag = %q, err=%v", news, err)
	}
	service, err := c.tag("serviceindex()", Row{}, 0)
	if err != nil || !strings.Contains(service, `href="/service/detail/300.html"`) || strings.Contains(service, "/news/detail/") {
		t.Fatalf("service tag = %q, err=%v", service, err)
	}
}

func TestListTemplatesReceiveStructuredData(t *testing.T) {
	c := &content{tables: map[string][]Row{
		"gocms_category": {
			{"id": "575", "name": "新闻", "source_table": "benming_ch_NewsCat", "source_id": "4", "list_path": "news", "list_template": "service_category_list.html", "detail_path": "news/detail", "detail_file_pattern": "{id}.html"},
			{"id": "576", "name": "行业新闻", "parent_id": "575", "source_table": "benming_ch_NewsCat", "source_id": "6", "list_path": "news", "list_template": "service_category_list.html", "detail_path": "news/detail", "detail_file_pattern": "{id}.html"},
			{"id": "577", "name": "技术文章", "source_table": "benming_ch_NewsCat", "source_id": "12", "list_path": "service", "list_template": "service_category_list.html", "detail_path": "service/detail", "detail_file_pattern": "{id}.html"},
			{"id": "578", "name": "阀门技术", "parent_id": "577", "source_table": "benming_ch_NewsCat", "source_id": "13", "list_path": "service", "list_template": "service_category_list.html", "detail_path": "service/detail", "detail_file_pattern": "{id}.html"},
			{"id": "579", "name": "阀门知识", "parent_id": "577", "source_table": "benming_ch_NewsCat", "source_id": "14", "list_path": "service", "list_template": "service_category_list.html", "detail_path": "service/detail", "detail_file_pattern": "{id}.html"},
		},
		"gocms_content": {
			{"id": "1", "category_id": "578", "route_key": "1", "title": "测试文章 &", "summary": "测试摘要", "published_at": "2026-09-10 12:00:00", "cover_image": "/images/article.jpg", "visible": "1", "sort_order": "1"},
		},
	}}

	for _, category := range []Row{c.cat(575), c.cat(578), c.cat(579)} {
		if got := c.categoryListTemplate(category); got != "service_category_list.html" {
			t.Fatalf("article category %q uses list template %q", category["id"], got)
		}
	}
	configuredDefault := Row{
		"source_table":  "benming_ch_NewsCat",
		"list_path":     "service",
		"detail_path":   "service/detail",
		"list_template": templateconfig.DefaultListTemplate,
	}
	if got := c.categoryListTemplate(configuredDefault); got != templateconfig.DefaultListTemplate {
		t.Fatalf("configured list template was overridden: %q", got)
	}
	custom := c.cat(578)
	custom["list_template"] = "custom-list.html"
	if got := c.categoryListTemplate(custom); got != "custom-list.html" {
		t.Fatalf("custom list template was ignored: %q", got)
	}

	view := Row{"category_id": "578", "list_page": "1", "list_page_size": "6"}
	items := c.listItems(view)
	if len(items) != 1 || items[0].Title != "测试文章 &amp;" || items[0].Date != "2026-09-10" || items[0].URL != "/service/detail/1.html" {
		t.Fatalf("structured list item = %+v", items)
	}

	listTemplate, err := template.New("list").Funcs(c.templateFuncs()).Parse(`{{range listItems .}}<li>{{.Title}}|{{.Date}}|{{.Excerpt}}</li>{{end}}{{with listPagination .}}<span>{{.Total}}/{{.Pages}}</span>{{end}}`)
	if err != nil {
		t.Fatal(err)
	}
	var rendered strings.Builder
	if err := listTemplate.Execute(&rendered, view); err != nil {
		t.Fatal(err)
	}
	if got := rendered.String(); got != `<li>测试文章 &amp;|2026-09-10|测试摘要</li><span>1/1</span>` {
		t.Fatalf("theme list rendering = %q", got)
	}
}

func TestRootCategoriesIncludeAllCollections(t *testing.T) {
	c := &content{tables: map[string][]Row{
		"gocms_category": {
			{"id": "1", "name": "新闻", "order_id": "1", "detail_path": "news/detail", "list_path": "news"},
			{"id": "2", "name": "进口阀门", "order_id": "0", "source_table": "benming_ch_ProdCat", "source_id": "25", "list_path": "valve"},
			{"id": "3", "name": "技术文章", "order_id": "2", "source_table": "benming_ch_NewsCat", "source_id": "12", "detail_path": "service/detail", "list_path": "service"},
			{"id": "4", "name": "闸阀", "order_id": "1", "source_table": "benming_ch_ProdCat", "source_id": "26", "list_path": "valve"},
		},
	}}

	got := c.cats(0, false)
	productStart := strings.Index(got, ">进口阀门</a>")
	productEnd := strings.Index(got, ">闸阀</a>")
	news := strings.Index(got, ">新闻</a>")
	service := strings.Index(got, ">技术文章</a>")
	if productStart < 0 || productEnd < 0 || productStart > productEnd || news < 0 || service < 0 {
		t.Fatalf("root category order = %q", got)
	}
}
