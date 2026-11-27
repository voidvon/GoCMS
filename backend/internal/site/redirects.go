package site

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode"

	"gocms/internal/db"
)

type redirectRule struct {
	ID            int64  `json:"id"`
	MatchType     string `json:"match_type"`
	SourcePath    string `json:"source_path"`
	TargetURL     string `json:"target_url"`
	StatusCode    int    `json:"status_code"`
	Enabled       bool   `json:"enabled"`
	PreserveQuery bool   `json:"preserve_query"`
	Note          string `json:"note"`
}

const (
	redirectMatchExact = "exact"
	redirectMatchRegex = "regex"
)

func newRedirectRule() redirectRule {
	return redirectRule{MatchType: redirectMatchExact, StatusCode: http.StatusMovedPermanently, Enabled: true, PreserveQuery: true}
}

func redirectPath(value string) string {
	return path.Clean("/" + strings.TrimPrefix(value, "/"))
}

func protectedRedirectPath(value string) bool {
	value = strings.ToLower(redirectPath(value))
	if value == "/assets/theme" || strings.HasPrefix(value, "/assets/theme/") || strings.Contains(value, "/themes/") || strings.HasSuffix(value, "/themes") {
		return true
	}
	for _, prefix := range []string{"/api", "/admin", "/shop", "/data", "/database", "/.git", "/internal", "/cmd"} {
		if value == prefix || strings.HasPrefix(value, prefix+"/") {
			return true
		}
	}
	return false
}

func normalizeRedirect(rule *redirectRule) error {
	rule.SourcePath = strings.TrimSpace(rule.SourcePath)
	rule.TargetURL = strings.TrimSpace(rule.TargetURL)
	rule.Note = strings.TrimSpace(rule.Note)
	if len(rule.SourcePath) > 2048 || len(rule.TargetURL) > 4096 || len(rule.Note) > 1000 {
		return errors.New("来源路径、目标地址或备注过长")
	}
	if rule.StatusCode != http.StatusMovedPermanently && rule.StatusCode != http.StatusFound {
		return errors.New("重定向状态码仅支持 301 或 302")
	}
	switch rule.MatchType {
	case redirectMatchRegex:
		return validateRegexRedirect(*rule)
	case redirectMatchExact:
		return normalizeExactRedirect(rule)
	default:
		return errors.New("重定向匹配方式无效")
	}
}

func normalizeExactRedirect(rule *redirectRule) error {
	for _, value := range []string{rule.SourcePath, rule.TargetURL} {
		if strings.ContainsRune(value, '\\') || strings.ContainsFunc(value, unicode.IsControl) {
			return errors.New("地址不能包含反斜杠或控制字符")
		}
	}
	source, err := url.Parse(rule.SourcePath)
	if err != nil || source.IsAbs() || source.Host != "" || source.RawQuery != "" || source.ForceQuery || source.Fragment != "" || strings.Contains(rule.SourcePath, "#") || !strings.HasPrefix(source.Path, "/") || strings.HasPrefix(source.Path, "//") {
		return errors.New("来源必须是以 / 开头的站内路径，不能包含域名、查询参数或锚点")
	}
	if strings.ContainsRune(source.Path, '\\') || strings.ContainsFunc(source.Path, unicode.IsControl) {
		return errors.New("来源路径包含非法字符")
	}
	rule.SourcePath = redirectPath(source.Path)
	if protectedRedirectPath(rule.SourcePath) {
		return errors.New("后台、接口、购物前台和私有目录不能作为重定向来源")
	}
	target, err := url.Parse(rule.TargetURL)
	if err != nil || rule.TargetURL == "" || target.User != nil {
		return errors.New("目标地址无效")
	}
	if target.IsAbs() {
		if (target.Scheme != "https" && target.Scheme != "http") || target.Hostname() == "" {
			return errors.New("外部目标必须使用完整的 http:// 或 https:// 地址")
		}
	} else if target.Host != "" || !strings.HasPrefix(target.Path, "/") || strings.HasPrefix(target.Path, "//") {
		return errors.New("目标必须是以 / 开头的站内路径或完整的 HTTP 地址")
	}
	if strings.ContainsRune(target.Path, '\\') || strings.ContainsFunc(target.Path, unicode.IsControl) {
		return errors.New("目标路径包含非法字符")
	}
	rule.TargetURL = target.String()
	return nil
}

