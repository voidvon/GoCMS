package security

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gocms/internal/db"
	"gocms/internal/nginxguard"
)

type state struct {
	defaultSite int64
	platform    Platform
	policies    map[int64]Policy
	networks    map[networkScope]map[netip.Prefix]string
	hosts       map[string]int64
	snapshot    nginxguard.Snapshot
}
type networkScope struct {
	siteID int64
	kind   string
}
type attempt struct {
	times   []time.Time
	touched time.Time
}
type Service struct {
	database   *sql.DB
	mu         sync.Mutex
	loaded     time.Time
	state      *state
	attempts   map[string]attempt
	syncMu     sync.Mutex
	status     SyncStatus
	controller NginxController
}

type NginxController interface {
	Apply(context.Context, nginxguard.Config, nginxguard.Snapshot) (nginxguard.Result, error)
	Events(context.Context, nginxguard.Config, string) (nginxguard.LogBatch, error)
	Setup(context.Context, nginxguard.Config) nginxguard.SetupStatus
	Configuration(context.Context, nginxguard.Config) (nginxguard.ConfigFile, error)
}

func New(database *sql.DB, controllers ...NginxController) (*Service, error) {
	if err := Ensure(context.Background(), database); err != nil {
		return nil, err
	}
	s := &Service{database: database, attempts: map[string]attempt{}, controller: nginxguard.Client{}}
	if len(controllers) > 0 {
		s.controller = controllers[0]
	}
	var saved string
	if err := database.QueryRow(`SELECT value FROM gocms_security_state WHERE key='nginx_status'`).Scan(&saved); err == nil {
		if err := json.Unmarshal([]byte(saved), &s.status); err != nil {
			return nil, err
		}
	} else if err != sql.ErrNoRows {
		return nil, err
	}
	_, err := s.current(context.Background())
	return s, err
}
func (s *Service) Invalidate() { s.mu.Lock(); s.loaded = time.Time{}; s.mu.Unlock() }

