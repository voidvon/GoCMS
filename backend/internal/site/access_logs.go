package site

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"gocms/internal/db"
)

// Public access statistics. Every row is bound to a site, so the dashboard and
// the access log viewer can only ever read the rows of the active site.
//
// Successful static asset requests are ignored on purpose (a single page view
// pulls dozens of files); errors are always kept, including asset errors such
// as a missing stylesheet or image, because those are exactly the problems the
// dashboard is meant to surface.
const (
	defaultAccessLogRetentionDays = 3
	accessLogCleanupInterval      = 30 * time.Minute
	accessLogSiteCacheTTL         = 5 * time.Minute
	accessLogSiteCacheLimit       = 64
	accessLogQueryLimit           = 200
	accessLogDefaultPageSize      = 30
	accessLogMaxPageSize          = 200
	accessLogTopPagesLimit        = 10
	accessLogTopErrorsLimit       = 30
)

// accessLogCacheEntry caches host → site resolution plus the domains that
// belong to that site (used to tell internal from external referers).
type accessLogCacheEntry struct {
	siteID  int64
	domains map[string]bool
}

type accessLogConfig struct {
	location      *time.Location
	retentionDays int
	mu            sync.RWMutex
	refreshedAt   time.Time
	hosts         map[string]accessLogCacheEntry
}

func newAccessLogConfig() *accessLogConfig {
	return &accessLogConfig{
		location:      time.Local,
		retentionDays: defaultAccessLogRetentionDays,
		hosts:         map[string]accessLogCacheEntry{},
	}
}

// ConfigureAccessLogs sets the day boundary location and the retention window
// used by the public access log.
func (s *Server) ConfigureAccessLogs(location *time.Location, retentionDays int) {
	if s == nil || s.accessLogs == nil {
		return
	}
	if location == nil {
		location = time.Local
	}
	if retentionDays < 1 {
		retentionDays = defaultAccessLogRetentionDays
	}
	s.accessLogs.mu.Lock()
	defer s.accessLogs.mu.Unlock()
	s.accessLogs.location = location
	s.accessLogs.retentionDays = retentionDays
}

func (c *accessLogConfig) retention() int {
	if c == nil {
		return defaultAccessLogRetentionDays
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.retentionDays < 1 {
		return defaultAccessLogRetentionDays
	}
	return c.retentionDays
}

func (c *accessLogConfig) loc() *time.Location {
	if c == nil {
		return time.Local
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.location == nil {
		return time.Local
	}
	return c.location
}

func (c *accessLogConfig) dayKey(t time.Time) string {
	return t.In(c.loc()).Format("2006-01-02")
}

// formatVisited converts the stored UTC timestamp into the configured zone so
// the dashboard shows operator-local times.
func (c *accessLogConfig) formatVisited(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", trimmed, time.UTC)
	if err != nil {
		return trimmed
	}
	return parsed.In(c.loc()).Format("2006-01-02 15:04:05")
}

// accessResponse captures the status code and content type of a public
// response. It is a pure pass-through wrapper: it never mutates headers so
// http.ServeContent / http.FileServer keep working unchanged.
type accessResponse struct {
	http.ResponseWriter
	status      int
	contentType string
}

func (w *accessResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *accessResponse) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.contentType = w.Header().Get("Content-Type")
	w.ResponseWriter.WriteHeader(status)
}

func (w *accessResponse) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (s *Server) accessLogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		capture := &accessResponse{ResponseWriter: response}
		defer s.recordAccessLog(request, capture)
		next.ServeHTTP(capture, request)
	})
}

