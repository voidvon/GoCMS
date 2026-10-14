package site

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestCaptchaEndpointsAndSubmission(t *testing.T) {
	server, _, _ := newCategoryTestServer(t)

	// 1. GET /api/captcha
	req := httptest.NewRequest(http.MethodGet, "/api/captcha", nil)
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("GET /api/captcha returned status %d: %s", resp.Code, resp.Body.String())
	}

	var captResp struct {
		OK    bool   `json:"ok"`
		Key   string `json:"key"`
		Image string `json:"image"`
		Tile  string `json:"tile"`
		TileX int    `json:"tile_x"`
		TileY int    `json:"tile_y"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &captResp); err != nil {
		t.Fatalf("failed to decode captcha response: %v", err)
	}

	if !captResp.OK || captResp.Key == "" || captResp.Image == "" || captResp.Tile == "" {
		t.Fatalf("invalid captcha response payload: %+v", captResp)
	}

	// 2. POST /api/captcha/check with incorrect coordinates
	checkForm := url.Values{
		"key":   {captResp.Key},
		"point": {"0,0"},
	}
	checkReq := httptest.NewRequest(http.MethodPost, "/api/captcha/check", strings.NewReader(checkForm.Encode()))
	checkReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	checkResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(checkResp, checkReq)

	if checkResp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for wrong coordinates, got %d", checkResp.Code)
	}

	// Retrieve real target coordinates from server.captcha
	targetX, targetY, ok := server.captcha.GetTargetCoordinatesForTest(captResp.Key)
	if !ok {
		t.Fatalf("target coordinates not found for key %s", captResp.Key)
	}

	// 2b. POST /api/captcha/check with correct coordinates
	checkValidForm := url.Values{
		"key":   {captResp.Key},
		"point": {fmt.Sprintf("%d,%d", targetX, targetY)},
	}
	checkValidReq := httptest.NewRequest(http.MethodPost, "/api/captcha/check", strings.NewReader(checkValidForm.Encode()))
	checkValidReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	checkValidResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(checkValidResp, checkValidReq)

	if checkValidResp.Code != http.StatusOK {
		t.Fatalf("expected 200 for correct coordinates, got %d: %s", checkValidResp.Code, checkValidResp.Body.String())
	}

	// 3. POST /api/messages with the validated token
	msgForm := url.Values{
		"name":          {"测试用户"},
		"title":         {"技术咨询"},
		"phone":         {"13800000000"},
		"content":       {"这是一条测试留言"},
		"captcha_token": {captResp.Key},
	}
	msgReq := httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader(msgForm.Encode()))
	msgReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	msgResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(msgResp, msgReq)

	if msgResp.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", msgResp.Code, msgResp.Body.String())
	}

	// 4. Repeated submission with consumed token should fail with 400
	msgReq2 := httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader(msgForm.Encode()))
	msgReq2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	msgResp2 := httptest.NewRecorder()
	server.Handler().ServeHTTP(msgResp2, msgReq2)

	if msgResp2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request on consumed token replay, got %d", msgResp2.Code)
	}
}
