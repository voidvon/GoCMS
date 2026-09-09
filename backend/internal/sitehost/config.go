package sitehost

import (
	"context"
	"database/sql"
	"net/url"
	"strings"
)

// FromDatabase reads the public site URL kept in the imported site settings.
// An empty result means absolute URLs should be treated as external.
func FromDatabase(ctx context.Context, database *sql.DB) string {
	if database == nil {
		return ""
	}
	var value string
	if err := database.QueryRowContext(ctx, `
		SELECT COALESCE("WebUrl", '')
		FROM "benming_ch_config" ORDER BY "id" LIMIT 1`).Scan(&value); err != nil {
		return ""
	}
	return FromURL(value)
}

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
