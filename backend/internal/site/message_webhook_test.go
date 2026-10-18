package site

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

const testFeishuWebhookURL = "https://open.feishu.cn/open-apis/bot/v2/hook/11111111-2222-3333-4444-555555555555"

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// fakeFeishu stands in for the Feishu custom-bot endpoint: it records every
// pushed payload and answers with a canned response.
type fakeFeishu struct {
	server *httptest.Server
	mu     sync.Mutex
	types  []string
	texts  []string
	status int
	body   string
}

func newFakeFeishu(t *testing.T) *fakeFeishu {
	t.Helper()
	fake := &fakeFeishu{status: http.StatusOK, body: `{"code":0,"msg":"success"}`}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var payload struct {
			MsgType string `json:"msg_type"`
			Content struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		_ = json.Unmarshal(raw, &payload)

		fake.mu.Lock()
		fake.types = append(fake.types, payload.MsgType)
		fake.texts = append(fake.texts, payload.Content.Text)
		status, body := fake.status, fake.body
		fake.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

// client rewrites the (strictly validated) Feishu host onto the local test
// server so the real request path is exercised end to end.
func (f *fakeFeishu) client() *http.Client {
	host := strings.TrimPrefix(f.server.URL, "http://")
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		request.URL.Scheme = "http"
		request.URL.Host = host
		return http.DefaultTransport.RoundTrip(request)
	})}
}

func (f *fakeFeishu) reply(status int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = status
	f.body = body
}

func (f *fakeFeishu) pushed() ([]string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.types...), append([]string(nil), f.texts...)
}

func decodeWebhookSettings(t *testing.T, response *httptest.ResponseRecorder) MessageWebhookSettings {
	t.Helper()
	var settings MessageWebhookSettings
	if err := json.Unmarshal(response.Body.Bytes(), &settings); err != nil {
		t.Fatalf("decode settings %s: %v", response.Body.String(), err)
	}
	return settings
}

func TestMessageWebhookSettingsLifecycle(t *testing.T) {
	server, _, token := newCategoryTestServer(t)

	response := categoryRequest(t, server, token, http.MethodGet, "/api/admin/messages/webhook", "")
	if response.Code != http.StatusOK {
		t.Fatalf("GET webhook settings = %d, body %s", response.Code, response.Body.String())
	}
	if settings := decodeWebhookSettings(t, response); settings.Enabled || settings.WebhookURL != "" {
		t.Fatalf("initial settings = %+v, want disabled and empty", settings)
	}

	// A non-Feishu address is refused.
	response = categoryRequest(t, server, token, http.MethodPut, "/api/admin/messages/webhook",
		`{"enabled":true,"webhook_url":"https://example.com/open-apis/bot/v2/hook/abc"}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid host = %d, want 400 (body %s)", response.Code, response.Body.String())
	}

	// Enabling without an address is refused.
	response = categoryRequest(t, server, token, http.MethodPut, "/api/admin/messages/webhook",
		`{"enabled":true,"webhook_url":""}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("enable without url = %d, want 400 (body %s)", response.Code, response.Body.String())
	}

	// A valid address is stored with its query and fragment stripped.
	response = categoryRequest(t, server, token, http.MethodPut, "/api/admin/messages/webhook",
		fmt.Sprintf(`{"enabled":true,"webhook_url":%q}`, testFeishuWebhookURL+"?sign=abc#frag"))
	if response.Code != http.StatusOK {
		t.Fatalf("save settings = %d, body %s", response.Code, response.Body.String())
	}
	response = categoryRequest(t, server, token, http.MethodGet, "/api/admin/messages/webhook", "")
	if settings := decodeWebhookSettings(t, response); !settings.Enabled || settings.WebhookURL != testFeishuWebhookURL {
		t.Fatalf("saved settings = %+v, want enabled with normalized url", settings)
	}

	// Disabling keeps the address so it can be switched back on later.
	response = categoryRequest(t, server, token, http.MethodPut, "/api/admin/messages/webhook", `{"enabled":false}`)
	if response.Code != http.StatusOK {
		t.Fatalf("disable settings = %d, body %s", response.Code, response.Body.String())
	}
	response = categoryRequest(t, server, token, http.MethodGet, "/api/admin/messages/webhook", "")
	if settings := decodeWebhookSettings(t, response); settings.Enabled || settings.WebhookURL != testFeishuWebhookURL {
		t.Fatalf("disabled settings = %+v, want disabled but address kept", settings)
	}

	// Clearing the address is allowed while notifications stay off.
	response = categoryRequest(t, server, token, http.MethodPut, "/api/admin/messages/webhook", `{"webhook_url":""}`)
	if response.Code != http.StatusOK {
		t.Fatalf("clear settings = %d, body %s", response.Code, response.Body.String())
	}
	response = categoryRequest(t, server, token, http.MethodGet, "/api/admin/messages/webhook", "")
	if settings := decodeWebhookSettings(t, response); settings.WebhookURL != "" {
		t.Fatalf("cleared settings = %+v, want empty url", settings)
	}
}

