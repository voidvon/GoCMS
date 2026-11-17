package site

import (
	"context"
	"sync"
	"time"
)

const dashboardStatsTTL = 20 * time.Second

type dashboardStatsKey struct {
	siteID int64
	window string
}

type dashboardStatsEntry struct {
	ready         chan struct{}
	complete      bool
	dayKey        string
	retentionDays int
	expires       time.Time
	summary       AccessLogSummary
}

// Only the dashboard's access aggregates are cached. Content/message counts
// and paginated lists remain fresh, and authorization runs before every lookup.
type dashboardStatsCache struct {
	mu      sync.Mutex
	entries map[dashboardStatsKey]*dashboardStatsEntry
}

func (s *Server) dashboardAccessSummary(ctx context.Context, siteID int64) (AccessLogSummary, error) {
	return s.dashboardAccessSummaryForWindow(ctx, siteID, "")
}

func (s *Server) dashboardAccessSummaryForWindow(ctx context.Context, siteID int64, window string) (AccessLogSummary, error) {
	key := dashboardStatsKey{siteID: siteID, window: window}
	return s.dashboardStats.get(ctx, key, s.accessLogs.dayKey(time.Now()), s.accessLogs.retention(), func() (AccessLogSummary, error) {
		return s.accessLogSummaryForWindow(ctx, siteID, false, window)
	})
}

func (c *dashboardStatsCache) get(ctx context.Context, key dashboardStatsKey, dayKey string, retentionDays int, load func() (AccessLogSummary, error)) (AccessLogSummary, error) {
	for {
		if err := ctx.Err(); err != nil {
			return AccessLogSummary{}, err
		}
		c.mu.Lock()
		if c.entries == nil {
			c.entries = make(map[dashboardStatsKey]*dashboardStatsEntry)
		}
		entry := c.entries[key]
		if entry != nil && entry.dayKey == dayKey && entry.retentionDays == retentionDays {
			if !entry.complete {
				ready := entry.ready
				c.mu.Unlock()
				select {
				case <-ctx.Done():
					return AccessLogSummary{}, ctx.Err()
				case <-ready:
					continue
				}
			}
			if time.Now().Before(entry.expires) {
				summary := entry.summary
				c.mu.Unlock()
				return summary, nil
			}
		}
		// Retired sites and expired entries must not accumulate indefinitely.
		for id, cached := range c.entries {
			if cached.complete && !time.Now().Before(cached.expires) {
				delete(c.entries, id)
			}
		}
		entry = &dashboardStatsEntry{ready: make(chan struct{}), dayKey: dayKey, retentionDays: retentionDays}
		c.entries[key] = entry
		c.mu.Unlock()

		summary, err := load()
		c.mu.Lock()
		current := c.entries[key] == entry
		if current {
			if err == nil {
				entry.summary = summary
				entry.expires = time.Now().Add(dashboardStatsTTL)
				entry.complete = true
			} else {
				delete(c.entries, key)
			}
		}
		close(entry.ready)
		c.mu.Unlock()
		if !current && err == nil {
			// A mutation invalidated this computation while it was running.
			// Retry instead of publishing or returning the stale snapshot.
			continue
		}
		return summary, err
	}
}

func (c *dashboardStatsCache) invalidate(siteID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if siteID == 0 {
		clear(c.entries)
	} else {
		for key := range c.entries {
			if key.siteID == siteID {
				delete(c.entries, key)
			}
		}
	}
}
