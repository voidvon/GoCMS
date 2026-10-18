package site

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// searchResponse 对应 /api/search 的返回结构。
type searchResponse struct {
	Total int `json:"total"`
	Items []struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
	} `json:"items"`
}

func searchRequest(t *testing.T, server *Server, target string) searchResponse {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", target, response.Code, response.Body.String())
	}
	var result searchResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

// TestSearchMatchesRequestedLanguageTranslations 验证关键词搜索会命中译文，
// 但只命中请求语言的译文：英文站搜不到只存在于中文或西班牙语译文里的内容。
func TestSearchMatchesRequestedLanguageTranslations(t *testing.T) {
	server, database, _ := newCategoryTestServer(t)
	ctx := context.Background()

	// 基准语言（zh-CN）内容 + 英文译文。
	if _, err := database.ExecContext(ctx, `
		INSERT INTO "gocms_content" ("id", "site_id", "category_id", "route_key", "title", "summary", "body", "visible")
		VALUES (1, 1, 0, 'valve-a', '蒸汽阀门', '用于蒸汽系统的阀门', '正文内容', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO "gocms_content_translation" ("content_id", "lang", "title", "summary", "body")
		VALUES (1, 'en', 'Steam control valve', 'Regulates steam flow', 'English body')`); err != nil {
		t.Fatal(err)
	}
	// 基准语言内容 + 西班牙语译文，用于验证语言隔离。
	if _, err := database.ExecContext(ctx, `
		INSERT INTO "gocms_content" ("id", "site_id", "category_id", "route_key", "title", "summary", "body", "visible")
		VALUES (2, 1, 0, 'valve-b', '调节阀', '调节阀说明', '正文', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO "gocms_content_translation" ("content_id", "lang", "title", "summary", "body")
		VALUES (2, 'es', 'Válvula de control', 'Resumen español', 'Cuerpo')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO "gocms_content" ("id", "site_id", "category_id", "route_key", "title", "summary", "body", "visible")
		VALUES (3, 1, 0, 'hidden', '隐藏阀门', '隐藏摘要', '隐藏正文', 0)`); err != nil {
		t.Fatal(err)
	}

	// 英文译文只在 lang=en 时命中，且返回英文标题。
	if en := searchRequest(t, server, "/api/search?q=control&lang=en"); en.Total != 1 || len(en.Items) != 1 || en.Items[0].ID != 1 {
		t.Fatalf("lang=en search = %+v, want content 1", en)
	} else if en.Items[0].Title != "Steam control valve" {
		t.Fatalf("lang=en title = %q", en.Items[0].Title)
	}
	// 西语译文只在 lang=es 时命中，英文站搜不到。
	if es := searchRequest(t, server, "/api/search?q=control&lang=es"); es.Total != 1 || es.Items[0].ID != 2 {
		t.Fatalf("lang=es search = %+v, want content 2", es)
	}
	if isolated := searchRequest(t, server, "/api/search?q=V%C3%A1lvula&lang=en"); isolated.Total != 0 {
		t.Fatalf("english site matched a spanish translation: %+v", isolated)
	}
	// 默认语言（zh-CN）也不应命中其他语言的译文。
	if base := searchRequest(t, server, "/api/search?q=control"); base.Total != 0 {
		t.Fatalf("default language matched an english translation: %+v", base)
	}

	// 基准语言关键词、译文摘要命中，以及按请求语言返回标题。
	if zh := searchRequest(t, server, "/api/search?q=%E8%92%B8%E6%B1%BD%E9%98%80%E9%97%A8"); zh.Total != 1 || zh.Items[0].ID != 1 {
		t.Fatalf("base language search = %+v", zh)
	}
	if summary := searchRequest(t, server, "/api/search?q=Regulates&lang=en"); summary.Total != 1 || summary.Items[0].ID != 1 {
		t.Fatalf("translation summary search = %+v", summary)
	}
	if localized := searchRequest(t, server, "/api/search?q=%E8%92%B8%E6%B1%BD%E9%98%80%E9%97%A8&lang=en"); len(localized.Items) != 1 || localized.Items[0].Title != "Steam control valve" {
		t.Fatalf("lang=en result = %+v", localized)
	}

	// 未命中、不可见内容与空关键词。
	if miss := searchRequest(t, server, "/api/search?q=zzz-no-such-term"); miss.Total != 0 || len(miss.Items) != 0 {
		t.Fatalf("unexpected match = %+v", miss)
	}
	if hidden := searchRequest(t, server, "/api/search?q=%E9%9A%90%E8%97%8F"); hidden.Total != 0 {
		t.Fatalf("hidden content matched = %+v", hidden)
	}
	if all := searchRequest(t, server, "/api/search?q="); all.Total != 2 {
		t.Fatalf("empty query total = %d, want 2", all.Total)
	}
}

// TestSearchRanksByRelevance 验证搜索结果按相关性排序：
// 标题完全匹配 > 标题前缀 > 标题包含 > 编码 > 摘要 > 正文，
// 而不是按 sort_order（下面刻意把 sort_order 排成相反顺序）。
func TestSearchRanksByRelevance(t *testing.T) {
	server, database, _ := newCategoryTestServer(t)
	ctx := context.Background()

	seed := []struct {
		id        int64
		routeKey  string
		title     string
		code      string
		summary   string
		body      string
		sortOrder int
	}{
		{1, "body-only", "Thermal efficiency services", "", "steam services", "long body text ... bsa mention", 1},
		{2, "summary-only", "Sugar refining", "", "bsa steam solutions for sugar refining", "body", 2},
		{3, "code-only", "Packaged heat exchanger", "BSA-100", "summary", "body", 3},
		{4, "title-contains", "Spirax Sarco BSA3T Bellows Sealed Stop Valve", "", "summary", "body", 4},
		{5, "title-prefix", "BSA1T Bellows Sealed Stop Valve", "", "summary", "body", 5},
		{6, "title-exact", "bsa", "", "summary", "body", 9},
	}
	for _, row := range seed {
		if _, err := database.ExecContext(ctx, `
			INSERT INTO "gocms_content" ("id", "site_id", "category_id", "route_key", "title", "code", "summary", "body", "visible", "sort_order")
			VALUES (?, 1, 0, ?, ?, ?, ?, ?, 1, ?)`,
			row.id, row.routeKey, row.title, row.code, row.summary, row.body, row.sortOrder); err != nil {
			t.Fatal(err)
		}
	}

	result := searchRequest(t, server, "/api/search?q=bsa&page_size=20")
	got := make([]int64, 0, len(result.Items))
	for _, item := range result.Items {
		got = append(got, item.ID)
	}
	want := []int64{6, 5, 4, 3, 2, 1}
	if len(got) != len(want) {
		t.Fatalf("search order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("search order = %v, want %v", got, want)
		}
	}
}
