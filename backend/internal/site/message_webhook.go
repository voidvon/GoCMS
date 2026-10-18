package site

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gocms/internal/db"
)

// Feishu (Lark) custom-bot webhook notifications for public messages.
//
// The webhook is configured per site through the gocms_site_setting key/value
// store, so every site can push its own messages into its own group chat.
const (
	messageWebhookURLSetting     = "message_webhook_url"
	messageWebhookEnabledSetting = "message_webhook_enabled"

	feishuWebhookPathPrefix = "/open-apis/bot/v2/hook/"
	messageWebhookTimeout   = 8 * time.Second
	messageWebhookMaxURLLen = 1000
	messageWebhookMaxBody   = 2000
)

// Notification states persisted on gocms_message.notification_status.
const (
	messageNotificationPending  = "pending"
	messageNotificationSent     = "sent"
	messageNotificationFailed   = "failed"
	messageNotificationDisabled = "disabled"
)

// feishuWebhookHosts accepts the China (Feishu) and international (Lark)
// webhook hosts; both serve the same custom-bot API.
var feishuWebhookHosts = map[string]bool{
	"open.feishu.cn":     true,
	"open.larksuite.com": true,
}

// MessageWebhookSettings is the per-site Feishu notification configuration.
type MessageWebhookSettings struct {
	Enabled    bool   `json:"enabled"`
	WebhookURL string `json:"webhook_url"`
}

// messageNotification carries the submitted message into the background push.
type messageNotification struct {
	SiteID    int64
	ClassID   int64
	Title     string
	Name      string
	Phone     string
	Email     string
	Content   string
	IP        string
	CreatedAt string
}

func (s *Server) loadMessageWebhookSettings(ctx context.Context, siteID int64) (MessageWebhookSettings, error) {
	settings, err := db.LoadSiteSettingsForSite(ctx, s.database, siteID)
	if err != nil {
		return MessageWebhookSettings{}, err
	}
	return MessageWebhookSettings{
		Enabled:    settings[messageWebhookEnabledSetting] == "1",
		WebhookURL: strings.TrimSpace(settings[messageWebhookURLSetting]),
	}, nil
}

func (s *Server) saveMessageWebhookSettings(ctx context.Context, siteID int64, settings MessageWebhookSettings) error {
	enabled := "0"
	if settings.Enabled {
		enabled = "1"
	}
	return db.SaveSiteSettingsForSite(ctx, s.database, siteID, map[string]string{
		messageWebhookURLSetting:     settings.WebhookURL,
		messageWebhookEnabledSetting: enabled,
	})
}

// normalizeFeishuWebhookURL validates a custom-bot webhook address and strips
// any query/fragment noise. An empty input stays empty so callers can decide
// whether clearing the setting is allowed.
func normalizeFeishuWebhookURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if len(trimmed) > messageWebhookMaxURLLen {
		return "", fmt.Errorf("飞书 Webhook 地址不能超过 %d 个字符", messageWebhookMaxURLLen)
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", errInvalidFeishuWebhook
	}
	if !strings.EqualFold(parsed.Scheme, "https") || !feishuWebhookHosts[strings.ToLower(parsed.Hostname())] ||
		!strings.HasPrefix(parsed.Path, feishuWebhookPathPrefix) || len(parsed.Path) <= len(feishuWebhookPathPrefix) {
		return "", errInvalidFeishuWebhook
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

var errInvalidFeishuWebhook = fmt.Errorf("请填写有效的飞书机器人 Webhook 地址，形如 https://open.feishu.cn%s<token>", feishuWebhookPathPrefix)

func (s *Server) webhookClient() *http.Client {
	if s.feishuClient != nil {
		return s.feishuClient
	}
	return &http.Client{Timeout: messageWebhookTimeout}
}

// sendFeishuText posts a plain-text message to a Feishu custom bot.
func (s *Server) sendFeishuText(ctx context.Context, webhookURL, text string) error {
	payload, err := json.Marshal(map[string]any{
		"msg_type": "text",
		"content":  map[string]string{"text": text},
	})
	if err != nil {
		return errors.New("飞书通知内容编码失败")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(payload))
	if err != nil {
		return errInvalidFeishuWebhook
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")

	response, err := s.webhookClient().Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return errors.New("飞书通知请求超时")
		}
		return fmt.Errorf("飞书通知请求失败：%s", err.Error())
	}
	defer response.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(response.Body, 8192))
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fmt.Errorf("飞书通知请求失败（HTTP %d）", response.StatusCode)
	}

	// Feishu answers {"code":0,"msg":"success"}; the older shape uses
	// {"StatusCode":0,"StatusMessage":"success"}. Both must be checked, since a
	// rejected webhook still returns HTTP 200.
	var result struct {
		Code          *int   `json:"code"`
		Message       string `json:"msg"`
		StatusCode    *int   `json:"StatusCode"`
		StatusMessage string `json:"StatusMessage"`
	}
	if len(bytes.TrimSpace(body)) > 0 {
		_ = json.Unmarshal(body, &result)
	}
	code := result.Code
	if code == nil {
		code = result.StatusCode
	}
	if code != nil && *code != 0 {
		reason := strings.TrimSpace(result.Message)
		if reason == "" {
			reason = strings.TrimSpace(result.StatusMessage)
		}
		if reason == "" {
			reason = fmt.Sprintf("code %d", *code)
		}
		return fmt.Errorf("飞书通知被拒绝：%s", truncateMessage(reason, 200))
	}
	return nil
}

