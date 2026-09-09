package site

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestMessagesUseUnifiedContentAssociation(t *testing.T) {
	server, database, _ := newCategoryTestServer(t)
	form := url.Values{
		"name":       {"访客"},
		"title":      {"咨询"},
		"phone":      {"021-12345678"},
		"content":    {"留言内容"},
		"content_id": {"185"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create message returned %d: %s", response.Code, response.Body.String())
	}

	var contentID int64
	if err := database.QueryRow(`SELECT "content_id" FROM "bilvie_message" WHERE "title" = '咨询'`).Scan(&contentID); err != nil {
		t.Fatal(err)
	}
	if contentID != 185 {
		t.Fatalf("content association = %d", contentID)
	}
	var legacyCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM "benming_ch_Msg"`).Scan(&legacyCount); err != nil {
		t.Fatal(err)
	}
	if legacyCount != 0 {
		t.Fatalf("runtime inserted into legacy message table: %d", legacyCount)
	}
}
