package templatelabel_test

import (
	"context"
	"testing"

	"gocms/internal/db"
	"gocms/internal/templatelabel"
)

func TestTemplateLabelManagement(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	if err := templatelabel.Ensure(ctx, database); err != nil {
		t.Fatal(err)
	}

	category, err := templatelabel.CreateCategory(ctx, database, "内容卡片", 2)
	if err != nil {
		t.Fatal(err)
	}
	item, err := templatelabel.Create(ctx, database, templatelabel.Input{
		Key: "Article-Card", Name: "文章卡片", CategoryID: category.ID, Context: templatelabel.ContextList,
		Description: "列表中的文章卡片",
		Temptext:    "<section>[!--list.temp--]<!--list.var1-->[!--list.temp--]</section>",
		Listvar:     "<article>{{.Title}}</article>",
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.Key != "article-card" || item.CategoryName != "内容卡片" || item.Context != templatelabel.ContextList {
		t.Fatalf("unexpected template label: %+v", item)
	}

	page, err := templatelabel.List(ctx, database, "article", category.ID, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || !testingContains(page.Items[0].Content, `<article>{{.Title}}</article>`) {
		t.Fatalf("unexpected template label page: %+v", page)
	}

	item, err = templatelabel.Update(ctx, database, item.ID, templatelabel.Input{
		Key: "article-card", Name: "文章卡片新版", CategoryID: category.ID, Context: templatelabel.ContextDetail,
		Temptext: "<section class=\"v2\">[!--list.temp--]<!--list.var1-->[!--list.temp--]</section>",
		Listvar:  "<article>{{.title}}</article>",
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.Name != "文章卡片新版" || item.Context != templatelabel.ContextDetail {
		t.Fatalf("updated template label = %+v", item)
	}

	if err := templatelabel.DeleteCategory(ctx, database, category.ID); err != nil {
		t.Fatal(err)
	}
	item, err = templatelabel.Get(ctx, database, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.CategoryID != 0 || item.CategoryName != "" {
		t.Fatalf("template label was not uncategorized: %+v", item)
	}
	if err := templatelabel.Delete(ctx, database, item.ID); err != nil {
		t.Fatal(err)
	}
}

func TestTemplateLabelValidation(t *testing.T) {
	if err := templatelabel.ValidateContent("valid", `{{range listItems .}}{{label "card" .}}{{end}}`); err != nil {
		t.Fatalf("valid template was rejected: %v", err)
	}
	if err := templatelabel.ValidateContent("content-items", `{{range contentItems 0 10 true false "newest"}}{{.Index}} {{.Title}}{{end}}`); err != nil {
		t.Fatalf("contentItems template was rejected: %v", err)
	}
	if err := templatelabel.ValidateContent("invalid", `{{unknown .}}`); err == nil {
		t.Fatal("unknown template function was accepted")
	}
	if _, err := templatelabel.NormalizeInput(templatelabel.Input{Key: "1-card", Name: "卡片", Temptext: "x", Listvar: "y"}); err == nil {
		t.Fatal("invalid template label key was accepted")
	}
}

func TestTwoBlockTemplateLabel(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	if err := templatelabel.Ensure(ctx, database); err != nil {
		t.Fatal(err)
	}

	temptext := `<ul class="news-list">
[!--list.temp--]
  <!--list.var1-->
[!--list.temp--]
</ul>`
	listvar := `<li><a href="[!--url--]">[!--title--]</a><span>[!--date--]</span></li>`

	item, err := templatelabel.Create(ctx, database, templatelabel.Input{
		Key:      "news-list",
		Name:     "新闻两块列表模板",
		Temptext: temptext,
		Listvar:  listvar,
		Rownum:   1,
		Subnews:  100,
		Showdate: "Y-m-d",
	})
	if err != nil {
		t.Fatalf("create template label failed: %v", err)
	}
	if item.Key != "news-list" || item.Temptext != temptext || item.Listvar != listvar || item.Rownum != 1 || item.Showdate != "Y-m-d" {
		t.Fatalf("unexpected created label: %+v", item)
	}
	if !testingContains(item.Content, `{{.Title}}`) || !testingContains(item.Content, `{{.URL}}`) || !testingContains(item.Content, `{{.Date}}`) {
		t.Fatalf("compiled content does not contain converted placeholders: %s", item.Content)
	}
}

func TestTemplateLabelMultiSiteIsolation(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	if err := templatelabel.Ensure(ctx, database); err != nil {
		t.Fatal(err)
	}

	// Create label "header" for Site 1
	item1, err := templatelabel.CreateForSite(ctx, database, 1, templatelabel.Input{
		Key:      "header",
		Name:     "Site 1 Header",
		Temptext: "<header>Site 1 Header</header>",
		Listvar:  "<span>var</span>",
	})
	if err != nil {
		t.Fatalf("create label for site 1 failed: %v", err)
	}

	// Create label "header" for Site 2 with SAME key
	item2, err := templatelabel.CreateForSite(ctx, database, 2, templatelabel.Input{
		Key:      "header",
		Name:     "Site 2 Header",
		Temptext: "<header>Site 2 Header</header>",
		Listvar:  "<span>var</span>",
	})
	if err != nil {
		t.Fatalf("create label for site 2 with same key failed: %v", err)
	}

	if item1.ID == item2.ID {
		t.Fatalf("item1 and item2 must have distinct IDs")
	}

	// Load for Site 1
	loaded1, err := templatelabel.LoadForSite(ctx, database, 1)
	if err != nil || len(loaded1) != 1 || loaded1[0].Content != "<header>Site 1 Header</header>" {
		t.Fatalf("expected site 1 header content, got: %+v, err: %v", loaded1, err)
	}

	// Load for Site 2
	loaded2, err := templatelabel.LoadForSite(ctx, database, 2)
	if err != nil || len(loaded2) != 1 || loaded2[0].Content != "<header>Site 2 Header</header>" {
		t.Fatalf("expected site 2 header content, got: %+v, err: %v", loaded2, err)
	}

	// Count for Site 1 and Site 2
	c1, err := templatelabel.CountForSite(ctx, database, 1)
	if err != nil || c1 != 1 {
		t.Fatalf("expected site 1 count 1, got %d, err: %v", c1, err)
	}
	c2, err := templatelabel.CountForSite(ctx, database, 2)
	if err != nil || c2 != 1 {
		t.Fatalf("expected site 2 count 1, got %d, err: %v", c2, err)
	}
}

func testingContains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(substr) > 0 && len(s) > 0 && indexOfString(s, substr) >= 0))
}

func indexOfString(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
