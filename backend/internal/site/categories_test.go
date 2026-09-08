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

	"bilvie/internal/db"
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
	token, err := server.createSession("bilvie")
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
	request.AddCookie(&http.Cookie{Name: "bilvie_admin", Value: token})
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

func categoryID(t *testing.T, database *sql.DB, name string) int64 {
	t.Helper()
	var id int64
	if err := database.QueryRow(`SELECT "id" FROM "benming_ch_ProdCat" WHERE "CatName" = ?`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestProductCategoryManagement(t *testing.T) {
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
	if categories[0].ListPath != "valve" || categories[0].DetailPath != "Product" || categories[0].DetailFilePattern != "{id}.html" {
		t.Fatalf("unexpected root route defaults: %#v", categories[0])
	}
	customPayload := `{"name":"子分类","parent_id":` + strconv.FormatInt(rootID, 10) + `,"order_id":0,"list_path":"catalog","list_file_pattern":"{id}.htm","detail_path":"ProductDetail","detail_file_pattern":"item-{id}.html"}`
	if response := categoryRequest(t, server, token, http.MethodPut, "/api/admin/categories/"+strconv.FormatInt(childID, 10), customPayload); response.Code != http.StatusOK {
		t.Fatalf("custom route update returned %d: %s", response.Code, response.Body.String())
	}
	var route struct {
		ListPath          string
		ListFilePattern   string
		DetailPath        string
		DetailFilePattern string
	}
	if err := database.QueryRow(`SELECT "ListPath", "ListFilePattern", "DetailPath", "DetailFilePattern" FROM "benming_ch_ProdCat" WHERE "id" = ?`, childID).
		Scan(&route.ListPath, &route.ListFilePattern, &route.DetailPath, &route.DetailFilePattern); err != nil {
		t.Fatal(err)
	}
	if route.ListPath != "catalog" || route.ListFilePattern != "{id}.htm" || route.DetailPath != "ProductDetail" || route.DetailFilePattern != "item-{id}.html" {
		t.Fatalf("custom route was not persisted: %#v", route)
	}
	invalidPayload := `{"name":"子分类","parent_id":` + strconv.FormatInt(rootID, 10) + `,"order_id":0,"list_path":"../outside","list_file_pattern":"{id}.html","detail_path":"Product","detail_file_pattern":"{id}.html"}`
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

	if _, err := database.Exec(`INSERT INTO "benming_ch_prod" ("id", "prodName", "CatId") VALUES (1, '测试产品', ?)`, childID); err != nil {
		t.Fatal(err)
	}
	if response := categoryRequest(t, server, token, http.MethodDelete, "/api/admin/categories/"+strconv.FormatInt(childID, 10), ""); response.Code != http.StatusConflict {
		t.Fatalf("delete category with product returned %d: %s", response.Code, response.Body.String())
	}
	if _, err := database.Exec(`DELETE FROM "benming_ch_prod" WHERE "id" = 1`); err != nil {
		t.Fatal(err)
	}
	if response := categoryRequest(t, server, token, http.MethodDelete, "/api/admin/categories/"+strconv.FormatInt(childID, 10), ""); response.Code != http.StatusOK {
		t.Fatalf("delete child returned %d: %s", response.Code, response.Body.String())
	}
	if response := categoryRequest(t, server, token, http.MethodDelete, "/api/admin/categories/"+strconv.FormatInt(rootID, 10), ""); response.Code != http.StatusOK {
		t.Fatalf("delete root returned %d: %s", response.Code, response.Body.String())
	}
}

func TestProductDetailURLUsesCategoryRoute(t *testing.T) {
	server, database, _ := newCategoryTestServer(t)
	if _, err := database.Exec(`
		INSERT INTO "benming_ch_ProdCat"
		("id", "CatName", "Root", "ListPath", "ListFilePattern", "DetailPath", "DetailFilePattern")
		VALUES (9, '自定义分类', 0, 'catalog', '{id}.html', 'product-detail', 'item-{id}.htm')`); err != nil {
		t.Fatal(err)
	}
	url, err := server.productDetailURL(context.Background(), 9, 185)
	if err != nil {
		t.Fatal(err)
	}
	if url != "/product-detail/item-185.htm" {
		t.Fatalf("product detail URL = %q", url)
	}
}
