// Package consent provides site-scoped privacy policy configuration and the
// static-page integration used by every theme, without geolocation assumptions.
package consent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
)

const SettingKey = "consent_config"

type Override struct {
	Mode string            `json:"mode,omitempty"` // inherit, on, off
	Text map[string]string `json:"text,omitempty"`
}

type Config struct {
	Enabled    bool                `json:"enabled"`
	Version    string              `json:"version"`
	MaxAgeDays int                 `json:"max_age_days"`
	Purposes   []string            `json:"purposes"`
	Text       map[string]string   `json:"text,omitempty"`
	Languages  map[string]Override `json:"languages,omitempty"`
}

type Policy struct {
	Enabled    bool              `json:"enabled"`
	Version    string            `json:"version"`
	Revision   string            `json:"revision"`
	MaxAgeDays int               `json:"max_age_days"`
	Purposes   []string          `json:"purposes"`
	Text       map[string]string `json:"text"`
	SiteID     int64             `json:"site_id"`
	Language   string            `json:"language"`
}

var defaultText = map[string]string{
	"title":       "Your privacy choices",
	"description": "We use necessary cookies to provide this website. With your permission, we also use optional technologies for the purposes below. You can reject them or change your choices at any time.",
	"accept":      "Accept all", "reject": "Reject optional", "customize": "Customize", "save": "Save choices",
	"settings": "Privacy settings", "close": "Close", "necessary": "Strictly necessary (always active)",
	"preferences": "Preferences", "analytics": "Analytics", "marketing": "Marketing",
	"privacy_label": "Privacy policy", "cookie_label": "Cookie policy", "privacy_url": "", "cookie_url": "",
}

func Parse(raw string) (Config, error) {
	c := Config{Version: "1", MaxAgeDays: 180, Purposes: []string{"preferences", "analytics", "marketing"}}
	if strings.TrimSpace(raw) != "" {
		if strings.TrimSpace(raw) == "null" {
			return c, fmt.Errorf("consent_config must be an object")
		}
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&c); err != nil {
			return c, fmt.Errorf("invalid consent_config: %w", err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return c, fmt.Errorf("consent_config must contain one JSON object")
		}
	}
	if strings.TrimSpace(c.Version) == "" || len(c.Version) > 100 {
		return c, fmt.Errorf("consent version must contain 1–100 characters")
	}
	if c.MaxAgeDays < 1 || c.MaxAgeDays > 365 {
		return c, fmt.Errorf("consent max_age_days must be between 1 and 365")
	}
	seen := map[string]bool{}
	if c.Purposes == nil {
		c.Purposes = []string{}
	}
	for _, purpose := range c.Purposes {
		if !ValidPurpose(purpose) || seen[purpose] {
			return c, fmt.Errorf("invalid or duplicate consent purpose: %s", purpose)
		}
		seen[purpose] = true
	}
	if err := validateText(c.Text); err != nil {
		return c, err
	}
	for code, override := range c.Languages {
		if strings.TrimSpace(code) == "" {
			return c, fmt.Errorf("consent language code is required")
		}
		if override.Mode != "" && override.Mode != "inherit" && override.Mode != "on" && override.Mode != "off" {
			return c, fmt.Errorf("invalid consent mode for %s", code)
		}
		if err := validateText(override.Text); err != nil {
			return c, fmt.Errorf("%s: %w", code, err)
		}
	}
	return c, nil
}

func ValidPurpose(value string) bool {
	return value == "preferences" || value == "analytics" || value == "marketing"
}

func validateText(values map[string]string) error {
	for key, value := range values {
		if _, ok := defaultText[key]; !ok {
			return fmt.Errorf("unknown consent text: %s", key)
		}
		if len(value) > 8000 {
			return fmt.Errorf("consent text %s is too long", key)
		}
		if !strings.HasSuffix(key, "_url") || value == "" {
			continue
		}
		u, err := url.Parse(value)
		if err != nil || strings.ContainsAny(value, "\\\r\n\t") || strings.TrimSpace(value) != value ||
			!((u.Scheme == "https" || u.Scheme == "http") && u.Host != "" || u.Scheme == "" && u.Host == "" && strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//")) {
			return fmt.Errorf("consent %s must be an HTTP(S) URL or an absolute site path", key)
		}
	}
	return nil
}

func (c Config) Resolve(siteID int64, language string) Policy {
	if siteID <= 0 {
		siteID = 1
	}
	p := Policy{Enabled: c.Enabled, Version: c.Version, MaxAgeDays: c.MaxAgeDays, Purposes: c.Purposes, SiteID: siteID, Language: language, Text: map[string]string{}}
	for key, value := range defaultText {
		p.Text[key] = value
	}
	// Defaults are localization only. A language never implies a legal regime.
	if strings.EqualFold(language, "zh-CN") || strings.EqualFold(language, "zh") {
		for key, value := range map[string]string{
			"title": "您的隐私选择", "description": "我们使用必要 Cookie 提供网站服务。经您同意后，才会为以下用途使用可选技术。您可以拒绝，并随时修改或撤回选择。",
			"accept": "全部接受", "reject": "拒绝非必要项", "customize": "自定义设置", "save": "保存选择", "settings": "隐私设置", "close": "关闭",
			"necessary": "严格必要（始终启用）", "preferences": "偏好设置", "analytics": "访问分析", "marketing": "营销推广", "privacy_label": "隐私政策", "cookie_label": "Cookie 政策",
		} {
			p.Text[key] = value
		}
	}
	for key, value := range c.Text {
		if value != "" {
			p.Text[key] = value
		}
	}
	if override, ok := c.Languages[language]; ok {
		switch override.Mode {
		case "on":
			p.Enabled = true
		case "off":
			p.Enabled = false
		}
		for key, value := range override.Text {
			if value != "" {
				p.Text[key] = value
			}
		}
	}
	// Text translations share consent on one origin. Changes to purposes,
	// retention or the operator's policy version require a fresh choice.
	data, _ := json.Marshal([]any{c.Version, c.Purposes, c.MaxAgeDays})
	hash := sha256.Sum256(data)
	p.Revision = hex.EncodeToString(hash[:16])
	return p
}
