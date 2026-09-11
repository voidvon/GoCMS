package site

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSystemModelsAndFieldsManagement(t *testing.T) {
	server, database, sessionCookie := newCategoryTestServer(t)

	// 1. Check default table exists
	var tableCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM "gocms_model_table"`).Scan(&tableCount); err != nil || tableCount < 1 {
		t.Fatalf("expected default table, got %d, err: %v", tableCount, err)
	}

	// 2. Create a new model field
	fieldPayload, _ := json.Marshal(map[string]any{
		"table_id":      1,
		"field_name":    "price",
		"field_label":   "参考价格",
		"field_type":    "text",
		"field_options": "",
		"description":   "产品价格",
		"sort_order":    15,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/model-fields", bytes.NewReader(fieldPayload))
	req.AddCookie(&http.Cookie{Name: "gocms_admin", Value: sessionCookie})
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("create model field failed %d: %s", resp.Code, resp.Body.String())
	}

	// 3. Create a new system model
	entryFieldsJSON, _ := json.Marshal([]map[string]string{
		{"field": "title", "label": "商品名称"},
		{"field": "price", "label": "销售价格"},
		{"field": "body", "label": "详细说明"},
	})
	mustFieldsJSON, _ := json.Marshal([]string{"title", "price"})
	modelPayload, _ := json.Marshal(map[string]any{
		"name":         "商品模型",
		"table_id":     1,
		"description":  "电商或产品展示模型",
		"entry_fields": entryFieldsJSON,
		"must_fields":  mustFieldsJSON,
		"sort_order":   20,
	})
	req = httptest.NewRequest(http.MethodPost, "/api/admin/models", bytes.NewReader(modelPayload))
	req.AddCookie(&http.Cookie{Name: "gocms_admin", Value: sessionCookie})
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("create model failed %d: %s", resp.Code, resp.Body.String())
	}

	// 4. Create content with extra_data
	if _, err := database.Exec(`INSERT INTO "gocms_category" ("id", "name", "route_id", "model_id") VALUES (1, '产品栏目', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	contentPayload, _ := json.Marshal(map[string]any{
		"title":       "测试高压球阀",
		"category_id": 1,
		"model_id":    1,
		"extra_data": map[string]any{
			"price": "999.00",
			"spec":  "DN50 PN16",
		},
	})
	req = httptest.NewRequest(http.MethodPost, "/api/admin/content", bytes.NewReader(contentPayload))
	req.AddCookie(&http.Cookie{Name: "gocms_admin", Value: sessionCookie})
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("create content with extra_data failed %d: %s", resp.Code, resp.Body.String())
	}

	// Verify content saved with extra_data in database
	var extraData string
	if err := database.QueryRow(`SELECT "extra_data" FROM "gocms_content" WHERE "title" = '测试高压球阀'`).Scan(&extraData); err != nil {
		t.Fatalf("query content extra_data: %v", err)
	}
	if !strings.Contains(extraData, "999.00") || !strings.Contains(extraData, "DN50") {
		t.Fatalf("unexpected extra_data: %s", extraData)
	}
}

func TestCustomFeedbackSystem(t *testing.T) {
	server, database, sessionCookie := newCategoryTestServer(t)

	// 1. Submit feedback via /api/feedback with extra fields
	form := url.Values{
		"class_id": {"1"},
		"title":    {"大宗采购询盘"},
		"name":     {"王经理"},
		"phone":    {"13800138000"},
		"email":    {"wang@example.com"},
		"company":  {"远东石化有限公司"},
		"budget":   {"500000"},
		"content":  {"急需采购一批耐酸止回阀，请联系"},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/feedback", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("submit feedback returned %d: %s", resp.Code, resp.Body.String())
	}

	// 2. Verify database record
	var title, name, extraData string
	var state int
	if err := database.QueryRow(`SELECT "title", "name", "state", "extra_data" FROM "gocms_message" WHERE "name" = '王经理'`).Scan(&title, &name, &state, &extraData); err != nil {
		t.Fatalf("query feedback: %v", err)
	}
	if title != "大宗采购询盘" || state != 0 || !strings.Contains(extraData, "500000") {
		t.Fatalf("unexpected feedback row: title=%s, state=%d, extra=%s", title, state, extraData)
	}

	// 3. Admin query feedback
	req = httptest.NewRequest(http.MethodGet, "/api/admin/feedback?keyword=石化", nil)
	req.AddCookie(&http.Cookie{Name: "gocms_admin", Value: sessionCookie})
	resp = httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("admin get feedback returned %d: %s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "大宗采购询盘") {
		t.Fatalf("admin feedback query did not find item: %s", resp.Body.String())
	}

	// 4. Missing required field should be rejected
	missingForm := url.Values{
		"class_id": {"1"},
		"title":    {"只有标题"},
		// name and phone are required in default class
	}
	req = httptest.NewRequest(http.MethodPost, "/api/feedback", strings.NewReader(missingForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp = httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for missing required fields, got %d", resp.Code)
	}
}
