package site

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gocms/internal/db"
)

type notificationPage struct {
	Items  []db.AdminNotification `json:"items"`
	Unread int64                  `json:"unread"`
}

func decodeNotificationPage(t *testing.T, response *httptest.ResponseRecorder) notificationPage {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("GET notifications = %d, body %s", response.Code, response.Body.String())
	}
	var page notificationPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode notifications %s: %v", response.Body.String(), err)
	}
	return page
}

// waitForNotificationInbox polls until the condition holds, because the webhook
// push and its notification run in the background.
func waitForNotificationInbox(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if condition() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestMessageWebhookFailureRaisesAdminNotification(t *testing.T) {
	server, database, token := newCategoryTestServer(t)
	fake := newFakeFeishu(t)
	fake.reply(http.StatusOK, `{"code":19021,"msg":"sign match fail"}`)
	server.feishuClient = fake.client()

	response := categoryRequest(t, server, token, http.MethodPut, "/api/admin/messages/webhook",
		fmt.Sprintf(`{"enabled":true,"webhook_url":%q}`, testFeishuWebhookURL))
	if response.Code != http.StatusOK {
		t.Fatalf("enable webhook = %d, body %s", response.Code, response.Body.String())
	}

	firstID := submitPublicMessage(t, server, "第一条失败留言")
	if status, _ := waitForNotification(t, database, firstID); status != messageNotificationFailed {
		t.Fatalf("first message status = %q, want failed", status)
	}
	secondID := submitPublicMessage(t, server, "第二条失败留言")
	if status, _ := waitForNotification(t, database, secondID); status != messageNotificationFailed {
		t.Fatalf("second message status = %q, want failed", status)
	}

	// Both failures fold into one unread entry instead of flooding the inbox.
	waitForNotificationInbox(t, "two occurrences in one notification", func() bool {
		var occurrences int64
		if err := database.QueryRow(`SELECT "occurrences" FROM "` + db.AdminNotificationTable + `" WHERE "site_id" = 1`).Scan(&occurrences); err != nil {
			return false
		}
		return occurrences == 2
	})

	page := decodeNotificationPage(t, categoryRequest(t, server, token, http.MethodGet, "/api/admin/notifications", ""))
	if page.Unread != 1 || len(page.Items) != 1 {
		t.Fatalf("inbox = %+v, want a single unread notification", page)
	}
	item := page.Items[0]
	if item.Title != "留言飞书通知发送失败" || item.Level != db.AdminNotificationLevelError {
		t.Errorf("notification = %+v, want an error-titled entry", item)
	}
	if item.LinkView != "messages" {
		t.Errorf("notification link_view = %q, want messages", item.LinkView)
	}
	if !strings.Contains(item.Detail, "第二条失败留言") || !strings.Contains(item.Detail, "sign match fail") {
		t.Errorf("notification detail = %q, want the latest message and the Feishu reason", item.Detail)
	}
	if item.CreatedAt == "" || item.UpdatedAt == "" {
		t.Errorf("notification timestamps = %+v, want created_at and updated_at", item)
	}

	// Marking it read clears the badge.
	response = categoryRequest(t, server, token, http.MethodPost, "/api/admin/notifications", `{"all":true}`)
	if response.Code != http.StatusOK {
		t.Fatalf("mark read = %d, body %s", response.Code, response.Body.String())
	}
	page = decodeNotificationPage(t, categoryRequest(t, server, token, http.MethodGet, "/api/admin/notifications", ""))
	if page.Unread != 0 {
		t.Fatalf("unread after marking read = %d, want 0", page.Unread)
	}
	if len(page.Items) != 1 || page.Items[0].ReadAt == "" {
		t.Fatalf("inbox after marking read = %+v, want the entry kept and stamped", page.Items)
	}

	// A later failure is a fresh entry again, so it cannot be missed.
	thirdID := submitPublicMessage(t, server, "第三条失败留言")
	if status, _ := waitForNotification(t, database, thirdID); status != messageNotificationFailed {
		t.Fatalf("third message status = %q, want failed", status)
	}
	waitForNotificationInbox(t, "a new unread notification", func() bool {
		unread, err := db.CountUnreadAdminNotifications(context.Background(), database, 1)
		return err == nil && unread == 1
	})
	page = decodeNotificationPage(t, categoryRequest(t, server, token, http.MethodGet, "/api/admin/notifications", ""))
	if len(page.Items) != 2 || page.Items[0].Occurrences != 1 || page.Items[0].ReadAt != "" {
		t.Fatalf("inbox after the third failure = %+v, want a new unread entry first", page.Items)
	}
	if !strings.Contains(page.Items[0].Detail, "第三条失败留言") {
		t.Errorf("newest notification detail = %q, want the third message", page.Items[0].Detail)
	}
}

func TestAdminNotificationsScopeAndAccess(t *testing.T) {
	server, database, token := newCategoryTestServer(t)

	// Unauthenticated access is refused.
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/admin/notifications", nil),
		httptest.NewRequest(http.MethodPost, "/api/admin/notifications", strings.NewReader(`{"all":true}`)),
	} {
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401", request.Method, request.URL.Path, response.Code)
		}
	}

	// Another site's notifications stay invisible.
	if err := db.RecordAdminNotification(context.Background(), database, db.AdminNotification{
		SiteID: 2,
		Kind:   "message_webhook_failed",
		Level:  db.AdminNotificationLevelError,
		Title:  "其他站点的通知",
	}); err != nil {
		t.Fatal(err)
	}
	if page := decodeNotificationPage(t, categoryRequest(t, server, token, http.MethodGet, "/api/admin/notifications", "")); len(page.Items) != 0 || page.Unread != 0 {
		t.Fatalf("site 1 inbox = %+v, want nothing from site 2", page)
	}

	// An empty mark-read payload is rejected instead of silently doing nothing.
	response := categoryRequest(t, server, token, http.MethodPost, "/api/admin/notifications", `{}`)
	if response.Code != http.StatusBadRequest {
		t.Errorf("empty mark-read payload = %d, want 400 (body %s)", response.Code, response.Body.String())
	}

	// Marking a specific id only clears that entry.
	if err := db.RecordAdminNotification(context.Background(), database, db.AdminNotification{
		SiteID: 1, Kind: "kind-a", Title: "A", Level: db.AdminNotificationLevelWarning,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordAdminNotification(context.Background(), database, db.AdminNotification{
		SiteID: 1, Kind: "kind-b", Title: "B", Level: db.AdminNotificationLevelWarning,
	}); err != nil {
		t.Fatal(err)
	}
	page := decodeNotificationPage(t, categoryRequest(t, server, token, http.MethodGet, "/api/admin/notifications", ""))
	if len(page.Items) != 2 || page.Unread != 2 {
		t.Fatalf("inbox = %+v, want two unread entries", page)
	}
	response = categoryRequest(t, server, token, http.MethodPost, "/api/admin/notifications",
		fmt.Sprintf(`{"ids":[%d]}`, page.Items[0].ID))
	if response.Code != http.StatusOK {
		t.Fatalf("mark one read = %d, body %s", response.Code, response.Body.String())
	}
	page = decodeNotificationPage(t, categoryRequest(t, server, token, http.MethodGet, "/api/admin/notifications", ""))
	if page.Unread != 1 {
		t.Fatalf("unread after marking one = %d, want 1", page.Unread)
	}
}

func TestRecordAdminNotificationPrunesReadHistory(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.CreateSchema(context.Background(), database); err != nil {
		t.Fatal(err)
	}

	// Each read entry is followed by a new one, so the inbox grows past the
	// retention count and must drop the oldest read history.
	for index := 0; index < 60; index++ {
		if err := db.RecordAdminNotification(context.Background(), database, db.AdminNotification{
			SiteID: 1,
			Kind:   fmt.Sprintf("kind-%d", index),
			Title:  fmt.Sprintf("通知 %d", index),
		}); err != nil {
			t.Fatal(err)
		}
		if err := db.MarkAdminNotificationsRead(context.Background(), database, 1, nil); err != nil {
			t.Fatal(err)
		}
	}

	var total int
	if err := database.QueryRow(`SELECT COUNT(*) FROM "` + db.AdminNotificationTable + `"`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total > 51 {
		t.Errorf("stored notifications = %d, want the read history pruned", total)
	}

	items, unread, err := db.ListAdminNotifications(context.Background(), database, 1, 200)
	if err != nil {
		t.Fatal(err)
	}
	if unread != 0 || len(items) == 0 {
		t.Fatalf("inbox = %d items, unread %d, want read history kept", len(items), unread)
	}
	if items[0].Title != "通知 59" {
		t.Errorf("newest notification = %q, want 通知 59", items[0].Title)
	}
}
