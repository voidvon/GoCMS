package site

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAdminMediaUploadAndAccess(t *testing.T) {
	server, database, token := newCategoryTestServer(t)
	server.assetsRoot = t.TempDir()
	content := testPNG(t)

	response := mediaUploadRequest(t, server, token, "cover.png", content)
	if response.Code != http.StatusCreated {
		t.Fatalf("upload returned %d: %s", response.Code, response.Body.String())
	}
	var result struct {
		OK    bool       `json:"ok"`
		Asset MediaAsset `json:"asset"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Asset.Kind != "image" || result.Asset.MimeType != "image/png" || result.Asset.Width != 3 || result.Asset.Height != 2 {
		t.Fatalf("unexpected media response: %+v", result)
	}
	if !strings.HasPrefix(result.Asset.URL, "/images/uploads/") || !strings.HasSuffix(result.Asset.URL, ".png") {
		t.Fatalf("unexpected media URL: %q", result.Asset.URL)
	}

	diskPath := filepath.Join(server.assetsRoot, filepath.FromSlash(strings.TrimPrefix(result.Asset.URL, "/")))
	stored, err := os.ReadFile(diskPath)
	if err != nil {
		t.Fatalf("read stored image: %v", err)
	}
	if !bytes.Equal(stored, content) {
		t.Fatal("stored image differs from upload")
	}

	publicResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(publicResponse, httptest.NewRequest(http.MethodGet, result.Asset.URL, nil))
	if publicResponse.Code != http.StatusOK || publicResponse.Header().Get("Content-Type") != "image/png" || !bytes.Equal(publicResponse.Body.Bytes(), content) {
		t.Fatalf("public image response = %d %q (%d bytes)", publicResponse.Code, publicResponse.Header().Get("Content-Type"), publicResponse.Body.Len())
	}

	var mediaCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM "gocms_media"`).Scan(&mediaCount); err != nil {
		t.Fatal(err)
	}
	if mediaCount != 1 {
		t.Fatalf("media count = %d", mediaCount)
	}
}

func TestAdminMediaUploadRequiresImageAndAdmin(t *testing.T) {
	server, database, token := newCategoryTestServer(t)
	server.assetsRoot = t.TempDir()
	content := testPNG(t)

	if response := mediaUploadRequest(t, server, "", "unauthorized.png", content); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized upload returned %d: %s", response.Code, response.Body.String())
	}
	if response := mediaUploadRequest(t, server, token, "not-image.png", []byte("not an image")); response.Code != http.StatusBadRequest {
		t.Fatalf("non-image upload returned %d: %s", response.Code, response.Body.String())
	}

	var mediaCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM "gocms_media"`).Scan(&mediaCount); err != nil {
		t.Fatal(err)
	}
	if mediaCount != 0 {
		t.Fatalf("media count after rejected uploads = %d", mediaCount)
	}
}

func TestContentMediaReferencesFollowContent(t *testing.T) {
	server, database, token := newCategoryTestServer(t)
	server.assetsRoot = t.TempDir()
	bodyAsset := uploadTestMedia(t, server, token, "body.png")
	coverAsset := uploadTestMedia(t, server, token, "cover.png")

	createPayload := map[string]any{
		"title":       "带图片的内容",
		"content":     `<p><img src="` + bodyAsset.URL + `"></p>`,
		"cover_image": coverAsset.URL,
	}
	if response := contentJSONRequest(t, server, token, http.MethodPost, "/api/admin/content", createPayload); response.Code != http.StatusOK {
		t.Fatalf("content create returned %d: %s", response.Code, response.Body.String())
	}

	var contentID int64
	if err := database.QueryRow(`SELECT "id" FROM "gocms_content" WHERE "title" = ?`, "带图片的内容").Scan(&contentID); err != nil {
		t.Fatal(err)
	}
	refs := mediaReferences(t, database, contentID)
	if len(refs) != 2 || refs["body"] != bodyAsset.ID || refs["cover_image"] != coverAsset.ID {
		t.Fatalf("unexpected initial media references: %#v", refs)
	}

	detailResponse := mediaItemRequest(t, server, token, http.MethodGet, coverAsset.ID)
	var detail struct {
		MediaAsset
		References []MediaReference `json:"references"`
	}
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("media detail returned %d: %s", detailResponse.Code, detailResponse.Body.String())
	}
	if err := json.Unmarshal(detailResponse.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.ID != coverAsset.ID || len(detail.References) != 1 || detail.References[0].ContentID != contentID || detail.References[0].Title != "带图片的内容" || detail.References[0].FieldName != "cover_image" {
		t.Fatalf("unexpected media detail: %+v", detail)
	}
	if response := mediaItemRequest(t, server, "", http.MethodGet, coverAsset.ID); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized detail returned %d", response.Code)
	}

	if response := mediaItemRequest(t, server, token, http.MethodDelete, coverAsset.ID); response.Code != http.StatusConflict {
		t.Fatalf("delete referenced cover returned %d: %s", response.Code, response.Body.String())
	}

	updatePayload := map[string]any{
		"title":   "带图片的内容",
		"content": `<p><img src="` + coverAsset.URL + `"></p>`,
	}
	if response := contentJSONRequest(t, server, token, http.MethodPut, "/api/admin/content/"+strconv.FormatInt(contentID, 10), updatePayload); response.Code != http.StatusOK {
		t.Fatalf("content update returned %d: %s", response.Code, response.Body.String())
	}
	refs = mediaReferences(t, database, contentID)
	if len(refs) != 1 || refs["body"] != coverAsset.ID {
		t.Fatalf("unexpected updated media references: %#v", refs)
	}

	if response := mediaItemRequest(t, server, token, http.MethodDelete, bodyAsset.ID); response.Code != http.StatusOK {
		t.Fatalf("delete unreferenced body image returned %d: %s", response.Code, response.Body.String())
	}
	if response := contentJSONRequest(t, server, token, http.MethodDelete, "/api/admin/content/"+strconv.FormatInt(contentID, 10), nil); response.Code != http.StatusOK {
		t.Fatalf("content delete returned %d: %s", response.Code, response.Body.String())
	}
	if refs := mediaReferences(t, database, contentID); len(refs) != 0 {
		t.Fatalf("media references remained after content delete: %#v", refs)
	}
	if response := mediaItemRequest(t, server, token, http.MethodDelete, coverAsset.ID); response.Code != http.StatusOK {
		t.Fatalf("delete image after content delete returned %d: %s", response.Code, response.Body.String())
	}
}

func TestContentImageURLsUsesSrcAttribute(t *testing.T) {
	urls := contentImageURLs(`<img data-src="/images/lazy.png" src="/images/uploads/real.png">`)
	if len(urls) != 1 || urls[0] != "/images/uploads/real.png" {
		t.Fatalf("content image URLs = %#v", urls)
	}
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, 3, 2))
	picture.Set(0, 0, color.RGBA{R: 255, A: 255})
	picture.Set(1, 0, color.RGBA{G: 255, A: 255})
	picture.Set(2, 0, color.RGBA{B: 255, A: 255})
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, picture); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func mediaUploadRequest(t *testing.T, server *Server, token, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/admin/media", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		request.AddCookie(&http.Cookie{Name: "gocms_admin", Value: token})
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

func uploadTestMedia(t *testing.T, server *Server, token, filename string) MediaAsset {
	t.Helper()
	response := mediaUploadRequest(t, server, token, filename, testPNG(t))
	if response.Code != http.StatusCreated {
		t.Fatalf("upload %s returned %d: %s", filename, response.Code, response.Body.String())
	}
	var result struct {
		Asset MediaAsset `json:"asset"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.Asset
}

