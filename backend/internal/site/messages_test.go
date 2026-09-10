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
	if err := database.QueryRow(`SELECT "content_id" FROM "gocms_message" WHERE "title" = '咨询'`).Scan(&contentID); err != nil {
		t.Fatal(err)
	}
	if contentID != 185 {
		t.Fatalf("content association = %d", contentID)
	}
	var messageCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM "gocms_message"`).Scan(&messageCount); err != nil {
		t.Fatal(err)
	}
	if messageCount != 1 {
		t.Fatalf("message count = %d, want 1", messageCount)
	}
}
