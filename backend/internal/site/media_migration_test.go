package site

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestLocalImageMigrationPreservesURLAndVerifiesR2(t *testing.T) {
	server, database, token := newCategoryTestServer(t)
	server.assetsRoot = t.TempDir()
	if err := server.ConfigureMediaStorage(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO gocms_site (id, code, name, domain)
		VALUES (2, 'second', 'Second', 'second.example.test')`); err != nil {
		t.Fatal(err)
	}
	objects := map[string][]byte{}
	corruptRead := false
	server.mediaStorageClient = &http.Client{Transport: r2TestTransport(func(r *http.Request) (*http.Response, error) {
		key := strings.TrimPrefix(r.URL.Path, "/test-files/")
		status := http.StatusOK
		var body []byte
		switch r.Method {
		case http.MethodPut:
			if _, exists := objects[key]; exists && r.Header.Get("If-None-Match") == "*" {
				status = http.StatusPreconditionFailed
				break
			}
			if strings.Contains(key, "/migrated/") && r.Header.Get("If-None-Match") != "*" {
				t.Error("migration wrote without a create-only condition")
			}
			data, err := io.ReadAll(r.Body)
			if err != nil {
				return nil, err
			}
			objects[key] = data
		case http.MethodGet:
			var exists bool
			body, exists = objects[key]
			if !exists {
				status = http.StatusNotFound
			}
			if corruptRead && strings.Contains(key, "/migrated/") {
				body = []byte("corrupted R2 response")
			}
		case http.MethodDelete:
			delete(objects, key)
			status = http.StatusNoContent
		default:
			return nil, fmt.Errorf("unexpected method %s", r.Method)
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(body))}, nil
	})}

	check := func(response *httptest.ResponseRecorder, status int) {
		t.Helper()
		if response.Code != status {
			t.Fatalf("got %d, want %d: %s", response.Code, status, response.Body)
		}
	}
	check(contentJSONRequest(t, server, token, http.MethodPut, "/api/admin/media-storage",
		map[string]any{"enabled": true, "images": false, "bucket": "test-files", "access_key": "access", "secret": "secret",
			"endpoint": "https://0123456789abcdef0123456789abcdef.r2.cloudflarestorage.com", "public_url": "https://downloads.example.test"}), http.StatusOK)

	other := uploadTestMedia(t, server, token, "other.png")
	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	part, err := writer.CreateFormFile("file", "keep-url.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(testPNG(t)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/admin/media?site_id=2", &form)
	request.AddCookie(&http.Cookie{Name: "gocms_admin", Value: token})
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	check(response, http.StatusCreated)
	var upload struct{ Asset MediaAsset }
	if err := json.Unmarshal(response.Body.Bytes(), &upload); err != nil {
		t.Fatal(err)
	}
	original := upload.Asset
	diskPath, err := server.mediaDiskPath(original.URL)
	if err != nil || original.Storage != "local" || original.SiteID != 2 {
		t.Fatalf("unexpected source: %+v %v", original, err)
	}
	read := func(method string, status int) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httptest.NewRequest(method, "http://second.example.test"+original.URL, nil))
		check(response, status)
		return response
	}
	read(http.MethodGet, http.StatusOK)

	migrateURL := mediaMigrationRoute + "?site_id=2"
	unauthenticated := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, migrateURL, nil))
	check(unauthenticated, http.StatusUnauthorized)
	response = contentJSONRequest(t, server, token, http.MethodGet, migrateURL, nil)
	check(response, http.StatusOK)
	var preview imageMigrationPreview
	if err := json.Unmarshal(response.Body.Bytes(), &preview); err != nil || !preview.Ready ||
		preview.Count != 1 || preview.Bytes != original.SizeBytes || preview.ThroughID != original.ID {
		t.Fatalf("unexpected preview: %+v (%v)", preview, err)
	}
	migrate := func() imageMigrationBatch {
		t.Helper()
		response := contentJSONRequest(t, server, token, http.MethodPost, migrateURL, map[string]any{"through_id": preview.ThroughID})
		check(response, http.StatusOK)
		var batch imageMigrationBatch
		if err := json.Unmarshal(response.Body.Bytes(), &batch); err != nil {
			t.Fatal(err)
		}
		return batch
	}
	corruptRead = true
	if result := migrate(); result.Migrated != 0 || len(result.Failures) != 1 || result.Failures[0].ID != original.ID {
		t.Fatalf("mismatched bytes changed migration state: %+v", result)
	}
	read(http.MethodGet, http.StatusOK)

	corruptRead = false
	if result := migrate(); result.Migrated != 1 || len(result.Failures) != 0 || result.More {
		t.Fatalf("retry did not switch a verified object: %+v", result)
	}
	if result := migrate(); result.Migrated != 0 || len(result.Failures) != 0 || len(objects) != 1 {
		t.Fatalf("repeating a completed migration changed objects: %+v", result)
	}
	stored, err := server.readMedia(t.Context(), original.ID)
	if err != nil || stored.URL != original.URL || stored.Storage != "r2" || stored.SHA256 != original.SHA256 {
		t.Fatalf("attachment identity or content changed: %+v, %v", stored, err)
	}
	stillLocal, err := server.readMedia(t.Context(), other.ID)
	if err != nil || stillLocal.Storage != "local" {
		t.Fatalf("migration crossed sites: %+v %v", stillLocal, err)
	}
	if _, err := os.Stat(diskPath); err != nil {
		t.Fatalf("local source was removed: %v", err)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		redirect := read(method, http.StatusTemporaryRedirect)
		if redirect.Header().Get("Location") != "https://downloads.example.test/"+stored.storagePath ||
			redirect.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("incorrect migration redirect for %s: %v", method, redirect.Header())
		}
	}
	check(contentJSONRequest(t, server, token, http.MethodDelete, fmt.Sprintf("/api/admin/media/%d?site_id=2", original.ID), nil), http.StatusOK)
	read(http.MethodGet, http.StatusNotFound)
	if _, err := os.Stat(diskPath); !os.IsNotExist(err) || len(objects) != 0 {
		t.Fatalf("deleting a migrated attachment left a copy: %v", err)
	}
}
