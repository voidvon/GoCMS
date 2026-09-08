package generator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bilvie/internal/db"
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
		`INSERT INTO benming_ch_config (id) VALUES (1)`,
		`INSERT INTO benming_ch_cuslabel (id,lname,lcontent) VALUES (1,'#BM_indextop#',''),(2,'#BM_indexfoot#',''),(3,'#BM_about#',''),(4,'#BM_botten#',''),(5,'#BM_top#','')`,
		`INSERT INTO benming_ch_prod (id,prodName,CatId,show,itemize,smallpic,tjhome) VALUES (1,'阀门 & 新产品',10,1,'<p>正文</p><img src="/UploadFile/produppic/product.jpg">','/UploadFile/produppic/product.jpg',1)`,
		`INSERT INTO benming_ch_ProdCat (id,CatName,Root) VALUES (2,'总类',0),(10,'子类',2)`,
		`INSERT INTO benming_ch_NewsCat (id,CatName,Root) VALUES (4,'新闻',0),(6,'公司新闻',4)`,
		`INSERT INTO benming_ch_news (newsid,Title,Content,Typeid) VALUES (9,'测试新闻','<p>新闻内容</p>',6)`,
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
	if e = os.WriteFile(filepath.Join(assets, "images", "product.jpg"), []byte("product"), 0644); e != nil {
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
	if report.Products != 1 || report.News != 1 {
		t.Fatalf("bad report: %+v", report)
	}
	for _, n := range []string{"index.html", "Product/1.html", "Products/10.html", "Products/10-1.html", "valve/2.html", "news/detail/9.html", "news/6.html", "Sitemap.xml"} {
		if _, e = os.Stat(filepath.Join(web, n)); e != nil {
			t.Fatal(n, e)
		}
	}
	body, _ := os.ReadFile(filepath.Join(web, "Product/1.html"))
	if !strings.Contains(string(body), "阀门 &amp; 新产品") || !strings.Contains(string(body), "<p>正文</p>") || !strings.Contains(string(body), `src="/images/product.jpg"`) {
		t.Fatal("escaping or body rendering failed")
	}
	if _, e = d.Exec(`UPDATE benming_ch_prod SET show=0 WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	if _, e = p.Generate(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(web, "Product/1.html")); !os.IsNotExist(e) {
		t.Fatal("hidden product HTML survived")
	}
	if b, e := os.ReadFile(filepath.Join(assets, "images", "photo.jpg")); e != nil || string(b) != "asset" {
		t.Fatal("asset changed")
	}
	if _, e = os.Stat(filepath.Join(web, "photo.jpg")); !os.IsNotExist(e) {
		t.Fatal("resource copied into web")
	}
	if b, err := os.ReadFile(filepath.Join(assets, "images", "product.jpg")); err != nil || string(b) != "product" {
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
