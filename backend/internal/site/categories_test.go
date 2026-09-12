package site

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"gocms/internal/db"
)

func newCategoryTestServer(t *testing.T) (*Server, *sql.DB, string) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSchema(context.Background(), database); err != nil {
		database.Close()
		t.Fatal(err)
	}
	server, err := New(database, t.TempDir())
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	seedTestAdmin(t, server)
	token, err := server.createSession("gocms")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return server, database, token
}

func categoryRequest(t *testing.T, server *Server, token, method, path, payload string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(payload))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "gocms_admin", Value: token})
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

func categoryID(t *testing.T, database *sql.DB, name string) int64 {
	t.Helper()
	var id int64
	if err := database.QueryRow(`SELECT "id" FROM "gocms_category" WHERE "name" = ?`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestCategoryManagement(t *testing.T) {
	server, database, token := newCategoryTestServer(t)

	if response := categoryRequest(t, server, token, http.MethodPost, "/api/admin/categories", `{"name":"根分类","parent_id":0,"order_id":0}`); response.Code != http.StatusOK {
		t.Fatalf("create root returned %d: %s", response.Code, response.Body.String())
	}
	rootID := categoryID(t, database, "根分类")
	childPayload := `{"name":"子分类","parent_id":` + strconv.FormatInt(rootID, 10) + `,"order_id":0}`
	if response := categoryRequest(t, server, token, http.MethodPost, "/api/admin/categories", childPayload); response.Code != http.StatusOK {
		t.Fatalf("create child returned %d: %s", response.Code, response.Body.String())
	}
	childID := categoryID(t, database, "子分类")

	response := categoryRequest(t, server, token, http.MethodGet, "/api/admin/categories", "")
	if response.Code != http.StatusOK {
		t.Fatalf("list returned %d: %s", response.Code, response.Body.String())
	}
	var categories []CategoryItem
	if err := json.Unmarshal(response.Body.Bytes(), &categories); err != nil {
		t.Fatal(err)
	}
	if len(categories) != 2 || categories[1].ParentID != rootID {
		t.Fatalf("unexpected category tree data: %#v", categories)
	}
	if categories[0].ListPath != "category" || categories[0].ListTemplate != "category_list.html" || categories[0].DetailPath != "content" || categories[0].DetailTemplate != "content_detail.html" || categories[0].DetailFilePattern != "{id}.html" {
		t.Fatalf("unexpected root route defaults: %#v", categories[0])
	}
	if categories[1].ListTemplate != "category_list.html" || categories[1].DetailTemplate != "content_detail.html" {
		t.Fatalf("unexpected child template defaults: %#v", categories[1])
	}
	customPayload := `{"name":"子分类","parent_id":` + strconv.FormatInt(rootID, 10) + `,"order_id":0,"list_path":"catalog","list_file_pattern":"{id}.htm","list_template":"custom-list.html","detail_path":"content-detail","detail_file_pattern":"item-{id}.html","detail_template":"custom-detail.html"}`
	if response := categoryRequest(t, server, token, http.MethodPut, "/api/admin/categories/"+strconv.FormatInt(childID, 10), customPayload); response.Code != http.StatusOK {
		t.Fatalf("custom route update returned %d: %s", response.Code, response.Body.String())
	}
	var route struct {
		ListPath          string
		ListFilePattern   string
		ListTemplate      string
		DetailPath        string
		DetailFilePattern string
		DetailTemplate    string
	}
	if err := database.QueryRow(`SELECT "list_path", "list_file_pattern", "list_template", "detail_path", "detail_file_pattern", "detail_template" FROM "gocms_category" WHERE "id" = ?`, childID).
		Scan(&route.ListPath, &route.ListFilePattern, &route.ListTemplate, &route.DetailPath, &route.DetailFilePattern, &route.DetailTemplate); err != nil {
		t.Fatal(err)
	}
	if route.ListPath != "catalog" || route.ListFilePattern != "{id}.htm" || route.ListTemplate != "custom-list.html" || route.DetailPath != "content-detail" || route.DetailFilePattern != "item-{id}.html" || route.DetailTemplate != "custom-detail.html" {
		t.Fatalf("custom route was not persisted: %#v", route)
	}
	invalidPayload := `{"name":"子分类","parent_id":` + strconv.FormatInt(rootID, 10) + `,"order_id":0,"list_path":"../outside","list_file_pattern":"{id}.html","detail_path":"content","detail_file_pattern":"{id}.html"}`
	if response := categoryRequest(t, server, token, http.MethodPut, "/api/admin/categories/"+strconv.FormatInt(childID, 10), invalidPayload); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid route update returned %d: %s", response.Code, response.Body.String())
	}

	cyclePayload := `{"name":"根分类","parent_id":` + strconv.FormatInt(childID, 10) + `,"order_id":0}`
	if response := categoryRequest(t, server, token, http.MethodPut, "/api/admin/categories/"+strconv.FormatInt(rootID, 10), cyclePayload); response.Code != http.StatusBadRequest {
		t.Fatalf("cycle update returned %d: %s", response.Code, response.Body.String())
	}
	if response := categoryRequest(t, server, token, http.MethodDelete, "/api/admin/categories/"+strconv.FormatInt(rootID, 10), ""); response.Code != http.StatusConflict {
		t.Fatalf("delete parent with child returned %d: %s", response.Code, response.Body.String())
	}

	if _, err := database.Exec(`INSERT INTO "gocms_content" ("id", "category_id", "route_key", "title") VALUES (1, ?, '1', '测试内容')`, childID); err != nil {
		t.Fatal(err)
	}
	if response := categoryRequest(t, server, token, http.MethodDelete, "/api/admin/categories/"+strconv.FormatInt(childID, 10), ""); response.Code != http.StatusConflict {
		t.Fatalf("delete category with content returned %d: %s", response.Code, response.Body.String())
	}
	if _, err := database.Exec(`DELETE FROM "gocms_content" WHERE "id" = 1`); err != nil {
		t.Fatal(err)
	}
	if response := categoryRequest(t, server, token, http.MethodDelete, "/api/admin/categories/"+strconv.FormatInt(childID, 10), ""); response.Code != http.StatusOK {
		t.Fatalf("delete child returned %d: %s", response.Code, response.Body.String())
	}
	if response := categoryRequest(t, server, token, http.MethodDelete, "/api/admin/categories/"+strconv.FormatInt(rootID, 10), ""); response.Code != http.StatusOK {
		t.Fatalf("delete root returned %d: %s", response.Code, response.Body.String())
	}
}

func TestContentDetailURLUsesCategoryRoute(t *testing.T) {
	server, database, _ := newCategoryTestServer(t)
	if _, err := database.Exec(`
		INSERT INTO "gocms_category"
		("id", "name", "parent_id", "route_id", "list_path", "list_file_pattern", "list_template", "detail_path", "detail_file_pattern", "detail_template")
		VALUES (9, '自定义分类', 0, 9, 'catalog', '{id}.html', 'category_list.html', 'content-detail', 'item-{id}.htm', 'content_detail.html')`); err != nil {
		t.Fatal(err)
	}
	url, err := server.contentDetailURL(context.Background(), 9, "185")
	if err != nil {
		t.Fatal(err)
	}
	if url != "/content-detail/item-185.htm" {
		t.Fatalf("content detail URL = %q", url)
	}
}

func TestCoverCategoryManagement(t *testing.T) {
	server, database, token := newCategoryTestServer(t)
	payload := `{"name":"联系我们","parent_id":0,"order_id":0,"page_type":"cover","list_path":"","list_file_pattern":"contact.html","list_template":"category_list.html","cover_template":"contact.html","detail_path":"content","detail_file_pattern":"{id}.html","detail_template":"content_detail.html"}`
	response := categoryRequest(t, server, token, http.MethodPost, "/api/admin/categories", payload)
	if response.Code != http.StatusOK {
		t.Fatalf("create cover category returned %d: %s", response.Code, response.Body.String())
	}

	var category CategoryItem
	if err := database.QueryRow(`
		SELECT "id", "parent_id", "page_type", "list_path", "list_file_pattern", "cover_template"
		FROM "gocms_category" WHERE "name" = ?`, "联系我们").
		Scan(&category.ID, &category.ParentID, &category.PageType, &category.ListPath, &category.ListFilePattern, &category.CoverTemplate); err != nil {
		t.Fatal(err)
	}
	if category.ID == 0 || category.ParentID != 0 || category.PageType != "cover" || category.ListPath != "" || category.ListFilePattern != "contact.html" || category.CoverTemplate != "contact.html" {
		t.Fatalf("unexpected cover category: %+v", category)
	}

	invalid := `{"name":"联系我们","parent_id":0,"page_type":"list","list_path":"","list_file_pattern":"contact.html","list_template":"category_list.html","cover_template":"contact.html","detail_path":"content","detail_file_pattern":"{id}.html","detail_template":"content_detail.html"}`
	response = categoryRequest(t, server, token, http.MethodPut, "/api/admin/categories/"+strconv.FormatInt(category.ID, 10), invalid)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid list route for cover category returned %d: %s", response.Code, response.Body.String())
	}
}