func (s *Server) exactRedirect(ctx context.Context, siteID int64, source string) (redirectRule, error) {
	var rule redirectRule
	err := s.database.QueryRowContext(ctx, `SELECT target_url, status_code, preserve_query FROM gocms_redirect
		WHERE site_id = ? AND match_type = 'exact' AND source_path = ? AND enabled = 1`, siteID, source).
		Scan(&rule.TargetURL, &rule.StatusCode, &rule.PreserveQuery)
	return rule, err
}

func (s *Server) serveConfiguredRedirect(w http.ResponseWriter, r *http.Request) bool {
	if s.database == nil || (r.Method != http.MethodGet && r.Method != http.MethodHead) || protectedRedirectPath(r.URL.Path) {
		return false
	}
	siteID, err := s.resolvePublicSiteID(r)
	if err != nil {
		return false
	}
	rule, err := s.exactRedirect(r.Context(), siteID, redirectPath(r.URL.Path))
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	if err != nil {
		http.Error(w, "redirect lookup failed", http.StatusInternalServerError)
		return true
	}
	destination, err := url.Parse(rule.TargetURL)
	if err != nil {
		http.Error(w, "invalid redirect target", http.StatusInternalServerError)
		return true
	}
	writeConfiguredRedirect(w, r, destination, rule)
	return true
}

func writeConfiguredRedirect(w http.ResponseWriter, r *http.Request, destination *url.URL, rule redirectRule) {
	if rule.PreserveQuery && r.URL.RawQuery != "" {
		query := destination.Query()
		for key, values := range r.URL.Query() {
			if !query.Has(key) {
				query[key] = values
			}
		}
		destination.RawQuery = query.Encode()
	}
	// Rules can be edited or disabled immediately; do not cache a stale 301.
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, destination.String(), rule.StatusCode)
}

func (s *Server) adminRedirects(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSiteID(r, s.currentAdmin(r))
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "无权访问该站点"})
		return
	}
	part := strings.Trim(strings.TrimPrefix(strings.ToLower(r.URL.Path), "/api/admin/redirects"), "/")
	if part == "" && r.Method == http.MethodGet {
		items, err := listRedirects(r.Context(), s.database, siteID)
		if err != nil {
			accountError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "site_id": siteID})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	if part == "import" && r.Method == http.MethodPost {
		var payload struct {
			Items []json.RawMessage `json:"items"`
		}
		if err := decodeRequest(r, &payload); err != nil || len(payload.Items) == 0 || len(payload.Items) > 1000 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请选择包含 items 数组的 JSON 文件，每次导入 1–1000 条规则"})
			return
		}
		rules := make([]redirectRule, len(payload.Items))
		for i, raw := range payload.Items {
			rules[i] = newRedirectRule()
			if err := json.Unmarshal(raw, &rules[i]); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("第 %d 条规则格式无效", i+1)})
				return
			}
			rules[i].ID = 0
			if err := normalizeRedirect(&rules[i]); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("第 %d 条规则：%s", i+1, err)})
				return
			}
		}
		if err := s.saveRedirects(r.Context(), siteID, rules); err != nil {
			writeRedirectError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"imported": len(payload.Items)})
		return
	}
	id := int64(0)
	if part != "" {
		id, err = strconv.ParseInt(part, 10, 64)
		if err != nil || id <= 0 {
			http.NotFound(w, r)
			return
		}
	}
	if id > 0 && r.Method == http.MethodDelete {
		result, err := s.database.ExecContext(r.Context(), `DELETE FROM gocms_redirect WHERE id = ? AND site_id = ?`, id, siteID)
		if err != nil {
			accountError(w, err)
			return
		}
		if n, _ := result.RowsAffected(); n == 0 {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	if (id == 0 && r.Method == http.MethodPost) || (id > 0 && r.Method == http.MethodPut) {
		rule := newRedirectRule()
		if err := decodeRequest(r, &rule); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的重定向规则"})
			return
		}
		rule.ID = id
		if err := normalizeRedirect(&rule); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := s.saveRedirects(r.Context(), siteID, []redirectRule{rule}); err != nil {
			writeRedirectError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	w.Header().Set("Allow", "GET, POST, PUT, DELETE")
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func listRedirects(ctx context.Context, database *sql.DB, siteID int64) ([]redirectRule, error) {
	rows, err := database.QueryContext(ctx, `SELECT id, match_type, source_path, target_url, status_code, enabled, preserve_query, note
		FROM gocms_redirect WHERE site_id = ? ORDER BY id DESC`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []redirectRule{}
	for rows.Next() {
		var rule redirectRule
		if err := rows.Scan(&rule.ID, &rule.MatchType, &rule.SourcePath, &rule.TargetURL, &rule.StatusCode, &rule.Enabled, &rule.PreserveQuery, &rule.Note); err != nil {
			return nil, err
		}
		items = append(items, rule)
	}
	return items, rows.Err()
}

type redirectValidationError struct{ message string }

func (e redirectValidationError) Error() string { return e.message }

func writeRedirectError(w http.ResponseWriter, err error) {
	var validation redirectValidationError
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "重定向规则不存在"})
	} else if errors.As(err, &validation) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": validation.message})
	} else {
		accountError(w, err)
	}
}

