package site

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"gocms/internal/db"
	"gocms/internal/templatelabel"
)

func TestAdminTemplateLabelManagement(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.CreateSchema(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	server, err := New(database, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seedTestAdmin(t, server)
	token, err := server.createSession("gocms")
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, payload string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, bytes.NewBufferString(payload))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(&http.Cookie{Name: "gocms_admin", Value: token})
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		return response
	}

	categoryResponse := request(http.MethodPost, "/api/admin/theme/label-categories", `{"name":"内容卡片","sort_order":1}`)
	if categoryResponse.Code != http.StatusOK {
		t.Fatalf("create label category status = %d: %s", categoryResponse.Code, categoryResponse.Body.String())
	}
	var createdCategory struct {
		Category templatelabel.Category `json:"category"`
	}
	if err := json.Unmarshal(categoryResponse.Body.Bytes(), &createdCategory); err != nil {
		t.Fatal(err)
	}

	labelResponse := request(http.MethodPost, "/api/admin/theme/label-templates", `{"key":"article-card","name":"文章卡片","category_id":`+strconv.FormatInt(createdCategory.Category.ID, 10)+`,"context":"list","description":"列表卡片","temptext":"<ul>[!--list.temp--]<!--list.var1-->[!--list.temp--]</ul>","listvar":"<li><a href=\"[!--url--]\">[!--title--]</a></li>"}`)
	if labelResponse.Code != http.StatusOK {
		t.Fatalf("create template label status = %d: %s", labelResponse.Code, labelResponse.Body.String())
	}
	var createdLabel struct {
		Item templatelabel.Label `json:"item"`
	}
	if err := json.Unmarshal(labelResponse.Body.Bytes(), &createdLabel); err != nil {
		t.Fatal(err)
	}
	if createdLabel.Item.CategoryName != "内容卡片" {
		t.Fatalf("created label category = %+v", createdLabel.Item)
	}

	listResponse := request(http.MethodGet, "/api/admin/theme/label-templates?page=1&page_size=20&q=article", "")
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list template labels status = %d: %s", listResponse.Code, listResponse.Body.String())
	}
	var listed templateLabelsResponse
	if err := json.Unmarshal(listResponse.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.PageSize != 20 || listed.Total != 1 || len(listed.Items) != 1 || len(listed.Categories) != 1 {
		t.Fatalf("unexpected template labels response: %+v", listed)
	}

	id := strconv.FormatInt(createdLabel.Item.ID, 10)
	updateResponse := request(http.MethodPut, "/api/admin/theme/label-templates/"+id, `{"key":"article-card","name":"文章卡片新版","category_id":`+strconv.FormatInt(createdCategory.Category.ID, 10)+`,"context":"detail","temptext":"<ul class=\"news-list\">[!--list.temp--]<!--list.var1-->[!--list.temp--]</ul>","listvar":"<li><a href=\"[!--url--]\">[!--title--]</a></li>"}`)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("update template label status = %d: %s", updateResponse.Code, updateResponse.Body.String())
	}
	var updated struct {
		Item templatelabel.Label `json:"item"`
	}
	if err := json.Unmarshal(updateResponse.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Item.Name != "文章卡片新版" || updated.Item.Context != templatelabel.ContextDetail {
		t.Fatalf("updated template label = %+v", updated.Item)
	}

	if response := request(http.MethodDelete, "/api/admin/theme/label-templates/"+id, ""); response.Code != http.StatusOK {
		t.Fatalf("delete template label status = %d: %s", response.Code, response.Body.String())
	}
	if response := request(http.MethodDelete, "/api/admin/theme/label-categories/"+strconv.FormatInt(createdCategory.Category.ID, 10), ""); response.Code != http.StatusOK {
		t.Fatalf("delete label category status = %d: %s", response.Code, response.Body.String())
	}
}