func (s *Server) recordAccessLog(request *http.Request, result *accessResponse) {
	if s == nil || s.database == nil || s.accessLogs == nil || request == nil || result == nil {
		return
	}
	if request.Method != http.MethodGet {
		return
	}
	path := request.URL.Path
	if path == "" {
		path = "/"
	}
	lowerPath := strings.ToLower(path)
	if strings.HasPrefix(lowerPath, "/api/") || lowerPath == "/admin" || strings.HasPrefix(lowerPath, "/admin/") {
		return
	}
	status := result.status
	if status == 0 {
		status = http.StatusOK
	}
	contentType := strings.TrimSpace(result.contentType)
	isPage := strings.HasPrefix(strings.ToLower(contentType), "text/html")
	kind := "page"
	switch {
	case status >= http.StatusBadRequest:
		// Broken URLs, including missing assets, are always kept.
		kind = "error"
	case status >= http.StatusMultipleChoices:
		// Redirects are not page views.
		return
	case !isPage:
		// Successful static assets are noise; only HTML pages are counted.
		return
	}
	userAgent := strings.TrimSpace(request.UserAgent())
	agent := summarizeUserAgent(userAgent)
	referer := strings.TrimSpace(request.Referer())
	refererHost := refererHostName(referer)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	siteID, domains := s.accessLogSite(ctx, request)
	if siteID <= 0 {
		return
	}
	refererKind := "none"
	if refererHost != "" {
		// A referer from the very host being served is always internal, even
		// when that host is not (yet) recorded as a domain of the site.
		if refererHost == normalizeAccessLogHost(requestHost(request)) || domains[refererHost] {
			refererKind = "internal"
		} else {
			refererKind = "external"
		}
	}
	query := request.URL.RawQuery
	if len(query) > accessLogQueryLimit {
		query = query[:accessLogQueryLimit]
	}
	now := time.Now()
	_, err := s.database.ExecContext(ctx, `INSERT INTO "`+db.AccessLogTable+`"
		("site_id", "day_key", "path", "query", "method", "status", "kind", "content_type", "ip",
		 "referer", "referer_host", "referer_kind", "user_agent", "agent_kind", "agent_label", "device_kind", "visited_at")
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		siteID, s.accessLogs.dayKey(now), path, query, request.Method, status, kind, contentType, clientIP(request),
		referer, refererHost, refererKind, userAgent, agent.Kind, agent.Label, agent.Device, now.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		log.Printf("record access log: %v", err)
	}
}

// accessLogSite resolves the public site for a request and returns the set of
// hosts that belong to it. Results are cached briefly so that logging costs at
// most one lookup per host per cache window.
func (s *Server) accessLogSite(ctx context.Context, request *http.Request) (int64, map[string]bool) {
	host := normalizeAccessLogHost(requestHost(request))
	cache := s.accessLogs
	if cache == nil {
		return 0, nil
	}
	if host != "" {
		cache.mu.RLock()
		entry, ok := cache.hosts[host]
		fresh := time.Since(cache.refreshedAt) < accessLogSiteCacheTTL
		cache.mu.RUnlock()
		if ok && fresh {
			return entry.siteID, entry.domains
		}
	}

	site, err := db.GetSiteByHost(ctx, s.database, host)
	if err != nil || site == nil {
		site, err = db.GetDefaultSite(ctx, s.database)
	}
	if err != nil || site == nil {
		return 0, nil
	}
	domains := map[string]bool{}
	if normalized := normalizeAccessLogHost(site.Domain); normalized != "" {
		domains[normalized] = true
	}
	for _, alias := range site.Aliases {
		if normalized := normalizeAccessLogHost(alias); normalized != "" {
			domains[normalized] = true
		}
	}
	// Standalone language domains are bound to the same site and must count as
	// internal referers as well.
	rows, queryErr := s.database.QueryContext(ctx, `SELECT COALESCE("domain", '') FROM "`+db.LanguageTable+`" WHERE "site_id" = ?`, site.ID)
	if queryErr == nil {
		for rows.Next() {
			var domain string
			if scanErr := rows.Scan(&domain); scanErr == nil {
				if normalized := normalizeAccessLogHost(domain); normalized != "" {
					domains[normalized] = true
				}
			}
		}
		_ = rows.Close()
	}
	if host == "" {
		host = normalizeAccessLogHost(site.Domain)
	}
	if host != "" {
		cache.mu.Lock()
		if cache.hosts == nil || time.Since(cache.refreshedAt) >= accessLogSiteCacheTTL || len(cache.hosts) >= accessLogSiteCacheLimit {
			cache.hosts = map[string]accessLogCacheEntry{}
			cache.refreshedAt = time.Now()
		}
		cache.hosts[host] = accessLogCacheEntry{siteID: site.ID, domains: domains}
		cache.mu.Unlock()
	}
	return site.ID, domains
}

func normalizeAccessLogHost(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, "://") {
		if parsed, err := url.Parse(trimmed); err == nil {
			trimmed = parsed.Host
		}
	}
	if index := strings.Index(trimmed, "/"); index >= 0 {
		trimmed = trimmed[:index]
	}
	trimmed = strings.TrimSuffix(trimmed, ".")
	if host, port, err := splitHostPortLoose(trimmed); err {
		trimmed = host
		_ = port
	}
	if strings.HasPrefix(trimmed, "www.") {
		trimmed = strings.TrimPrefix(trimmed, "www.")
	}
	return trimmed
}

func splitHostPortLoose(value string) (string, string, bool) {
	index := strings.LastIndex(value, ":")
	if index < 0 {
		return value, "", false
	}
	port := value[index+1:]
	if port == "" {
		return value, "", false
	}
	for _, digit := range port {
		if digit < '0' || digit > '9' {
			return value, "", false
		}
	}
	return value[:index], port, true
}

func refererHostName(referer string) string {
	if strings.TrimSpace(referer) == "" {
		return ""
	}
	parsed, err := url.Parse(referer)
	if err != nil {
		return ""
	}
	return normalizeAccessLogHost(parsed.Host)
}

// userAgentInfo is a coarse, provider-independent classification used for
// filtering; it deliberately avoids site specific rules.
type userAgentInfo struct {
	Kind   string
	Label  string
	Device string
}

var accessLogAgentPatterns = []struct {
	match string
	kind  string
	label string
}{
	{"googlebot", "bot", "Googlebot"},
	{"google-inspectiontool", "bot", "Google Inspection"},
	{"googleother", "bot", "GoogleOther"},
	{"storebot-google", "bot", "Google StoreBot"},
	{"apis-google", "bot", "Google APIs"},
	{"mediapartners-google", "bot", "Google AdSense"},
	{"adsbot-google", "bot", "Google AdsBot"},
	{"bingbot", "bot", "Bingbot"},
	{"bingpreview", "bot", "Bing Preview"},
	{"msnbot", "bot", "MSN Bot"},
	{"duckduckbot", "bot", "DuckDuckBot"},
	{"baiduspider", "bot", "Baiduspider"},
	{"sogou", "bot", "Sogou Spider"},
	{"360spider", "bot", "360Spider"},
	{"haosouspider", "bot", "Haosou Spider"},
	{"bytespider", "bot", "ByteSpider"},
	{"petalbot", "bot", "PetalBot"},
	{"yandex", "bot", "Yandex Bot"},
	{"applebot", "bot", "Applebot"},
	{"ahrefsbot", "bot", "AhrefsBot"},
	{"semrushbot", "bot", "SemrushBot"},
	{"mj12bot", "bot", "MJ12bot"},
	{"dotbot", "bot", "DotBot"},
	{"dataforseobot", "bot", "DataForSeo"},
	{"serpstatbot", "bot", "SerpstatBot"},
	{"gptbot", "bot", "GPTBot"},
	{"chatgpt-user", "bot", "ChatGPT-User"},
	{"claudebot", "bot", "ClaudeBot"},
	{"anthropic-ai", "bot", "Anthropic"},
	{"ccbot", "bot", "CCBot"},
	{"perplexitybot", "bot", "PerplexityBot"},
	{"amazonbot", "bot", "Amazonbot"},
	{"facebookexternalhit", "bot", "Facebook"},
	{"facebookcatalog", "bot", "Facebook Catalog"},
	{"twitterbot", "bot", "Twitterbot"},
	{"linkedinbot", "bot", "LinkedInBot"},
	{"telegrambot", "bot", "TelegramBot"},
	{"whatsapp", "bot", "WhatsApp"},
	{"slackbot", "bot", "Slackbot"},
	{"discordbot", "bot", "Discordbot"},
	{"embedly", "bot", "Embedly"},
	{"pinterest", "bot", "Pinterest"},
	{"uptimerobot", "bot", "UptimeRobot"},
	{"pingdom", "bot", "Pingdom"},
	{"statuscake", "bot", "StatusCake"},
	{"site24x7", "bot", "Site24x7"},
	{"newrelicpinger", "bot", "New Relic"},
	{"nagios", "bot", "Nagios"},
	{"zabbix", "bot", "Zabbix"},
	{"check_http", "bot", "Monitoring"},
	{"headlesschrome", "tool", "Headless Chrome"},
	{"phantomjs", "tool", "PhantomJS"},
	{"puppeteer", "tool", "Puppeteer"},
	{"playwright", "tool", "Playwright"},
	{"lighthouse", "tool", "Lighthouse"},
	{"pagespeed", "tool", "PageSpeed"},
	{"curl/", "tool", "curl"},
	{"wget", "tool", "wget"},
	{"httpie", "tool", "HTTPie"},
	{"python-requests", "tool", "Python Requests"},
	{"python-urllib", "tool", "Python urllib"},
	{"aiohttp", "tool", "aiohttp"},
	{"httpx", "tool", "httpx"},
	{"go-http-client", "tool", "Go HTTP Client"},
	{"okhttp", "tool", "OkHttp"},
	{"java/", "tool", "Java Client"},
	{"libwww-perl", "tool", "libwww-perl"},
	{"node-fetch", "tool", "node-fetch"},
	{"undici", "tool", "undici"},
	{"axios", "tool", "axios"},
	{"postmanruntime", "tool", "Postman"},
	{"insomnia", "tool", "Insomnia"},
	{"googlesiteverification", "bot", "Site Verification"},
}

func summarizeUserAgent(userAgent string) userAgentInfo {
	lower := strings.ToLower(strings.TrimSpace(userAgent))
	if lower == "" {
		return userAgentInfo{Kind: "other", Label: "无 User-Agent", Device: "other"}
	}
	for _, pattern := range accessLogAgentPatterns {
		if strings.Contains(lower, pattern.match) {
			return userAgentInfo{Kind: pattern.kind, Label: pattern.label, Device: "other"}
		}
	}
	for _, keyword := range []string{"bot", "spider", "crawler", "crawl", "slurp", "scrapy", "monitoring"} {
		if strings.Contains(lower, keyword) {
			return userAgentInfo{Kind: "bot", Label: "Unknown Bot", Device: "other"}
		}
	}
	if label := browserLabel(lower); label != "" {
		return userAgentInfo{Kind: "browser", Label: label, Device: deviceKindForAgent(lower)}
	}
	return userAgentInfo{Kind: "other", Label: "未知客户端", Device: deviceKindForAgent(lower)}
}

func browserLabel(lower string) string {
	switch {
	case strings.Contains(lower, "edg/"):
		return "Edge " + versionAfter(lower, "edg/")
	case strings.Contains(lower, "opr/"), strings.Contains(lower, "opera"):
		return "Opera"
	case strings.Contains(lower, "firefox/"):
		return "Firefox " + versionAfter(lower, "firefox/")
	case strings.Contains(lower, "chrome/"):
		return "Chrome " + versionAfter(lower, "chrome/")
	case strings.Contains(lower, "safari/"):
		return "Safari"
	case strings.Contains(lower, "msie"), strings.Contains(lower, "trident"):
		return "Internet Explorer"
	case strings.Contains(lower, "mozilla/"):
		return "浏览器"
	}
	return ""
}

func versionAfter(lower, token string) string {
	index := strings.Index(lower, token)
	if index < 0 {
		return ""
	}
	rest := lower[index+len(token):]
	major := rest
	if dot := strings.IndexAny(rest, " .;)"); dot >= 0 {
		major = rest[:dot]
	}
	return major
}

func deviceKindForAgent(lower string) string {
	switch {
	case strings.Contains(lower, "ipad"), strings.Contains(lower, "tablet"),
		strings.Contains(lower, "kindle"), strings.Contains(lower, "silk/"),
		strings.Contains(lower, "android") && !strings.Contains(lower, "mobile"):
		return "tablet"
	case strings.Contains(lower, "mobi"), strings.Contains(lower, "iphone"), strings.Contains(lower, "ipod"),
		strings.Contains(lower, "windows phone"), strings.Contains(lower, "blackberry"), strings.Contains(lower, "opera mini"):
		return "mobile"
	case strings.Contains(lower, "windows nt"), strings.Contains(lower, "macintosh"), strings.Contains(lower, "mac os x"),
		strings.Contains(lower, "x11"), strings.Contains(lower, "linux"), strings.Contains(lower, "cros"):
		return "desktop"
	}
	return "other"
}

// AccessLogItem is one public request row.
type AccessLogItem struct {
	ID          int64  `json:"id"`
	SiteID      int64  `json:"site_id"`
	DayKey      string `json:"day_key"`
	Path        string `json:"path"`
	Query       string `json:"query"`
	Method      string `json:"method"`
	Status      int    `json:"status"`
	Kind        string `json:"kind"`
	ContentType string `json:"content_type"`
	IP          string `json:"ip"`
	Referer     string `json:"referer"`
	RefererHost string `json:"referer_host"`
	RefererKind string `json:"referer_kind"`
	UserAgent   string `json:"user_agent"`
	AgentKind   string `json:"agent_kind"`
	AgentLabel  string `json:"agent_label"`
	DeviceKind  string `json:"device_kind"`
	VisitedAt   string `json:"visited_at"`
}

// AccessLogMetrics is the dashboard headline block for one site.
type AccessLogMetrics struct {
	TodayVisits       int64  `json:"today_visits"`
	TodayErrors       int64  `json:"today_errors"`
	RecentVisits      int64  `json:"recent_visits"`
	RecentErrors      int64  `json:"recent_errors"`
	RecentUsers       int64  `json:"recent_users"`
	RecentNewUsers    int64  `json:"recent_new_users"`
	RecentPCUsers     int64  `json:"recent_pc_users"`
	RecentMobileUsers int64  `json:"recent_mobile_users"`
	TotalErrors       int64  `json:"total_errors"`
	TotalNotFound     int64  `json:"total_404_errors"`
	RecentNotFound    int64  `json:"recent_404_errors"`
	InternalNotFound  int64  `json:"internal_404_errors"`
	TotalPages        int64  `json:"total_pages"`
	TotalRequests     int64  `json:"total_requests"`
	RetentionDays     int    `json:"retention_days"`
	DayKey            string `json:"day_key"`
}

// AccessLogPageStat is one row of the popular page ranking.
type AccessLogPageStat struct {
	Path          string `json:"path"`
	Visits        int64  `json:"visits"`
	UniqueIPs     int64  `json:"unique_ips"`
	LastVisitedAt string `json:"last_visited_at"`
}

// AccessLogErrorStat is one grouped error URL, the core "what is broken" list.
type AccessLogErrorStat struct {
	Path            string `json:"path"`
	Status          int    `json:"status"`
	Hits            int64  `json:"hits"`
	UniqueIPs       int64  `json:"unique_ips"`
	InternalHits    int64  `json:"internal_hits"`
	LastVisitedAt   string `json:"last_visited_at"`
	LastReferer     string `json:"last_referer"`
	LastRefererKind string `json:"last_referer_kind"`
}

// AccessLogStatusStat is one row of the error status distribution.
type AccessLogStatusStat struct {
	Status int   `json:"status"`
	Hits   int64 `json:"hits"`
}

// AccessLogSummary powers the dashboard cards and the error panels.
type AccessLogSummary struct {
	Metrics         AccessLogMetrics      `json:"metrics"`
	TopPages        []AccessLogPageStat   `json:"top_pages"`
	TopErrors       []AccessLogErrorStat  `json:"top_errors"`
	StatusBreakdown []AccessLogStatusStat `json:"status_breakdown"`
}

// AccessLogFilter is the shared filter set of the dashboard, the log viewer
// and the clear action.
type AccessLogFilter struct {
	Kind        string
	Status      int
	StatusMode  string
	AgentKind   string
	DeviceKind  string
	RefererKind string
	Window      string
	Path        string
	PathPrefix  string
	IP          string
	Search      string
}

func parseAccessLogFilter(values url.Values) AccessLogFilter {
	return AccessLogFilter{
		Kind:        strings.ToLower(strings.TrimSpace(values.Get("kind"))),
		Status:      positiveInt(values.Get("status"), 0),
		StatusMode:  strings.ToLower(strings.TrimSpace(values.Get("status_mode"))),
		AgentKind:   strings.ToLower(strings.TrimSpace(values.Get("agent_kind"))),
		DeviceKind:  strings.ToLower(strings.TrimSpace(values.Get("device_kind"))),
		RefererKind: strings.ToLower(strings.TrimSpace(values.Get("referer_kind"))),
		Window:      strings.ToLower(strings.TrimSpace(values.Get("window"))),
		Path:        strings.TrimSpace(values.Get("path")),
		PathPrefix:  strings.TrimSpace(values.Get("path_prefix")),
		IP:          strings.TrimSpace(values.Get("ip")),
		Search:      strings.TrimSpace(values.Get("q")),
	}
}

var accessLogKinds = map[string]bool{"": true, "all": true, "page": true, "error": true}
var accessLogAgentKinds = map[string]bool{"": true, "all": true, "browser": true, "bot": true, "tool": true, "other": true}
var accessLogDeviceKinds = map[string]bool{"": true, "all": true, "desktop": true, "mobile": true, "tablet": true, "other": true}
var accessLogRefererKinds = map[string]bool{"": true, "all": true, "internal": true, "external": true, "none": true}
var accessLogStatusModes = map[string]bool{"": true, "all": true, "errors": true, "404": true, "4xx": true, "5xx": true, "200": true, "3xx": true}
var accessLogWindows = map[string]bool{"": true, "all": true, "today": true, "1h": true, "24h": true, "3d": true}

func normalizeAccessLogFilter(filter AccessLogFilter) AccessLogFilter {
	if !accessLogKinds[filter.Kind] {
		filter.Kind = ""
	}
	if !accessLogAgentKinds[filter.AgentKind] {
		filter.AgentKind = ""
	}
	if !accessLogDeviceKinds[filter.DeviceKind] {
		filter.DeviceKind = ""
	}
	if !accessLogRefererKinds[filter.RefererKind] {
		filter.RefererKind = ""
	}
	if !accessLogStatusModes[filter.StatusMode] {
		filter.StatusMode = ""
	}
	if !accessLogWindows[filter.Window] {
		filter.Window = ""
	}
	if len(filter.Path) > 300 {
		filter.Path = filter.Path[:300]
	}
	if len(filter.Search) > 200 {
		filter.Search = filter.Search[:200]
	}
	if len(filter.IP) > 64 {
		filter.IP = filter.IP[:64]
	}
	return filter
}

func escapeAccessLogLike(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

// buildAccessLogWhere renders the shared WHERE clause. The site id is always
// the first condition, so no query can leak rows across sites.
func (s *Server) buildAccessLogWhere(siteID int64, filter AccessLogFilter) (string, []any) {
	clauses := []string{`"site_id" = ?`}
	args := []any{siteID}
	switch filter.Kind {
	case "page", "error":
		clauses = append(clauses, `"kind" = ?`)
		args = append(args, filter.Kind)
	}
	switch filter.StatusMode {
	case "errors":
		clauses = append(clauses, `"status" >= 400`)
	case "404":
		clauses = append(clauses, `"status" = 404`)
	case "4xx":
		clauses = append(clauses, `"status" >= 400 AND "status" < 500`)
	case "5xx":
		clauses = append(clauses, `"status" >= 500`)
	case "3xx":
		clauses = append(clauses, `"status" >= 300 AND "status" < 400`)
	case "200":
		clauses = append(clauses, `"status" = 200`)
	}
	if filter.Status > 0 {
		clauses = append(clauses, `"status" = ?`)
		args = append(args, filter.Status)
	}
	switch filter.Window {
	case "today":
		clauses = append(clauses, `"day_key" = ?`)
		args = append(args, s.accessLogs.dayKey(time.Now()))
	case "1h":
		clauses = append(clauses, `"visited_at" >= datetime('now', '-1 hours')`)
	case "24h":
		clauses = append(clauses, `"visited_at" >= datetime('now', '-24 hours')`)
	case "3d":
		clauses = append(clauses, `"visited_at" >= datetime('now', '-3 days')`)
	}
	if filter.AgentKind != "" && filter.AgentKind != "all" {
		clauses = append(clauses, `"agent_kind" = ?`)
		args = append(args, filter.AgentKind)
	}
	if filter.DeviceKind != "" && filter.DeviceKind != "all" {
		clauses = append(clauses, `"device_kind" = ?`)
		args = append(args, filter.DeviceKind)
	}
	if filter.RefererKind != "" && filter.RefererKind != "all" {
		clauses = append(clauses, `"referer_kind" = ?`)
		args = append(args, filter.RefererKind)
	}
	if filter.Path != "" {
		clauses = append(clauses, `"path" LIKE ? ESCAPE '\'`)
		args = append(args, "%"+escapeAccessLogLike(filter.Path)+"%")
	}
	if filter.PathPrefix != "" {
		clauses = append(clauses, `"path" LIKE ? ESCAPE '\'`)
		args = append(args, escapeAccessLogLike(filter.PathPrefix)+"%")
	}
	if filter.IP != "" {
		clauses = append(clauses, `"ip" = ?`)
		args = append(args, filter.IP)
	}
	if filter.Search != "" {
		pattern := "%" + escapeAccessLogLike(filter.Search) + "%"
		clauses = append(clauses, `("path" LIKE ? ESCAPE '\' OR "referer" LIKE ? ESCAPE '\' OR "user_agent" LIKE ? ESCAPE '\' OR "agent_label" LIKE ? ESCAPE '\')`)
		args = append(args, pattern, pattern, pattern, pattern)
	}
	return strings.Join(clauses, " AND "), args
}

