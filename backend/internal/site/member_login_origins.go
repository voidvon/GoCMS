package site

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"gocms/internal/db"
	"gocms/internal/memberoauth"
)

type memberLoginCallback struct {
	SiteID   int64  `json:"site_id"`
	SiteName string `json:"site_name"`
	Origin   string `json:"origin"`
	URL      string `json:"url"`
}

// Callback origins come only from managed site configuration, never from an
// arbitrary request Host or a public site_id selector. Wildcards are excluded.
func (s *Server) memberLoginCallbacks(ctx context.Context) ([]memberLoginCallback, error) {
	sites, err := db.ListSites(ctx, s.database)
	if err != nil {
		return nil, err
	}
	out := []memberLoginCallback{}
	seen := map[string]bool{}
	add := func(site db.Site, origin string) {
		c := memberoauth.Config{PublicURL: strings.ToLower(strings.TrimRight(strings.TrimSpace(origin), "/"))}
		if c.PublicURL == "" || memberoauth.Validate(c) != nil {
			return
		}
		u, _ := url.Parse(c.PublicURL)
		if strings.Contains(u.Host, "*") {
			return
		}
		// Domain ownership must resolve to this exact site, including language domains.
		matched, e := db.GetSiteByHost(ctx, s.database, u.Host)
		if e != nil || matched == nil || matched.ID != site.ID || seen[c.PublicURL] {
			return
		}
		seen[c.PublicURL] = true
		out = append(out, memberLoginCallback{SiteID: site.ID, SiteName: site.Name, Origin: c.PublicURL, URL: c.Callback()})
	}
	for _, site := range sites {
		if site.Status != "active" {
			continue
		}
		settings, e := db.LoadSiteSettingsForSite(ctx, s.database, site.ID)
		if e != nil {
			return nil, e
		}
		if configured := strings.TrimSpace(settings["site_url"]); configured != "" {
			add(site, configured)
		}
		for _, host := range append([]string{site.Domain}, site.Aliases...) {
			if host == "" || strings.Contains(host, "*") {
				continue
			}
			add(site, "https://"+host)
		}
		rows, e := s.database.QueryContext(ctx, `SELECT domain FROM gocms_language WHERE site_id=? AND site_mode='standalone' AND is_enabled=1 AND domain<>'' ORDER BY id`, site.ID)
		if e != nil {
			return nil, e
		}
		domains := []string{}
		for rows.Next() {
			var domain string
			if e = rows.Scan(&domain); e != nil {
				rows.Close()
				return nil, e
			}
			domains = append(domains, domain)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
		for _, host := range domains {
			if !strings.Contains(host, "*") {
				add(site, "https://"+host)
			}
		}
	}
	return out, nil
}
func (s *Server) googleConfigForOrigin(ctx context.Context, origin string) (memberoauth.Config, error) {
	callbacks, err := s.memberLoginCallbacks(ctx)
	if err != nil {
		return memberoauth.Config{}, err
	}
	for _, cb := range callbacks {
		if cb.Origin == origin {
			c, e := memberoauth.Load(ctx, s.database, cb.SiteID)
			c.PublicURL = cb.Origin
			return c, e
		}
	}
	return memberoauth.Config{}, errors.New("origin is not a configured callback")
}
