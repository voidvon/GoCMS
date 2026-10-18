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