func (s *Server) accessLogSummary(ctx context.Context, siteID int64) (AccessLogSummary, error) {
	summary := AccessLogSummary{
		TopPages:        []AccessLogPageStat{},
		TopErrors:       []AccessLogErrorStat{},
		StatusBreakdown: []AccessLogStatusStat{},
	}
	dayKey := s.accessLogs.dayKey(time.Now())
	summary.Metrics.DayKey = dayKey
	summary.Metrics.RetentionDays = s.accessLogs.retention()

	var row struct {
		totalRows      sql.NullInt64
		todayVisits    sql.NullInt64
		todayErrors    sql.NullInt64
		recentVisits   sql.NullInt64
		recentErrors   sql.NullInt64
		totalErrors    sql.NullInt64
		totalNotFound  sql.NullInt64
		recentNotFound sql.NullInt64
		internalNotF   sql.NullInt64
		totalPages     sql.NullInt64
		recentUsers    sql.NullInt64
		recentPC       sql.NullInt64
		recentMobile   sql.NullInt64
	}
	err := s.database.QueryRowContext(ctx, `SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN "kind" = 'page' AND "day_key" = ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN "kind" = 'error' AND "day_key" = ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN "kind" = 'page' AND "visited_at" >= datetime('now', '-24 hours') THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN "kind" = 'error' AND "visited_at" >= datetime('now', '-24 hours') THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN "kind" = 'error' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN "status" = 404 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN "status" = 404 AND "visited_at" >= datetime('now', '-24 hours') THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN "status" = 404 AND "referer_kind" = 'internal' THEN 1 ELSE 0 END), 0),
			COUNT(DISTINCT CASE WHEN "kind" = 'page' AND "status" = 200 THEN "path" END),
			COUNT(DISTINCT CASE WHEN "kind" = 'page' AND "agent_kind" <> 'bot' AND "ip" <> '' AND "visited_at" >= datetime('now', '-24 hours') THEN "ip" END),
			COUNT(DISTINCT CASE WHEN "kind" = 'page' AND "agent_kind" <> 'bot' AND "ip" <> '' AND "device_kind" = 'desktop' AND "visited_at" >= datetime('now', '-24 hours') THEN "ip" END),
			COUNT(DISTINCT CASE WHEN "kind" = 'page' AND "agent_kind" <> 'bot' AND "ip" <> '' AND "device_kind" = 'mobile' AND "visited_at" >= datetime('now', '-24 hours') THEN "ip" END)
		FROM "`+db.AccessLogTable+`" WHERE "site_id" = ?`, dayKey, dayKey, siteID).
		Scan(&row.totalRows, &row.todayVisits, &row.todayErrors, &row.recentVisits, &row.recentErrors,
			&row.totalErrors, &row.totalNotFound, &row.recentNotFound, &row.internalNotF, &row.totalPages,
			&row.recentUsers, &row.recentPC, &row.recentMobile)
	if err != nil {
		return summary, err
	}
	summary.Metrics.TotalRequests = row.totalRows.Int64
	summary.Metrics.TodayVisits = row.todayVisits.Int64
	summary.Metrics.TodayErrors = row.todayErrors.Int64
	summary.Metrics.RecentVisits = row.recentVisits.Int64
	summary.Metrics.RecentErrors = row.recentErrors.Int64
	summary.Metrics.TotalErrors = row.totalErrors.Int64
	summary.Metrics.TotalNotFound = row.totalNotFound.Int64
	summary.Metrics.RecentNotFound = row.recentNotFound.Int64
	summary.Metrics.InternalNotFound = row.internalNotF.Int64
	summary.Metrics.TotalPages = row.totalPages.Int64
	summary.Metrics.RecentUsers = row.recentUsers.Int64
	summary.Metrics.RecentPCUsers = row.recentPC.Int64
	summary.Metrics.RecentMobileUsers = row.recentMobile.Int64

	// "New" means the visitor's first recorded request falls inside the window;
	// the window is bounded by the retention period.
	var newUsers sql.NullInt64
	if err := s.database.QueryRowContext(ctx, `SELECT COUNT(*) FROM (
			SELECT "ip" FROM "`+db.AccessLogTable+`"
			WHERE "site_id" = ? AND "kind" = 'page' AND "agent_kind" <> 'bot' AND "ip" <> ''
			GROUP BY "ip" HAVING MIN("visited_at") >= datetime('now', '-24 hours')
		)`, siteID).Scan(&newUsers); err == nil {
		summary.Metrics.RecentNewUsers = newUsers.Int64
	}

	pageRows, err := s.database.QueryContext(ctx, `SELECT "path", COUNT(*), COUNT(DISTINCT "ip"), MAX("visited_at")
		FROM "`+db.AccessLogTable+`"
		WHERE "site_id" = ? AND "kind" = 'page' AND "status" = 200 AND "agent_kind" <> 'bot'
		GROUP BY "path" ORDER BY COUNT(*) DESC, MAX("visited_at") DESC LIMIT ?`, siteID, accessLogTopPagesLimit)
	if err != nil {
		return summary, err
	}
	for pageRows.Next() {
		var item AccessLogPageStat
		var last sql.NullString
		if scanErr := pageRows.Scan(&item.Path, &item.Visits, &item.UniqueIPs, &last); scanErr != nil {
			_ = pageRows.Close()
			return summary, scanErr
		}
		item.LastVisitedAt = s.accessLogs.formatVisited(last.String)
		summary.TopPages = append(summary.TopPages, item)
	}
	_ = pageRows.Close()

	errorRows, err := s.database.QueryContext(ctx, `SELECT
			"path", "status", COUNT(*), COUNT(DISTINCT "ip"),
			COALESCE(SUM(CASE WHEN "referer_kind" = 'internal' THEN 1 ELSE 0 END), 0),
			MAX("visited_at"),
			COALESCE((SELECT "referer" FROM "`+db.AccessLogTable+`" x
				WHERE x."site_id" = l."site_id" AND x."path" = l."path" AND x."status" = l."status" AND x."referer" <> ''
				ORDER BY x."id" DESC LIMIT 1), ''),
			COALESCE((SELECT "referer_kind" FROM "`+db.AccessLogTable+`" x
				WHERE x."site_id" = l."site_id" AND x."path" = l."path" AND x."status" = l."status" AND x."referer" <> ''
				ORDER BY x."id" DESC LIMIT 1), 'none')
		FROM "`+db.AccessLogTable+`" l
		WHERE l."site_id" = ? AND l."status" >= 400
		GROUP BY l."path", l."status" ORDER BY COUNT(*) DESC, MAX(l."visited_at") DESC LIMIT ?`, siteID, accessLogTopErrorsLimit)
	if err != nil {
		return summary, err
	}
	for errorRows.Next() {
		var item AccessLogErrorStat
		var last sql.NullString
		if scanErr := errorRows.Scan(&item.Path, &item.Status, &item.Hits, &item.UniqueIPs, &item.InternalHits, &last, &item.LastReferer, &item.LastRefererKind); scanErr != nil {
			_ = errorRows.Close()
			return summary, scanErr
		}
		item.LastVisitedAt = s.accessLogs.formatVisited(last.String)
		summary.TopErrors = append(summary.TopErrors, item)
	}
	_ = errorRows.Close()

	statusRows, err := s.database.QueryContext(ctx, `SELECT "status", COUNT(*) FROM "`+db.AccessLogTable+`"
		WHERE "site_id" = ? AND "status" >= 400 GROUP BY "status" ORDER BY COUNT(*) DESC`, siteID)
	if err != nil {
		return summary, err
	}
	for statusRows.Next() {
		var item AccessLogStatusStat
		if scanErr := statusRows.Scan(&item.Status, &item.Hits); scanErr != nil {
			_ = statusRows.Close()
			return summary, scanErr
		}
		summary.StatusBreakdown = append(summary.StatusBreakdown, item)
	}
	_ = statusRows.Close()
	return summary, nil
}

