package site

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"gocms/internal/apikey"
	"gocms/internal/db"
)

func setupTestServer(t *testing.T) (*Server, string, int64) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}

	server, err := New(database, t.TempDir())
	if err != nil {
		database.Close()
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := db.CreateSchema(ctx, database); err != nil {
		database.Close()
		t.Fatal(err)
	}

	res, err := database.ExecContext(ctx, `
		INSERT INTO "gocms_admin_user" ("username", "password_hash", "flags")
		VALUES ('testadmin', 'pass', 'all')`)
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	adminID, _ := res.LastInsertId()

	sessionToken, err := server.createSession("testadmin")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}

	return server, sessionToken, adminID
}

func TestApiKeyEndpointsAndAuth(t *testing.T) {
	server, sessionToken, adminID := setupTestServer(t)
	defer server.database.Close()

	// 1. Unauthenticated request to /api/admin/api-keys fails
	reqNoAuth := httptest.NewRequest(http.MethodGet, "/api/admin/api-keys", nil)
	recNoAuth := httptest.NewRecorder()
	server.Handler().ServeHTTP(recNoAuth, reqNoAuth)
	if recNoAuth.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized without session, got %d", recNoAuth.Code)
	}

	// 2. Create API key with session
	createBody := []byte(`{"name":"Automated Sync Service"}`)
	reqCreate := httptest.NewRequest(http.MethodPost, "/api/admin/api-keys", bytes.NewReader(createBody))
	reqCreate.Header.Set("Content-Type", "application/json")
	reqCreate.AddCookie(&http.Cookie{Name: "gocms_admin", Value: sessionToken})
	recCreate := httptest.NewRecorder()
	server.Handler().ServeHTTP(recCreate, reqCreate)

	if recCreate.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", recCreate.Code, recCreate.Body.String())
	}

	var createResp struct {
		OK      bool                    `json:"ok"`
		Data    apikey.ApiKeyWithSecret `json:"data"`
		Message string                  `json:"message"`
	}
	if err := json.NewDecoder(recCreate.Body).Decode(&createResp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if createResp.Data.Key == "" || createResp.Data.Name != "Automated Sync Service" {
		t.Fatalf("unexpected create response data: %+v", createResp.Data)
	}
	apiKeySecret := createResp.Data.Key
	apiKeyID := createResp.Data.ID

	// 3. List API keys
	reqList := httptest.NewRequest(http.MethodGet, "/api/admin/api-keys", nil)
	reqList.AddCookie(&http.Cookie{Name: "gocms_admin", Value: sessionToken})
	recList := httptest.NewRecorder()
	server.Handler().ServeHTTP(recList, reqList)

	if recList.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on list, got %d", recList.Code)
	}
	var listResp struct {
		OK   bool            `json:"ok"`
		Data []apikey.ApiKey `json:"data"`
	}
	if err := json.NewDecoder(recList.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listResp.Data) != 1 || listResp.Data[0].ID != apiKeyID {
		t.Fatalf("unexpected list data: %+v", listResp.Data)
	}

	// 4. Test API Key authentication on protected admin endpoint (/api/admin/stats)
	// 4a. Using X-API-Key header
	reqStatsHeader := httptest.NewRequest(http.MethodGet, "/api/admin/stats", nil)
	reqStatsHeader.Header.Set("X-API-Key", apiKeySecret)
	reqStatsHeader.RemoteAddr = "198.51.100.42:12345"
	recStatsHeader := httptest.NewRecorder()
	server.Handler().ServeHTTP(recStatsHeader, reqStatsHeader)
	if recStatsHeader.Code != http.StatusOK {
		t.Fatalf("expected 200 OK with X-API-Key, got %d: %s", recStatsHeader.Code, recStatsHeader.Body.String())
	}

	// 4b. Using Authorization: Bearer <key>
	reqStatsBearer := httptest.NewRequest(http.MethodGet, "/api/admin/stats", nil)
	reqStatsBearer.Header.Set("Authorization", "Bearer "+apiKeySecret)
	recStatsBearer := httptest.NewRecorder()
	server.Handler().ServeHTTP(recStatsBearer, reqStatsBearer)
	if recStatsBearer.Code != http.StatusOK {
		t.Fatalf("expected 200 OK with Bearer API Key, got %d: %s", recStatsBearer.Code, recStatsBearer.Body.String())
	}

	// 5. Test that API Key CANNOT be used to manage API Keys (must require session auth)
	reqManageWithKey := httptest.NewRequest(http.MethodGet, "/api/admin/api-keys", nil)
	reqManageWithKey.Header.Set("X-API-Key", apiKeySecret)
	recManageWithKey := httptest.NewRecorder()
	server.Handler().ServeHTTP(recManageWithKey, reqManageWithKey)
	if recManageWithKey.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when accessing API Key management using API Key, got %d", recManageWithKey.Code)
	}

	// 6. Rotate API Key
	reqRotate := httptest.NewRequest(http.MethodPost, "/api/admin/api-keys/"+strconv.FormatInt(apiKeyID, 10)+"/rotate", nil)
	reqRotate.AddCookie(&http.Cookie{Name: "gocms_admin", Value: sessionToken})
	recRotate := httptest.NewRecorder()
	server.Handler().ServeHTTP(recRotate, reqRotate)
	if recRotate.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on rotate, got %d: %s", recRotate.Code, recRotate.Body.String())
	}

	var rotateResp struct {
		OK   bool                    `json:"ok"`
		Data apikey.ApiKeyWithSecret `json:"data"`
	}
	if err := json.NewDecoder(recRotate.Body).Decode(&rotateResp); err != nil {
		t.Fatalf("decode rotate response: %v", err)
	}
	rotatedKeyID := rotateResp.Data.ID
	rotatedSecret := rotateResp.Data.Key

	// Old key must no longer work
	reqOldKey := httptest.NewRequest(http.MethodGet, "/api/admin/stats", nil)
	reqOldKey.Header.Set("X-API-Key", apiKeySecret)
	recOldKey := httptest.NewRecorder()
	server.Handler().ServeHTTP(recOldKey, reqOldKey)
	if recOldKey.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with old rotated key, got %d", recOldKey.Code)
	}

	// New key must work
	reqNewKey := httptest.NewRequest(http.MethodGet, "/api/admin/stats", nil)
	reqNewKey.Header.Set("X-API-Key", rotatedSecret)
	recNewKey := httptest.NewRecorder()
	server.Handler().ServeHTTP(recNewKey, reqNewKey)
	if recNewKey.Code != http.StatusOK {
		t.Fatalf("expected 200 with new rotated key, got %d", recNewKey.Code)
	}

	// 7. Revoke new key
	revokeBody := []byte(`{"reason":"Testing revocation"}`)
	reqRevoke := httptest.NewRequest(http.MethodPost, "/api/admin/api-keys/"+strconv.FormatInt(rotatedKeyID, 10)+"/revoke", bytes.NewReader(revokeBody))
	reqRevoke.Header.Set("Content-Type", "application/json")
	reqRevoke.AddCookie(&http.Cookie{Name: "gocms_admin", Value: sessionToken})
	recRevoke := httptest.NewRecorder()
	server.Handler().ServeHTTP(recRevoke, reqRevoke)
	if recRevoke.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on revoke, got %d: %s", recRevoke.Code, recRevoke.Body.String())
	}

	// Revoked key must no longer work
	reqRevokedStats := httptest.NewRequest(http.MethodGet, "/api/admin/stats", nil)
	reqRevokedStats.Header.Set("X-API-Key", rotatedSecret)
	recRevokedStats := httptest.NewRecorder()
	server.Handler().ServeHTTP(recRevokedStats, reqRevokedStats)
	if recRevokedStats.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with revoked key, got %d", recRevokedStats.Code)
	}

	// 8. Check events endpoint
	reqEvents := httptest.NewRequest(http.MethodGet, "/api/admin/api-keys/"+strconv.FormatInt(apiKeyID, 10)+"/events", nil)
	reqEvents.AddCookie(&http.Cookie{Name: "gocms_admin", Value: sessionToken})
	recEvents := httptest.NewRecorder()
	server.Handler().ServeHTTP(recEvents, reqEvents)
	if recEvents.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on events, got %d: %s", recEvents.Code, recEvents.Body.String())
	}

	var eventsResp struct {
		OK   bool                 `json:"ok"`
		Data []apikey.ApiKeyEvent `json:"data"`
	}
	if err := json.NewDecoder(recEvents.Body).Decode(&eventsResp); err != nil {
		t.Fatalf("decode events response: %v", err)
	}
	if len(eventsResp.Data) < 2 {
		t.Fatalf("expected at least 2 events on original key (created, rotated), got %d", len(eventsResp.Data))
	}
	_ = adminID
	_ = time.Second
}