func TestMessageWebhookRequiresAdmin(t *testing.T) {
	server, _, _ := newCategoryTestServer(t)

	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/admin/messages/webhook", nil),
		httptest.NewRequest(http.MethodPut, "/api/admin/messages/webhook", strings.NewReader(`{"enabled":true}`)),
		httptest.NewRequest(http.MethodPost, "/api/admin/messages/webhook/test", strings.NewReader(`{}`)),
	} {
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401", request.Method, request.URL.Path, response.Code)
		}
	}
}

func TestMessageWebhookTestSendUsesFeishuPayload(t *testing.T) {
	server, _, token := newCategoryTestServer(t)
	fake := newFakeFeishu(t)
	server.feishuClient = fake.client()

	response := categoryRequest(t, server, token, http.MethodPost, "/api/admin/messages/webhook/test",
		fmt.Sprintf(`{"webhook_url":%q}`, testFeishuWebhookURL))
	if response.Code != http.StatusOK {
		t.Fatalf("test send = %d, body %s", response.Code, response.Body.String())
	}
	types, texts := fake.pushed()
	if len(types) != 1 || types[0] != "text" {
		t.Fatalf("pushed msg_type = %v, want [text]", types)
	}
	if !strings.Contains(texts[0], "测试成功") {
		t.Fatalf("test message = %q, want a success notice", texts[0])
	}

	// A webhook that answers HTTP 200 with a non-zero code is still a failure.
	fake.reply(http.StatusOK, `{"code":19021,"msg":"sign match fail"}`)
	response = categoryRequest(t, server, token, http.MethodPost, "/api/admin/messages/webhook/test",
		fmt.Sprintf(`{"webhook_url":%q}`, testFeishuWebhookURL))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("rejected test send = %d, want 400 (body %s)", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "sign match fail") {
		t.Fatalf("rejected test send body = %s, want the Feishu reason", response.Body.String())
	}

	// Transport level failures are reported too.
	fake.reply(http.StatusInternalServerError, `boom`)
	response = categoryRequest(t, server, token, http.MethodPost, "/api/admin/messages/webhook/test",
		fmt.Sprintf(`{"webhook_url":%q}`, testFeishuWebhookURL))
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "HTTP 500") {
		t.Fatalf("failing test send = %d body %s, want 400 mentioning HTTP 500", response.Code, response.Body.String())
	}
}

