// Package timestamps turns the wall clocks stored in SQLite into the
// unambiguous instants the admin API hands to the frontend.
//
// The admin UI renders every timestamp in the time zone of the viewer, so a
// bare "2006-01-02 15:04:05" string is not enough: the browser would read it as
// its own zone. Stored values are therefore converted to RFC3339 UTC at the API
// boundary, and the caller states which zone the stored value is in.
package timestamps

import (
	"strings"
	"time"
)

const (
	secondLayout = "2006-01-02 15:04:05"
	dayLayout    = "2006-01-02"
)

// UTC renders a timestamp stored in UTC, which is what the SQLite
// CURRENT_TIMESTAMP default writes.
func UTC(raw string) string { return At(raw, time.UTC) }

// Local renders a timestamp stored in the server's local zone, used by the
// legacy columns that were written with time.Now().Format.
func Local(raw string) string { return At(raw, time.Local) }

// At renders raw as an RFC3339 UTC instant, reading zone-less values in the
// given zone. Values that already carry a zone are normalized, values that
// cannot be parsed are returned untouched so a display never breaks.
func At(raw string, zone *time.Location) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if parsed, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return parsed.UTC().Format(time.RFC3339)
	}
	if zone == nil {
		zone = time.UTC
	}
	if parsed, err := time.ParseInLocation(secondLayout, trimmed, zone); err == nil {
		return parsed.UTC().Format(time.RFC3339)
	}
	if parsed, err := time.ParseInLocation(dayLayout, trimmed, zone); err == nil {
		return parsed.UTC().Format(time.RFC3339)
	}
	return trimmed
}