func (s *Server) listAccessLogs(ctx context.Context, siteID int64, filter AccessLogFilter, page, pageSize int) ([]AccessLogItem, int64, error) {
	filter = normalizeAccessLogFilter(filter)
	where, args := s.buildAccessLogWhere(siteID, filter)
	var total int64
	if err := s.database.QueryRowContext(ctx, `SELECT COUNT(*) FROM "`+db.AccessLogTable+`" WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	queryArgs := append(append([]any{}, args...), pageSize, offset)
	rows, err := s.database.QueryContext(ctx, `SELECT "id", "site_id", "day_key", "path", "query", "method", "status", "kind",
			"content_type", "ip", "referer", "referer_host", "referer_kind", "user_agent", "agent_kind", "agent_label", "device_kind", "visited_at"
		FROM "`+db.AccessLogTable+`" WHERE `+where+` ORDER BY "id" DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []AccessLogItem{}
	for rows.Next() {
		var item AccessLogItem
		var visited sql.NullString
		if err := rows.Scan(&item.ID, &item.SiteID, &item.DayKey, &item.Path, &item.Query, &item.Method, &item.Status, &item.Kind,
			&item.ContentType, &item.IP, &item.Referer, &item.RefererHost, &item.RefererKind, &item.UserAgent, &item.AgentKind, &item.AgentLabel, &item.DeviceKind, &visited); err != nil {
			return nil, 0, err
		}
		item.VisitedAt = s.accessLogs.formatVisited(visited.String)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *Server) listAccessLogErrors(ctx context.Context, siteID int64, filter AccessLogFilter, limit int) ([]AccessLogErrorStat, error) {
	filter = normalizeAccessLogFilter(filter)
	where, args := s.buildAccessLogWhere(siteID, filter)
	if filter.StatusMode == "" && filter.Status == 0 {
		where += ` AND "status" >= 400`
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	args = append(args, limit)
	rows, err := s.database.QueryContext(ctx, `SELECT
			l."path", l."status", COUNT(*), COUNT(DISTINCT l."ip"),
			COALESCE(SUM(CASE WHEN l."referer_kind" = 'internal' THEN 1 ELSE 0 END), 0),
			MAX(l."visited_at"),
			COALESCE((SELECT x."referer" FROM "`+db.AccessLogTable+`" x
				WHERE x."site_id" = l."site_id" AND x."path" = l."path" AND x."status" = l."status" AND x."referer" <> ''
				ORDER BY x."id" DESC LIMIT 1), ''),
			COALESCE((SELECT x."referer_kind" FROM "`+db.AccessLogTable+`" x
				WHERE x."site_id" = l."site_id" AND x."path" = l."path" AND x."status" = l."status" AND x."referer" <> ''
				ORDER BY x."id" DESC LIMIT 1), 'none')
		FROM "`+db.AccessLogTable+`" l
		WHERE `+where+`
		GROUP BY l."path", l."status" ORDER BY COUNT(*) DESC, MAX(l."visited_at") DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AccessLogErrorStat{}
	for rows.Next() {
		var item AccessLogErrorStat
		var last sql.NullString
		if err := rows.Scan(&item.Path, &item.Status, &item.Hits, &item.UniqueIPs, &item.InternalHits, &last, &item.LastReferer, &item.LastRefererKind); err != nil {
			return nil, err
		}
		item.LastVisitedAt = s.accessLogs.formatVisited(last.String)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Server) clearAccessLogs(ctx context.Context, siteID int64, filter AccessLogFilter) (int64, error) {
	filter = normalizeAccessLogFilter(filter)
	where, args := s.buildAccessLogWhere(siteID, filter)
	result, err := s.database.ExecContext(ctx, `DELETE FROM "`+db.AccessLogTable+`" WHERE `+where, args...)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return affected, nil
}

// cleanupAccessLogs drops rows older than the retention window.
func (s *Server) cleanupAccessLogs(ctx context.Context) (int64, error) {
	if s == nil || s.database == nil || s.accessLogs == nil {
		return 0, nil
	}
	days := s.accessLogs.retention()
	result, err := s.database.ExecContext(ctx, `DELETE FROM "`+db.AccessLogTable+`" WHERE "visited_at" < datetime('now', ?)`,
		fmt.Sprintf("-%d days", days))
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return affected, nil
}

// StartAccessLogMaintenance prunes expired access log rows on boot and then
// periodically until the context is cancelled.
func (s *Server) StartAccessLogMaintenance(ctx context.Context) {
	if s == nil || s.database == nil || s.accessLogs == nil {
		return
	}
	go func() {
		run := func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			deleted, err := s.cleanupAccessLogs(cleanupCtx)
			if err != nil {
				log.Printf("prune access logs: %v", err)
				return
			}
			if deleted > 0 {
				log.Printf("pruned %d expired access log rows", deleted)
			}
		}
		run()
		ticker := time.NewTicker(accessLogCleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}

// StartAccessLogMaintenance prunes expired access log rows on boot and then
// periodically until the context is cancelled.
