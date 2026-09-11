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

