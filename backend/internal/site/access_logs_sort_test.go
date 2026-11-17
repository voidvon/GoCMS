package site

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestAccessLogErrorHitSortAcrossPages(t *testing.T) {
	server, database, token, _ := newSiteAdminTestServer(t)
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := database.Exec(`INSERT INTO gocms_site (id, name, code) VALUES (2, 'Other', 'other')`); err != nil {
		t.Fatal(err)
	}
	insert := func(siteID int, host, path string, status, hits int, at time.Time) {
		t.Helper()
		for range hits {
			if _, err := database.Exec(`INSERT INTO gocms_access_log
				(site_id, host, day_key, path, status, kind, visited_at)
				VALUES (?, ?, ?, ?, ?, 'error', ?)`, siteID, host, at.Format("2006-01-02"), path, status, at.Format("2006-01-02 15:04:05")); err != nil {
				t.Fatal(err)
			}
		}
	}
	insert(1, "a.test", "/old-top", 404, 30, now.Add(-2*time.Hour))
	insert(1, "a.test", "/second", 404, 12, now.Add(-time.Hour))
	// Equal counts and timestamps across all group keys must paginate without
	// duplicates or gaps. Deliberately insert these in reverse key order.
	insert(1, "b.test", "/a-tie", 404, 4, now.Add(-time.Hour))
	insert(1, "a.test", "/b-tie", 404, 4, now.Add(-time.Hour))
	insert(1, "a.test", "/a-tie", 500, 4, now.Add(-time.Hour))
	insert(1, "a.test", "/a-tie", 404, 4, now.Add(-time.Hour))
	insert(1, "a.test", "/new-low", 404, 1, now)
	var low []string
	for index := range 25 {
		path := fmt.Sprintf("/low-%02d", index)
		insert(1, "a.test", path, 404, 1, now.Add(-10*time.Minute))
		low = append(low, "a.test"+path+":404:1")
	}
	// Higher counts outside this site's selected window, or explicitly ignored,
	// must never become the top result.
	insert(1, "a.test", "/outside", 404, 50, now.Add(-48*time.Hour))
	insert(2, "a.test", "/other-site", 404, 50, now)
	insert(1, "a.test", "/ignored", 404, 50, now)
	if _, err := server.addAccessLogIgnore(t.Context(), 1, "/ignored"); err != nil {
		t.Fatal(err)
	}
	ties := []string{"a.test/a-tie:404:4", "a.test/a-tie:500:4", "a.test/b-tie:404:4", "b.test/a-tie:404:4"}
	lows := append([]string{"a.test/new-low:404:1"}, low...)
	descending := append([]string{"a.test/old-top:404:30", "a.test/second:404:12"}, ties...)
	descending = append(descending, lows...)
	ascending := append(append([]string{}, lows...), ties...)
	ascending = append(ascending, "a.test/second:404:12", "a.test/old-top:404:30")
	recent := append(append([]string{}, lows...), "a.test/second:404:12")
	recent = append(append(recent, ties...), "a.test/old-top:404:30")
	for _, tc := range []struct {
		sort string
		want []string
	}{
		{"recent", recent},
		{"hits", descending},
		{"hits_asc", ascending},
		{"", descending},
		{"unsupported", descending},
	} {
		t.Run("sort="+tc.sort, func(t *testing.T) {
			var got []string
			for offset := 0; offset < len(tc.want); offset += 7 {
				path := fmt.Sprintf("/api/admin/access-logs/errors?window=24h&sort=%s&limit=7&offset=%d", tc.sort, offset)
				response := siteAdminRequest(server, token, http.MethodGet, path, "1", "")
				if response.Code != http.StatusOK {
					t.Fatalf("GET %s = %d: %s", path, response.Code, response.Body.String())
				}
				var result struct {
					Items  []AccessLogErrorStat
					Total  int
					Offset int
					Limit  int
				}
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				wantSize := min(7, len(tc.want)-offset)
				if result.Total != len(tc.want) || result.Offset != offset || result.Limit != 7 || len(result.Items) != wantSize {
					t.Fatalf("unexpected pagination: %+v", result)
				}
				for _, item := range result.Items {
					got = append(got, fmt.Sprintf("%s%s:%d:%d", item.Host, item.Path, item.Status, item.Hits))
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("sort across pages = %v, want %v", got, tc.want)
			}
		})
	}
}
