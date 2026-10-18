package timestamps

import (
	"testing"
	"time"
)

func TestAtRendersStoredWallClockAsInstant(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"utc wall clock", "2026-10-18 12:33:24", "2026-10-18T12:33:24Z"},
		{"surrounding spaces", "  2026-10-18 12:33:24  ", "2026-10-18T12:33:24Z"},
		{"date only", "2026-10-18", "2026-10-18T00:00:00Z"},
		{"already rfc3339", "2026-10-18T12:33:24Z", "2026-10-18T12:33:24Z"},
		{"rfc3339 offset", "2026-10-18T20:33:24+08:00", "2026-10-18T12:33:24Z"},
		{"fractional seconds", "2026-10-18T12:33:24.512Z", "2026-10-18T12:33:24Z"},
		{"empty", "", ""},
		{"unparsable", "刚刚", "刚刚"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := UTC(testCase.raw); got != testCase.want {
				t.Fatalf("UTC(%q) = %q, want %q", testCase.raw, got, testCase.want)
			}
		})
	}
}

func TestAtReadsZoneLessWallClockInGivenZone(t *testing.T) {
	shanghai := time.FixedZone("CST", 8*60*60)
	if got := At("2026-10-18 20:33:24", shanghai); got != "2026-10-18T12:33:24Z" {
		t.Fatalf("At local wall clock = %q, want 2026-10-18T12:33:24Z", got)
	}
	if got := At("2026-10-18 20:33:24", nil); got != "2026-10-18T20:33:24Z" {
		t.Fatalf("At with nil zone = %q, want 2026-10-18T20:33:24Z", got)
	}
}