func (s *Service) current(ctx context.Context) (*state, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != nil && time.Since(s.loaded) < 2*time.Second {
		return s.state, nil
	}
	v := &state{policies: map[int64]Policy{}, hosts: map[string]int64{}, networks: map[networkScope]map[netip.Prefix]string{}}
	var err error
	v.platform, err = LoadPlatform(ctx, s.database)
	if err != nil {
		return nil, err
	}
	sites, err := db.ListSites(ctx, s.database)
	if err != nil {
		return nil, err
	}
	var hasLanguages int
	if err := s.database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='gocms_language'`).Scan(&hasLanguages); err != nil {
		return nil, err
	}
	for _, site := range sites {
		if site.Status != "active" {
			continue
		}
		if site.IsDefault {
			v.defaultSite = site.ID
		}
		p, err := LoadPolicy(ctx, s.database, site.ID)
		if err != nil {
			return nil, err
		}
		v.policies[site.ID] = p
		t := nginxguard.Tenant{ID: site.ID, Hosts: []string{}, Paths: []string{}}
		for _, host := range append([]string{site.Domain}, site.Aliases...) {
			host = db.NormalizeDomain(host)
			if host != "" {
				t.Hosts = append(t.Hosts, host)
			}
		}
		if hasLanguages > 0 {
			rows, err := s.database.QueryContext(ctx, `SELECT domain FROM gocms_language WHERE site_id=? AND is_enabled=1 AND site_mode='standalone' AND domain<>''`, site.ID)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var host string
				if err = rows.Scan(&host); err != nil {
					rows.Close()
					return nil, err
				}
				if host = db.NormalizeDomain(host); host != "" {
					t.Hosts = append(t.Hosts, host)
				}
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, err
			}
		}
		sort.Strings(t.Hosts)
		unique := []string{}
		for _, h := range t.Hosts {
			if len(unique) > 0 && unique[len(unique)-1] == h {
				continue
			}
			if prev, ok := v.hosts[h]; ok && prev != site.ID {
				return nil, fmt.Errorf("安全防护域名重复：%s", h)
			}
			v.hosts[h] = site.ID
			unique = append(unique, h)
		}
		t.Hosts = unique
		t.Default = site.IsDefault
		if p.Enabled {
			t.Paths = p.Paths
		}
		v.snapshot.Tenants = append(v.snapshot.Tenants, t)
	}
	rows, err := s.database.QueryContext(ctx, `SELECT `+entryColumns+` FROM gocms_security_entry WHERE revoked_at='' AND (expires_at='' OR expires_at>?) ORDER BY site_id,kind,network`, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		if e.SiteID != 0 {
			if _, ok := v.policies[e.SiteID]; !ok {
				continue
			}
		}
		network, err := netip.ParsePrefix(e.Network)
		if err != nil {
			rows.Close()
			return nil, err
		}
		key := networkScope{e.SiteID, e.Kind}
		if v.networks[key] == nil {
			v.networks[key] = map[netip.Prefix]string{}
		}
		v.networks[key][network] = e.ExpiresAt
		v.snapshot.Rules = append(v.snapshot.Rules, nginxguard.Rule{SiteID: e.SiteID, Network: e.Network, Kind: e.Kind, ExpiresAt: e.ExpiresAt})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	s.state = v
	s.loaded = time.Now()
	return v, nil
}

func directAddr(remote string) netip.Addr {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip, _ := netip.ParseAddr(host)
	return ip.Unmap()
}
func trusted(ip netip.Addr, networks []string) bool {
	for _, n := range networks {
		p, err := netip.ParsePrefix(n)
		if err == nil && p.Contains(ip) {
			return true
		}
	}
	return false
}

// ResolveIP walks from the socket peer toward the client, trusting only the
// explicitly configured proxy networks. An invalid chain never supplies an IP.
func ResolveIP(r *http.Request, proxies []string) netip.Addr {
	ip := directAddr(r.RemoteAddr)
	if !ip.IsValid() || !trusted(ip, proxies) {
		return ip
	}
	value := r.Header.Get("X-Forwarded-For")
	if value == "" {
		return ip
	}
	parts := strings.Split(value, ",")
	if len(parts) > 32 {
		return ip
	}
	chain := make([]netip.Addr, len(parts))
	for i, p := range parts {
		parsed, err := netip.ParseAddr(strings.TrimSpace(p))
		if err != nil || parsed.Zone() != "" {
			return ip
		}
		chain[i] = parsed.Unmap()
	}
	for i := len(chain) - 1; i >= 0 && trusted(ip, proxies); i-- {
		ip = chain[i]
	}
	return ip
}

type ipKey struct{}

func ClientIP(r *http.Request) string {
	if ip, ok := r.Context().Value(ipKey{}).(netip.Addr); ok && ip.IsValid() {
		return ip.String()
	}
	return directAddr(r.RemoteAddr).String()
}

// RequestHost is shared with the public router, so proxy host selection cannot
// make enforcement inspect a different tenant than the application serves.
func RequestHost(r *http.Request) string {
	if r == nil {
		return ""
	}
	if value := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); value != "" {
		if comma := strings.Index(value, ","); comma >= 0 {
			return strings.TrimSpace(value[:comma])
		}
		return value
	}
	return r.Host
}

func decision(v *state, siteID int64, ip netip.Addr) string {
	now := time.Now().UTC().Format(time.RFC3339)
	for _, key := range []networkScope{{0, "allow"}, {0, "deny"}, {siteID, "allow"}, {siteID, "deny"}} {
		rules := v.networks[key]
		if len(rules) == 0 {
			continue
		}
		// Bounded by address width instead of the size of the blacklist.
		for bits := ip.BitLen(); bits >= 0; bits-- {
			if expires, ok := rules[netip.PrefixFrom(ip, bits).Masked()]; ok && (expires == "" || expires > now) {
				return key.kind
			}
		}
	}
	return ""
}

func (s *Service) Observe(ctx context.Context, host, pathValue string, ip netip.Addr) (bool, error) {
	return s.observe(ctx, 0, host, pathValue, ip)
}

func (s *Service) observe(ctx context.Context, siteHint int64, host, pathValue string, ip netip.Addr) (bool, error) {
	v, err := s.current(ctx)
	if err != nil {
		return false, err
	}
	normalHost := db.NormalizeDomain(host)
	siteID, matched := v.hosts[normalHost]
	if !matched {
		for _, tenant := range v.snapshot.Tenants {
			for _, pattern := range tenant.Hosts {
				if db.MatchHostPattern(pattern, normalHost) {
					siteID = tenant.ID
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
	}
	if !matched {
		siteID = v.defaultSite
		// Preserve the public router's tooling/development fallback only when
		// no configured host matched. It cannot override domain ownership.
		if _, ok := v.policies[siteHint]; ok {
			siteID = siteHint
		}
	}
	if !ip.IsValid() {
		return false, nil
	}
	if action := decision(v, siteID, ip); action != "" {
		return action == "deny", nil
	}
	p, ok := v.policies[siteID]
	if !ok || !p.Enabled || !Matches(p, pathValue) {
		return false, nil
	}
	// A missing loopback proxy configuration must not turn one scanner into
	// a ban of every visitor sharing the local reverse proxy connection.
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() {
		return true, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	key := fmt.Sprintf("%d:%s", siteID, ip)
	a := s.attempts[key]
	cutoff := now.Add(-time.Duration(p.WindowSeconds) * time.Second)
	fresh := a.times[:0]
	for _, t := range a.times {
		if t.After(cutoff) {
			fresh = append(fresh, t)
		}
	}
	a.times = append(fresh, now)
	a.touched = now
	// Bounded state; never evict a recently banned address into an allow window.
	if len(s.attempts) >= 20000 {
		for k, x := range s.attempts {
			if now.Sub(x.touched) > 24*time.Hour {
				delete(s.attempts, k)
			}
		}
		if len(s.attempts) >= 20000 && len(a.times) < p.Threshold {
			return true, nil
		}
	}
	s.attempts[key] = a
	if len(a.times) == 1 {
		if err := Record(ctx, s.database, Event{SiteID: siteID, IP: ip.String(), Host: host, Path: pathValue, Action: "probe", Reason: "命中扫描路径"}); err != nil {
			return true, err
		}
	}
	if len(a.times) >= p.Threshold {
		banSiteID := siteID
		if p.BanScope == "global" {
			banSiteID = 0
		}
		expiresAt := ""
		if p.BanSeconds > 0 {
			expiresAt = now.Add(time.Duration(p.BanSeconds) * time.Second).UTC().Format(time.RFC3339)
		}
		e, err := AddEntry(ctx, s.database, Entry{SiteID: banSiteID, Network: ip.String(), Kind: "deny", Reason: fmt.Sprintf("%d 秒内探测达到 %d 次", p.WindowSeconds, p.Threshold), Source: "automatic", ExpiresAt: expiresAt})
		if err != nil {
			return true, err
		}
		delete(s.attempts, key)
		s.loaded = time.Time{}
		if err := Record(ctx, s.database, Event{SiteID: e.SiteID, IP: ip.String(), Host: host, Path: pathValue, Action: "ban", Reason: e.Reason}); err != nil {
			return true, err
		}
	}
	// Explicit probe rules are rejected from their first hit; normal 404s do not count.
	return true, nil
}

func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, err := s.current(r.Context())
		if err != nil {
			http.Error(w, "安全防护暂时不可用", http.StatusServiceUnavailable)
			return
		}
		ip := ResolveIP(r, v.platform.TrustedProxies)
		r = r.WithContext(context.WithValue(r.Context(), ipKey{}, ip))
		selector := r.URL.Query().Get("site_id")
		if selector == "" {
			selector = r.Header.Get("X-Site-Id")
		}
		hint, _ := strconv.ParseInt(strings.TrimSpace(selector), 10, 64)
		blocked, err := s.observe(r.Context(), hint, RequestHost(r), r.URL.Path, ip)
		if err != nil {
			http.Error(w, "安全防护暂时不可用", http.StatusServiceUnavailable)
			return
		}
		if blocked {
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
