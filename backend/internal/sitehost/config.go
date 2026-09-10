package sitehost

import (
	"net/url"
	"strings"
)

func FromURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err == nil && parsed.Hostname() != "" {
		return strings.ToLower(parsed.Hostname())
	}
	parsed, err = url.Parse("//" + value)
	if err == nil && parsed.Hostname() != "" {
		return strings.ToLower(parsed.Hostname())
	}
	return ""
}

func Matches(host, configuredHost string) bool {
	return configuredHost != "" && strings.EqualFold(strings.TrimSpace(host), configuredHost)
}