func (s *Server) saveRedirects(ctx context.Context, siteID int64, rules []redirectRule) error {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, rule := range rules {
		if rule.ID > 0 {
			var id int64
			if err := tx.QueryRowContext(ctx, `SELECT id FROM gocms_redirect WHERE id = ? AND site_id = ?`, rule.ID, siteID).Scan(&id); err != nil {
				return err
			}
		}
		var duplicate int64
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM gocms_redirect WHERE site_id = ? AND match_type = ? AND source_path = ? AND id <> ?`, siteID, rule.MatchType, rule.SourcePath, rule.ID).Scan(&duplicate); err != nil {
			return err
		}
		if duplicate > 0 {
			return redirectValidationError{"来源路径已存在：" + rule.SourcePath}
		}
		args := []any{rule.MatchType, rule.SourcePath, rule.TargetURL, rule.StatusCode, rule.Enabled, rule.PreserveQuery, rule.Note, siteID}
		if rule.ID == 0 {
			_, err = tx.ExecContext(ctx, `INSERT INTO gocms_redirect (match_type, source_path, target_url, status_code, enabled, preserve_query, note, site_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, args...)
		} else {
			var result sql.Result
			result, err = tx.ExecContext(ctx, `UPDATE gocms_redirect SET match_type = ?, source_path = ?, target_url = ?, status_code = ?, enabled = ?, preserve_query = ?, note = ?, updated_at = CURRENT_TIMESTAMP WHERE site_id = ? AND id = ?`, append(args, rule.ID)...)
			if err == nil {
				if n, _ := result.RowsAffected(); n == 0 {
					return sql.ErrNoRows
				}
			}
		}
		if err != nil {
			return err
		}
	}
	if err := validateRedirectCycles(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// Validate the configured graph across sites too: an absolute destination may
// point to another configured site's relative redirect and then return here.
func validateRedirectCycles(ctx context.Context, tx *sql.Tx) error {
	type node struct {
		siteID int64
		path   string
	}
	// Regex rules only redirect missing paths to existing pages without an exact redirect.
	rows, err := tx.QueryContext(ctx, `SELECT site_id, source_path, target_url FROM gocms_redirect WHERE match_type = 'exact' AND enabled = 1`)
	if err != nil {
		return err
	}
	targets := map[node]string{}
	for rows.Next() {
		var source node
		var target string
		if err := rows.Scan(&source.siteID, &source.path, &target); err != nil {
			rows.Close()
			return err
		}
		targets[source] = target
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	edges := map[node]node{}
	hostSites := map[string]int64{}
	for source, raw := range targets {
		target, err := url.Parse(raw)
		if err != nil {
			return err
		}
		targetSite := source.siteID
		if target.Host != "" {
			var ok bool
			targetSite, ok = hostSites[target.Host]
			if !ok {
				targetSite = 0
				site, err := db.GetSiteByHost(ctx, tx, target.Host)
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return err
				}
				if site != nil {
					targetSite = site.ID
				}
				hostSites[target.Host] = targetSite
			}
		}
		if targetSite > 0 {
			edges[source] = node{targetSite, redirectPath(target.Path)}
		}
	}
	finished := map[node]bool{}
	for start := range edges {
		seen := map[node]bool{}
		for current := start; !finished[current]; {
			if seen[current] {
				return redirectValidationError{"规则会形成循环重定向，请修改目标地址"}
			}
			seen[current] = true
			next, ok := edges[current]
			if !ok {
				break
			}
			current = next
		}
		for visited := range seen {
			finished[visited] = true
		}
	}
	return nil
}