func contentJSONRequest(t *testing.T, server *Server, token string, method, path string, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, path, &body)
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "gocms_admin", Value: token})
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

func mediaItemRequest(t *testing.T, server *Server, token string, method string, id int64) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "/api/admin/media/"+strconv.FormatInt(id, 10), nil)
	request.AddCookie(&http.Cookie{Name: "gocms_admin", Value: token})
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

func mediaReferences(t *testing.T, database *sql.DB, contentID int64) map[string]int64 {
	t.Helper()
	rows, err := database.QueryContext(context.Background(), `SELECT "field_name", "media_id" FROM "gocms_media_ref" WHERE "content_id" = ?`, contentID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	refs := make(map[string]int64)
	for rows.Next() {
		var field string
		var mediaID int64
		if err := rows.Scan(&field, &mediaID); err != nil {
			t.Fatal(err)
		}
		refs[field] = mediaID
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return refs
}

func TestAdminMediaManagementSearchAndEmptyReferences(t *testing.T) {
	server, _, token := newCategoryTestServer(t)
	server.assetsRoot = t.TempDir()
	first := uploadTestMedia(t, server, token, "manual.png")
	second := uploadTestMedia(t, server, token, "manual-cover.png")
	uploadTestMedia(t, server, token, "other.png")
	response := contentJSONRequest(t, server, token, http.MethodGet, "/api/admin/media?q=manual&page_size=1&page=2", nil)
	var page MediaPage
	if response.Code != http.StatusOK {
		t.Fatalf("list returned %d: %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || page.Page != 2 || len(page.Items) != 1 || page.Items[0].ID != first.ID {
		t.Fatalf("unexpected page: %+v", page)
	}
	response = mediaItemRequest(t, server, token, http.MethodGet, second.ID)
	var detail struct {
		References []MediaReference `json:"references"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.References == nil || len(detail.References) != 0 {
		t.Fatalf("expected empty references: %s", response.Body.String())
	}
	if response := mediaItemRequest(t, server, token, http.MethodDelete, second.ID); response.Code != http.StatusOK {
		t.Fatalf("delete returned %d", response.Code)
	}
	if response := mediaItemRequest(t, server, token, http.MethodGet, second.ID); response.Code != http.StatusNotFound {
		t.Fatalf("deleted detail returned %d", response.Code)
	}
	diskPath := filepath.Join(server.assetsRoot, filepath.FromSlash(strings.TrimPrefix(second.URL, "/")))
	if _, err := os.Stat(diskPath); !os.IsNotExist(err) {
		t.Fatalf("deleted file still exists: %v", err)
	}
}
