package site

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"regexp/syntax"
	"strconv"
	"strings"
	"unicode"

	"gocms/internal/routing"
)

var redirectCapture = regexp.MustCompile(`\$\$|\$\{([\pL\p{Nd}_]+)\}|\$([\pL\p{Nd}_]+)|\$`)

func compileRedirectPattern(source string) (*regexp.Regexp, error) {
	if source == "" || strings.ContainsFunc(source, unicode.IsControl) {
		return nil, errors.New("来源正则不能为空或包含控制字符")
	}
	parsed, err := syntax.Parse(source, syntax.Perl)
	if err != nil {
		return nil, fmt.Errorf("来源正则无效：%w", err)
	}
	return regexp.Compile(`\A(?:` + parsed.String() + `)\z`)
}

func parseRegexRedirectTarget(raw string) (*url.URL, error) {
	if strings.ContainsRune(raw, '\\') || strings.ContainsFunc(raw, unicode.IsControl) {
		return nil, errors.New("目标地址不能包含反斜杠或控制字符")
	}
	target, err := url.Parse(raw)
	if err != nil || target.IsAbs() || target.Host != "" || strings.HasPrefix(raw, "//") || strings.HasPrefix(target.Path, "//") ||
		(!strings.HasPrefix(target.Path, "/") && (!strings.HasPrefix(target.Path, "$") || strings.HasPrefix(target.Path, "$$"))) {
		return nil, errors.New("正则规则的目标必须是站内路径，可用捕获组生成路径")
	}
	if strings.ContainsRune(target.Path, '\\') || strings.ContainsFunc(target.Path, unicode.IsControl) {
		return nil, errors.New("目标路径包含非法字符")
	}
	return target, nil
}

func validateRegexRedirect(rule redirectRule) error {
	pattern, err := compileRedirectPattern(rule.SourcePath)
	if err != nil {
		return err
	}
	target, err := parseRegexRedirectTarget(rule.TargetURL)
	if err != nil {
		return err
	}
	for _, token := range redirectCapture.FindAllStringSubmatch(target.Path, -1) {
		if token[0] == "$$" {
			continue
		}
		name := token[1] + token[2]
		if name == "" {
			return errors.New("目标路径中的捕获组引用无效，可使用 ${1} 或 ${name}，用 $$ 表示美元符号")
		}
		if index, err := strconv.Atoi(name); err == nil && index >= 0 && strconv.Itoa(index) == name {
			if index <= pattern.NumSubexp() {
				continue
			}
		} else if pattern.SubexpIndex(name) >= 0 {
			continue
		}
		return fmt.Errorf("目标路径引用了不存在的捕获组：%s", token[0])
	}
	return nil
}

func (s *Server) enabledRegexRedirects(ctx context.Context, siteID int64) ([]redirectRule, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT source_path, target_url, status_code, preserve_query FROM gocms_redirect
		WHERE site_id = ? AND match_type = 'regex' AND enabled = 1 ORDER BY id`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rules []redirectRule
	for rows.Next() {
		var rule redirectRule
		if err := rows.Scan(&rule.SourcePath, &rule.TargetURL, &rule.StatusCode, &rule.PreserveQuery); err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func expandedRegexRedirectTarget(rule redirectRule, source string) *url.URL {
	pattern, err := compileRedirectPattern(rule.SourcePath)
	if err != nil {
		return nil
	}
	match := pattern.FindStringSubmatchIndex(source)
	if match == nil {
		return nil
	}
	target, err := parseRegexRedirectTarget(rule.TargetURL)
	if err != nil {
		return nil
	}
	expanded := string(pattern.ExpandString(nil, target.Path, source, match))
	if !strings.HasPrefix(expanded, "/") || strings.HasPrefix(expanded, "//") || len(expanded) > 4096 {
		return nil
	}
	target.Path = redirectPath(expanded)
	target.RawPath = ""
	// FileServer serves index.html at its directory URL. Check that URL for exact rules too.
	if strings.HasSuffix(target.Path, "/index.html") {
		target.Path = strings.TrimSuffix(target.Path, "index.html")
	} else if strings.HasSuffix(expanded, "/") && target.Path != "/" {
		target.Path += "/"
	}
	return target
}

func (s *Server) serveRegexRedirect(w http.ResponseWriter, r *http.Request, siteID int64, dirs []string, source string) bool {
	if (r.Method != http.MethodGet && r.Method != http.MethodHead) || len(source) > 4096 || protectedRedirectPath(source) ||
		strings.ContainsRune(source, '\\') || strings.ContainsFunc(source, unicode.IsControl) {
		return false
	}
	rules, err := s.enabledRegexRedirects(r.Context(), siteID)
	if err != nil {
		http.Error(w, "redirect lookup failed", http.StatusInternalServerError)
		return true
	}
	for _, rule := range rules {
		target := expandedRegexRedirectTarget(rule, source)
		if target == nil {
			continue
		}
		targetPath := redirectPath(target.Path)
		if targetPath == source || targetPath == searchRedirectPath || protectedRedirectPath(targetPath) ||
			staticPathBlocked(strings.ToLower(targetPath)) || routing.IsNotFoundPage(targetPath) || !s.pathServed(dirs, targetPath) {
			continue
		}
		if _, _, resource := s.resourceRoots(r.Context(), siteID, target.Path); resource {
			continue
		}
		if _, err := s.exactRedirect(r.Context(), siteID, targetPath); err == nil {
			continue
		} else if !errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "redirect lookup failed", http.StatusInternalServerError)
			return true
		}
		writeConfiguredRedirect(w, r, target, rule)
		return true
	}
	return false
}