func TestFeedbackSubmitPushesFeishuNotification(t *testing.T) {
	server, database, token := newCategoryTestServer(t)
	fake := newFakeFeishu(t)
	server.feishuClient = fake.client()

	response := categoryRequest(t, server, token, http.MethodPut, "/api/admin/messages/webhook",
		fmt.Sprintf(`{"enabled":true,"webhook_url":%q}`, testFeishuWebhookURL))
	if response.Code != http.StatusOK {
		t.Fatalf("enable webhook = %d, body %s", response.Code, response.Body.String())
	}

	messageID := submitPublicMessage(t, server, "产品参数咨询")
	status, notificationError := waitForNotification(t, database, messageID)
	if status != messageNotificationSent {
		t.Fatalf("notification status = %q (%s), want sent", status, notificationError)
	}
	if notificationError != "" {
		t.Errorf("notification error = %q, want empty", notificationError)
	}

	var notifiedAt string
	if err := database.QueryRow(`SELECT "notified_at" FROM "gocms_message" WHERE "id" = ?`, messageID).Scan(&notifiedAt); err != nil {
		t.Fatal(err)
	}
	if notifiedAt == "" {
		t.Error("notified_at is empty after a successful push")
	}

	_, texts := fake.pushed()
	if len(texts) != 1 {
		t.Fatalf("pushed %d messages, want 1", len(texts))
	}
	for _, want := range []string{"产品参数咨询", "张三", "13800000000", "想了解产品参数", "默认反馈分类"} {
		if !strings.Contains(texts[0], want) {
			t.Errorf("notification text = %q, want it to contain %q", texts[0], want)
		}
	}

	// The admin list surfaces the outcome so a failed push stays visible.
	response = categoryRequest(t, server, token, http.MethodGet, "/api/admin/messages?page=1&page_size=10", "")
	if response.Code != http.StatusOK {
		t.Fatalf("GET messages = %d, body %s", response.Code, response.Body.String())
	}
	var page struct {
		Items []FeedbackItem `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("message list = %+v, want 1 item", page.Items)
	}
	if page.Items[0].NotificationStatus != messageNotificationSent || page.Items[0].NotifiedAt == "" {
		t.Errorf("listed message = %+v, want a sent notification with a timestamp", page.Items[0])
	}
}

func TestFeedbackSubmitNotificationStates(t *testing.T) {
	// Notifications switched off: the message is stored and marked disabled.
	server, database, _ := newCategoryTestServer(t)
	disabledID := submitPublicMessage(t, server, "未启用通知")
	if status, _ := waitForNotification(t, database, disabledID); status != messageNotificationDisabled {
		t.Errorf("status without settings = %q, want disabled", status)
	}

	// A rejected webhook records the failure instead of losing it.
	failing, failingDatabase, failingToken := newCategoryTestServer(t)
	fake := newFakeFeishu(t)
	fake.reply(http.StatusOK, `{"code":19021,"msg":"sign match fail"}`)
	failing.feishuClient = fake.client()
	response := categoryRequest(t, failing, failingToken, http.MethodPut, "/api/admin/messages/webhook",
		fmt.Sprintf(`{"enabled":true,"webhook_url":%q}`, testFeishuWebhookURL))
	if response.Code != http.StatusOK {
		t.Fatalf("enable webhook = %d, body %s", response.Code, response.Body.String())
	}

	failedID := submitPublicMessage(t, failing, "会被拒绝的留言")
	status, notificationError := waitForNotification(t, failingDatabase, failedID)
	if status != messageNotificationFailed {
		t.Fatalf("status after rejection = %q (%s), want failed", status, notificationError)
	}
	if !strings.Contains(notificationError, "sign match fail") {
		t.Errorf("notification error = %q, want the Feishu reason", notificationError)
	}
}

func submitPublicMessage(t *testing.T, server *Server, title string) int64 {
	t.Helper()
	form := url.Values{
		"title":   {title},
		"name":    {"张三"},
		"phone":   {"13800000000"},
		"content": {"想了解产品参数"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("submit message = %d, body %s", response.Code, response.Body.String())
	}

	var messageID int64
	if err := server.database.QueryRow(`SELECT "id" FROM "gocms_message" WHERE "title" = ? ORDER BY "id" DESC LIMIT 1`, title).Scan(&messageID); err != nil {
		t.Fatalf("load submitted message %q: %v", title, err)
	}
	return messageID
}

// waitForNotification polls until the background push has recorded an outcome.
func waitForNotification(t *testing.T, database *sql.DB, messageID int64) (string, string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var status, notificationError string
	for {
		if err := database.QueryRow(`SELECT "notification_status", "notification_error" FROM "gocms_message" WHERE "id" = ?`, messageID).
			Scan(&status, &notificationError); err != nil {
			t.Fatal(err)
		}
		if status != messageNotificationPending {
			return status, notificationError
		}
		if time.Now().After(deadline) {
			return status, notificationError
		}
		time.Sleep(20 * time.Millisecond)
	}
}
