package site

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type r2TestTransport func(*http.Request) (*http.Response, error)

func (send r2TestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return send(r) }

func TestSharedR2MediaStorageAndPDF(t *testing.T) {
	server, database, token := newCategoryTestServer(t)
	server.assetsRoot = t.TempDir()
	if err := server.ConfigureMediaStorage(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	const endpoint = "https://0123456789abcdef0123456789abcdef.r2.cloudflarestorage.com"
	objects := make(map[string][]byte)
	server.mediaStorageClient = &http.Client{Transport: r2TestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme+"://"+r.URL.Host != endpoint || !strings.HasPrefix(r.URL.Path, "/test-files/") {
			t.Errorf("unexpected S3 request %s %s", r.Method, r.URL)
		}
		if !strings.Contains(r.Header.Get("Authorization"), "Credential=test-access/") {
			t.Error("S3 request was not signed with the configured credential")
		}
		key := strings.TrimPrefix(r.URL.Path, "/test-files/")
		status := http.StatusOK
		var body []byte
		switch r.Method {
		case http.MethodPut:
			data, err := io.ReadAll(r.Body)
			if err != nil {
				return nil, err
			}
			objects[key] = data
		case http.MethodGet:
			body = objects[key]
		case http.MethodDelete:
			delete(objects, key)
			status = http.StatusNoContent
		default:
			t.Errorf("unexpected S3 method %s", r.Method)
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(body))}, nil
	})}

	if _, err := database.Exec(`INSERT INTO gocms_site (id, name, code, domain)
		VALUES (2, 'Second site', 'second', 'second.example.test')`); err != nil {
		t.Fatal(err)
	}
	const configURL = "/api/admin/media-storage"
	setup := map[string]any{"enabled": true, "images": true, "endpoint": endpoint,
		"bucket": "test-files", "access_key": "test-access", "secret": "test-secret"}
	check := func(status int, response *httptest.ResponseRecorder) {
		t.Helper()
		if response.Code != status {
			t.Fatalf("status %d, want %d: %s", response.Code, status, response.Body)
		}
	}
	check(200, contentJSONRequest(t, server, token, http.MethodPost, configURL+"/test", setup))
	if len(objects) != 0 {
		t.Fatal("connection test left an object")
	}
	check(200, contentJSONRequest(t, server, token, http.MethodPut, configURL, setup))
	settings := contentJSONRequest(t, server, token, http.MethodGet, configURL+"?site_id=2", nil)
	check(200, settings)
	if strings.Contains(settings.Body.String(), "test-secret") || !strings.Contains(settings.Body.String(), `"has_secret":true`) {
		t.Fatal("unexpected credential status")
	}
	var stored string
	if err := database.QueryRow(`SELECT secret_ciphertext FROM gocms_platform_media_storage WHERE id=1`).Scan(&stored); err != nil || stored == "test-secret" || stored == "" {
		t.Fatal("storage secret was not encrypted")
	}

	upload := func(data []byte, language, site string, status int) MediaAsset {
		t.Helper()
		var body bytes.Buffer
		writer := multipartPDF(t, &body, data)
		request := httptest.NewRequest(http.MethodPost, "/api/admin/media?kind=pdf&language="+language+"&site_id="+site, &body)
		request.Header.Set("Content-Type", writer)
		request.AddCookie(&http.Cookie{Name: "gocms_admin", Value: token})
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		check(status, response)
		var result struct{ Asset MediaAsset }
		if status < 300 && json.Unmarshal(response.Body.Bytes(), &result) != nil {
			t.Fatal("invalid upload response")
		}
		return result.Asset
	}

	data := []byte("%PDF-1.7\n%%EOF\n")
	asset := upload(data, "de-de", "1", http.StatusCreated)
	if asset.Language != "de-DE" || asset.Storage != "r2" || asset.Kind != "pdf" {
		t.Fatalf("unexpected attachment %+v", asset)
	}
	duplicate := upload(data, "de-DE", "1", http.StatusOK)
	if duplicate.ID != asset.ID || len(objects) != 1 {
		t.Fatal("duplicate PDF was not reused")
	}
	upload([]byte("invalid pdf"), "de-DE", "1", http.StatusBadRequest)
	for key, body := range objects {
		if !strings.HasPrefix(key, "sites/1/pdf/de-DE/") || !bytes.Equal(body, data) {
			t.Fatalf("incorrect PDF key or bytes for %s", key)
		}
	}

	fetch := func(method, address string, status int) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httptest.NewRequest(method, address, nil))
		check(status, response)
		return response
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		result := fetch(method, asset.URL, http.StatusTemporaryRedirect)
		target, err := url.Parse(result.Header().Get("Location"))
		if err != nil || !strings.HasPrefix(target.Path, "/test-files/sites/1/pdf/de-DE/") ||
			target.Query().Get("X-Amz-Signature") == "" || target.Query().Get("X-Amz-Expires") != "300" {
			t.Fatalf("unexpected download redirect for %s", method)
		}
	}
	image := uploadTestMedia(t, server, token, "test.png")
	for key := range objects {
		if !strings.HasPrefix(key, "sites/1/image/") && !strings.HasPrefix(key, "sites/1/pdf/de-DE/") {
			t.Fatalf("unexpected key %s", key)
		}
	}
	if image.Storage != "r2" || len(objects) != 2 {
		t.Fatal("image did not use R2")
	}

	second := upload(data, "de-DE", "2", http.StatusCreated)
	if second.ID == asset.ID || second.SiteID != 2 || len(objects) != 3 {
		t.Fatal("separate sites did not get independent attachment records")
	}
	fetch(http.MethodGet, second.URL, http.StatusNotFound)
	secondDownload := httptest.NewRecorder()
	server.Handler().ServeHTTP(secondDownload, httptest.NewRequest(http.MethodGet, "http://second.example.test"+second.URL, nil))
	check(http.StatusTemporaryRedirect, secondDownload)
	secondTarget, err := url.Parse(secondDownload.Header().Get("Location"))
	if err != nil || secondTarget.Host != "0123456789abcdef0123456789abcdef.r2.cloudflarestorage.com" {
		t.Fatal("second site did not use the shared endpoint")
	}
	secondKey, ok := strings.CutPrefix(secondTarget.Path, "/test-files/")
	if !ok || !strings.HasPrefix(secondKey, "sites/2/pdf/de-DE/") || !bytes.Equal(objects[secondKey], data) {
		t.Fatal("second site file was not saved in its own prefix of the shared bucket")
	}
	setup["bucket"] = "other-files"
	check(http.StatusConflict, contentJSONRequest(t, server, token, http.MethodPut, configURL+"?site_id=2", setup))
	setup["bucket"] = "test-files"

	setup["enabled"], setup["images"], setup["secret"] = false, false, ""
	setup["public_url"] = "https://downloads.example.com"
	check(200, contentJSONRequest(t, server, token, http.MethodPut, configURL, setup))
	result := fetch(http.MethodGet, asset.URL, http.StatusTemporaryRedirect)
	if !strings.HasPrefix(result.Header().Get("Location"), "https://downloads.example.com/sites/1/pdf/de-DE/") {
		t.Fatal("existing R2 object lost its download URL after disabling R2 uploads")
	}
	localData := []byte("%PDF-1.5\n%%EOF\n")
	local := upload(localData, "en-GB", "1", http.StatusCreated)
	result = fetch(http.MethodGet, local.URL, http.StatusOK)
	if local.Storage != "local" || !bytes.Equal(localData, result.Body.Bytes()) {
		t.Fatal("local upload did not work after disabling R2")
	}

	// Attachment links in secondary languages must also protect the R2 object.
	content := map[string]any{"title": "Technical document", "translations": map[string]any{
		"de": map[string]any{"title": "Technisches Dokument", "content": `<a href="https://example.test` + asset.URL + `">PDF</a>`},
	}}
	check(200, contentJSONRequest(t, server, token, http.MethodPost, "/api/admin/content", content))
	check(http.StatusConflict, mediaItemRequest(t, server, token, http.MethodDelete, asset.ID))
	if len(objects) != 3 {
		t.Fatal("a referenced object was deleted")
	}
	check(200, mediaItemRequest(t, server, token, http.MethodDelete, image.ID))
	fetch(http.MethodGet, image.URL, http.StatusNotFound)
	// Records written before the shared bucket keep their original keys and credentials.
	vault, err := server.mediaStorageCipher(false)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := vault.Seal("old-secret", "media-storage:1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO gocms_media_storage
		(site_id, enabled, images, endpoint, bucket, public_url, access_key, secret_ciphertext)
		VALUES (1, 1, 0, ?, 'legacy-files', '', 'old-access', ?)`, endpoint, secret); err != nil {
		t.Fatal(err)
	}
	legacyURL := mediaFilePrefix + "1/legacy.pdf"
	if _, err := database.Exec(`INSERT INTO gocms_media
		(site_id, kind, storage_provider, storage_path, public_path, mime_type)
		VALUES (1, 'pdf', 'r2', 'pdf/de-DE/1/legacy.pdf', ?, 'application/pdf')`, legacyURL); err != nil {
		t.Fatal(err)
	}
	legacyLocation := fetch(http.MethodGet, legacyURL, http.StatusTemporaryRedirect).Header().Get("Location")
	legacyTarget, err := url.Parse(legacyLocation)
	if err != nil || legacyTarget.Path != "/legacy-files/pdf/de-DE/1/legacy.pdf" ||
		!strings.HasPrefix(legacyTarget.Query().Get("X-Amz-Credential"), "old-access/") {
		t.Fatal("legacy attachment did not keep its original bucket and credentials")
	}
}

func multipartPDF(t *testing.T, body io.Writer, data []byte) string {
	t.Helper()
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "说明书.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return writer.FormDataContentType()
}