func buildMessageNotificationText(siteName, className string, item messageNotification) string {
	lines := []string{"【新留言】" + truncateMessage(item.Title, 200)}
	if siteName != "" {
		lines = append(lines, "站点："+siteName)
	}
	if className != "" {
		lines = append(lines, "分类："+className)
	}
	lines = append(lines,
		"姓名："+fallbackDash(item.Name),
		"电话："+fallbackDash(item.Phone),
		"邮箱："+fallbackDash(item.Email),
	)
	lines = append(lines, "内容："+truncateMessage(item.Content, messageWebhookMaxBody))
	if item.IP != "" {
		lines = append(lines, "来源 IP："+item.IP)
	}
	if item.CreatedAt != "" {
		lines = append(lines, "提交时间："+item.CreatedAt)
	}
	return strings.Join(lines, "\n")
}

// notifyMessageCreated pushes a freshly stored message to the site's Feishu
// group and records the outcome on the message row. It runs in its own
// goroutine with a detached context: the visitor's submission must neither wait
// for nor fail because of the webhook.
func (s *Server) notifyMessageCreated(messageID int64, item messageNotification) {
	ctx, cancel := context.WithTimeout(context.Background(), messageWebhookTimeout+2*time.Second)
	defer cancel()

	settings, err := s.loadMessageWebhookSettings(ctx, item.SiteID)
	if err != nil {
		s.recordMessageNotification(ctx, messageID, messageNotificationFailed, "读取通知设置失败")
		return
	}
	if !settings.Enabled || settings.WebhookURL == "" {
		s.recordMessageNotification(ctx, messageID, messageNotificationDisabled, "")
		return
	}

	className := ""
	_ = s.database.QueryRowContext(ctx, `SELECT "name" FROM "`+db.FeedbackClassTable+`" WHERE "id" = ?`, item.ClassID).Scan(&className)
	siteName := ""
	_ = s.database.QueryRowContext(ctx, `SELECT "name" FROM "`+db.SiteTable+`" WHERE "id" = ?`, item.SiteID).Scan(&siteName)

	if err := s.sendFeishuText(ctx, settings.WebhookURL, buildMessageNotificationText(siteName, className, item)); err != nil {
		s.recordMessageNotification(ctx, messageID, messageNotificationFailed, truncateMessage(err.Error(), 500))
		return
	}
	s.recordMessageNotification(ctx, messageID, messageNotificationSent, "")
}

func (s *Server) recordMessageNotification(ctx context.Context, messageID int64, status, errorMessage string) {
	notifiedAt := ""
	if status == messageNotificationSent {
		notifiedAt = time.Now().Format("2006-01-02 15:04:05")
	}
	_, _ = s.database.ExecContext(ctx, `
		UPDATE "gocms_message"
		SET "notification_status" = ?, "notification_error" = ?, "notified_at" = ?
		WHERE "id" = ?`, status, errorMessage, notifiedAt, messageID)
}

// Admin endpoints

func (s *Server) adminMessageWebhook(response http.ResponseWriter, request *http.Request) {
	if !s.requireAdmin(response, request) {
		return
	}
	siteID, err := s.resolveSiteID(request, s.currentAdmin(request))
	if err != nil {
		writeJSON(response, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}

	switch request.Method {
	case http.MethodGet:
		settings, err := s.loadMessageWebhookSettings(request.Context(), siteID)
		if err != nil {
			writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "读取通知设置失败"})
			return
		}
		writeJSON(response, http.StatusOK, settings)

	case http.MethodPut, http.MethodPost:
		var payload struct {
			Enabled    *bool   `json:"enabled"`
			WebhookURL *string `json:"webhook_url"`
		}
		if err := decodeRequest(request, &payload); err != nil {
			writeJSON(response, http.StatusBadRequest, map[string]string{"error": "请求格式不正确"})
			return
		}
		current, err := s.loadMessageWebhookSettings(request.Context(), siteID)
		if err != nil {
			writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "读取通知设置失败"})
			return
		}
		next := current
		if payload.WebhookURL != nil {
			normalized, err := normalizeFeishuWebhookURL(*payload.WebhookURL)
			if err != nil {
				writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			next.WebhookURL = normalized
		}
		if payload.Enabled != nil {
			next.Enabled = *payload.Enabled
		}
		if next.Enabled && next.WebhookURL == "" {
			writeJSON(response, http.StatusBadRequest, map[string]string{"error": "启用飞书通知前请先填写 Webhook 地址"})
			return
		}
		if err := s.saveMessageWebhookSettings(request.Context(), siteID, next); err != nil {
			writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "保存通知设置失败"})
			return
		}
		writeJSON(response, http.StatusOK, next)

	default:
		methodNotAllowed(response)
	}
}

func (s *Server) adminMessageWebhookTest(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(response)
		return
	}
	if !s.requireAdmin(response, request) {
		return
	}
	siteID, err := s.resolveSiteID(request, s.currentAdmin(request))
	if err != nil {
		writeJSON(response, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}

	var payload struct {
		WebhookURL string `json:"webhook_url"`
	}
	if request.ContentLength != 0 {
		if err := decodeRequest(request, &payload); err != nil {
			writeJSON(response, http.StatusBadRequest, map[string]string{"error": "请求格式不正确"})
			return
		}
	}

	target := strings.TrimSpace(payload.WebhookURL)
	if target == "" {
		settings, err := s.loadMessageWebhookSettings(request.Context(), siteID)
		if err != nil {
			writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "读取通知设置失败"})
			return
		}
		target = settings.WebhookURL
	}
	normalized, err := normalizeFeishuWebhookURL(target)
	if err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if normalized == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "请先填写 Webhook 地址"})
		return
	}

	if err := s.sendFeishuText(request.Context(), normalized, "GoCMS 留言通知测试成功。"); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

func fallbackDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func firstNonEmptyText(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func truncateMessage(value string, maxLength int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= maxLength {
		return string(runes)
	}
	return string(runes[:maxLength]) + "..."
}
