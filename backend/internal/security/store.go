// Package security owns platform and tenant-scoped request protection. Nginx is
// an optional enforcement adapter; the CMS never executes privileged commands.
package security

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"path"
	"path/filepath"
	"strings"
	"time"

	"gocms/internal/nginxguard"
)

type Platform struct {
	TrustedProxies []string `json:"trusted_proxies"`
	NginxEnabled   bool     `json:"nginx_enabled"`
	SocketPath     string   `json:"socket_path"`
	NginxBinary    string   `json:"nginx_binary"`
	NginxConfig    string   `json:"nginx_config"`
	ServerConfig   string   `json:"server_config"`
	AgentUser      string   `json:"agent_user"`
}

func DefaultPlatform() Platform {
	return Platform{TrustedProxies: []string{"127.0.0.1/32", "::1/128"}, SocketPath: "/run/gocms-security/agent.sock", NginxBinary: "/www/server/nginx/sbin/nginx", NginxConfig: "/www/server/nginx/conf/nginx.conf", ServerConfig: "/www/server/panel/vhost/nginx/gocms.conf", AgentUser: "www"}
}

type Policy struct {
	Enabled       bool     `json:"enabled"`
	WindowSeconds int      `json:"window_seconds"`
	Threshold     int      `json:"threshold"`
	BanScope      string   `json:"ban_scope"`
	BanSeconds    int      `json:"ban_seconds"`
	Paths         []string `json:"paths"`
}

func DefaultPolicy() Policy {
	return Policy{WindowSeconds: 300, Threshold: 5, BanScope: "global", Paths: []string{"/server.key", "/config.php", "/.env", "/.env.*", "/.git/*", "/.aws/credentials"}}
}

type Entry struct {
	ID        int64  `json:"id"`
	SiteID    int64  `json:"site_id"`
	Network   string `json:"network"`
	Kind      string `json:"kind"`
	Reason    string `json:"reason"`
	Source    string `json:"source"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
	RevokedAt string `json:"revoked_at"`
}

type Event struct {
	ID        int64  `json:"id"`
	SiteID    int64  `json:"site_id"`
	IP        string `json:"ip"`
	Host      string `json:"host"`
	Path      string `json:"path"`
	Action    string `json:"action"`
	Reason    string `json:"reason"`
	CreatedAt string `json:"created_at"`
}

func Ensure(ctx context.Context, database *sql.DB) error {
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS gocms_security_config (scope_id INTEGER PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS gocms_security_entry (id INTEGER PRIMARY KEY AUTOINCREMENT, site_id INTEGER NOT NULL, network TEXT NOT NULL, kind TEXT NOT NULL, reason TEXT NOT NULL DEFAULT '', source TEXT NOT NULL, created_at TEXT NOT NULL, expires_at TEXT NOT NULL DEFAULT '', revoked_at TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX IF NOT EXISTS idx_security_entry_scope ON gocms_security_entry(site_id,kind,revoked_at,expires_at)`,
		`CREATE TABLE IF NOT EXISTS gocms_security_event (id INTEGER PRIMARY KEY AUTOINCREMENT, site_id INTEGER NOT NULL, ip TEXT NOT NULL, host TEXT NOT NULL, path TEXT NOT NULL, action TEXT NOT NULL, reason TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_security_event_scope ON gocms_security_event(site_id,id)`,
		`CREATE TABLE IF NOT EXISTS gocms_security_state (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func loadConfig(ctx context.Context, database *sql.DB, scope int64, value any) error {
	var raw string
	err := database.QueryRowContext(ctx, `SELECT value FROM gocms_security_config WHERE scope_id=?`, scope).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(raw), value)
}

func LoadPlatform(ctx context.Context, database *sql.DB) (Platform, error) {
	p := DefaultPlatform()
	err := loadConfig(ctx, database, 0, &p)
	return p, err
}

func LoadPolicy(ctx context.Context, database *sql.DB, siteID int64) (Policy, error) {
	p := DefaultPolicy()
	var saved json.RawMessage
	if err := loadConfig(ctx, database, siteID, &saved); err != nil || len(saved) == 0 {
		return p, err
	}
	// Policies saved before ban_scope existed retain their original site boundary.
	p.BanScope = "site"
	err := json.Unmarshal(saved, &p)
	return p, err
}

func SaveConfig(ctx context.Context, database *sql.DB, scope int64, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = database.ExecContext(ctx, `INSERT INTO gocms_security_config(scope_id,value) VALUES(?,?) ON CONFLICT(scope_id) DO UPDATE SET value=excluded.value`, scope, string(raw))
	return err
}

func Network(value string) (netip.Prefix, error) {
	value = strings.TrimSpace(value)
	if addr, err := netip.ParseAddr(value); err == nil && addr.Zone() == "" {
		addr = addr.Unmap()
		return netip.PrefixFrom(addr, addr.BitLen()), nil
	}
	p, err := netip.ParsePrefix(value)
	if err != nil || p.Addr().Zone() != "" || p.Addr().Is4In6() {
		return netip.Prefix{}, errors.New("请输入有效的 IPv4、IPv6 地址或 CIDR 网段")
	}
	return p.Masked(), nil
}

func ValidatePlatform(p *Platform) error {
	if len(p.TrustedProxies) > 64 {
		return errors.New("可信代理最多 64 项")
	}
	for i, value := range p.TrustedProxies {
		n, err := Network(value)
		if err != nil || n.Bits() == 0 {
			return errors.New("可信代理必须为明确的 IP 或网段，不能信任所有地址")
		}
		p.TrustedProxies[i] = n.String()
	}
	for _, value := range []string{p.SocketPath, p.NginxBinary, p.NginxConfig, p.ServerConfig} {
		if !filepath.IsAbs(value) || filepath.Clean(value) != value || len(value) > 512 || strings.ContainsAny(value, "\x00\r\n\"'`;${} \\ ") {
			return errors.New("接入路径必须为不含空白或特殊字符的绝对路径")
		}
	}
	if err := (nginxguard.Config{SocketPath: p.SocketPath, NginxBinary: p.NginxBinary, NginxConfig: p.NginxConfig, ServerConfig: p.ServerConfig, AgentUser: p.AgentUser}).Validate(); err != nil {
		return errors.New("接入路径或运行用户无效")
	}
	if len(p.SocketPath) > 100 {
		return errors.New("本机 Socket 路径过长")
	}
	if p.NginxConfig == p.ServerConfig {
		return errors.New("Nginx 主配置和站点配置不能为同一文件")
	}
	if p.AgentUser == "" || len(p.AgentUser) > 64 || strings.Trim(p.AgentUser, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-") != "" {
		return errors.New("请输入有效的 GoCMS 系统运行用户")
	}
	return nil
}

func ValidatePolicy(p *Policy) error {
	if p.WindowSeconds < 10 || p.WindowSeconds > 86400 || p.Threshold < 2 || p.Threshold > 1000 || (p.BanSeconds != 0 && (p.BanSeconds < 60 || p.BanSeconds > 30*86400)) {
		return errors.New("检测窗口需为 10–86400 秒，阈值为 2–1000 次，封禁时长为长期有效或 60 秒至 30 天")
	}
	if p.BanScope == "" {
		p.BanScope = "global"
	}
	if p.BanScope != "global" && p.BanScope != "site" {
		return errors.New("自动封禁范围无效")
	}
	if len(p.Paths) == 0 || len(p.Paths) > 100 {
		return errors.New("请配置 1–100 条明确的扫描路径")
	}
	seen := map[string]bool{}
	var paths []string
	for _, value := range p.Paths {
		value = strings.ToLower(strings.TrimSpace(value))
		base := strings.TrimSuffix(value, "*")
		if !strings.HasPrefix(base, "/") || base == "/" || len(value) > 256 || !nginxguard.ValidScanPath(value) || strings.ContainsAny(base, "*? #\r\n\x00\\\t\"';${}") || path.Clean(base) != strings.TrimSuffix(base, "/") {
			return fmt.Errorf("扫描路径无效：%s；仅支持末尾 * 前缀匹配", value)
		}
		if base == "/admin" || strings.HasPrefix(base, "/admin/") || base == "/api" || strings.HasPrefix(base, "/api/") || strings.HasPrefix(base, "/.well-known") {
			return errors.New("扫描规则不能覆盖后台、API 或证书验证路径")
		}
		if !seen[value] {
			seen[value] = true
			paths = append(paths, value)
		}
	}
	p.Paths = paths
	return nil
}

func Matches(p Policy, value string) bool {
	value = strings.ToLower(path.Clean(value))
	for _, pattern := range p.Paths {
		if strings.HasSuffix(pattern, "*") {
			if strings.HasPrefix(value, strings.TrimSuffix(pattern, "*")) {
				return true
			}
		} else if pattern == value {
			return true
		}
	}
	return false
}

func scanEntry(rows interface{ Scan(...any) error }) (Entry, error) {
	var e Entry
	err := rows.Scan(&e.ID, &e.SiteID, &e.Network, &e.Kind, &e.Reason, &e.Source, &e.CreatedAt, &e.ExpiresAt, &e.RevokedAt)
	return e, err
}

const entryColumns = `id,site_id,network,kind,reason,source,created_at,expires_at,revoked_at`

func ListEntries(ctx context.Context, database *sql.DB, scope int64, active bool, page int) ([]Entry, int, error) {
	where := "site_id=?"
	args := []any{scope}
	if active {
		where += " AND revoked_at='' AND (expires_at='' OR expires_at>?)"
		args = append(args, time.Now().UTC().Format(time.RFC3339))
	}
	var total int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM gocms_security_entry WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, 50, (page-1)*50)
	rows, err := database.QueryContext(ctx, `SELECT `+entryColumns+` FROM gocms_security_entry WHERE `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []Entry{}
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, e)
	}
	return items, total, rows.Err()
}

func AddEntry(ctx context.Context, database *sql.DB, e Entry) (Entry, error) {
	p, err := Network(e.Network)
	if err != nil {
		return e, err
	}
	e.Network = p.String()
	if e.Kind != "allow" && e.Kind != "deny" {
		return e, errors.New("名单类型无效")
	}
	e.Reason = strings.TrimSpace(e.Reason)
	if e.Reason == "" || len(e.Reason) > 500 {
		return e, errors.New("请填写不超过 500 字节的原因")
	}
	if e.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, e.ExpiresAt)
		if err != nil || !t.After(time.Now()) || t.After(time.Now().Add(365*24*time.Hour)) {
			return e, errors.New("到期时间需在未来一年内")
		}
		e.ExpiresAt = t.UTC().Format(time.RFC3339)
	}
	if e.Source == "" {
		e.Source = "manual"
	}
	e.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return e, err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM gocms_security_entry WHERE revoked_at='' AND (expires_at='' OR expires_at>?)`, e.CreatedAt).Scan(&count); err != nil {
		return e, err
	}
	if count >= 10000 {
		return e, errors.New("有效名单已达到 10000 条上限，请先清理")
	}
	_, err = tx.ExecContext(ctx, `UPDATE gocms_security_entry SET revoked_at=? WHERE site_id=? AND network=? AND kind=? AND revoked_at=''`, e.CreatedAt, e.SiteID, e.Network, e.Kind)
	if err != nil {
		return e, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO gocms_security_entry(site_id,network,kind,reason,source,created_at,expires_at) VALUES(?,?,?,?,?,?,?)`, e.SiteID, e.Network, e.Kind, e.Reason, e.Source, e.CreatedAt, e.ExpiresAt)
	if err != nil {
		return e, err
	}
	e.ID, err = res.LastInsertId()
	if err != nil {
		return e, err
	}
	return e, tx.Commit()
}

func RevokeEntry(ctx context.Context, database *sql.DB, siteID, id int64) (bool, error) {
	res, err := database.ExecContext(ctx, `UPDATE gocms_security_entry SET revoked_at=? WHERE id=? AND site_id=? AND revoked_at=''`, time.Now().UTC().Format(time.RFC3339), id, siteID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func Record(ctx context.Context, database *sql.DB, e Event) error {
	if len(e.Path) > 512 {
		e.Path = e.Path[:512]
	}
	if len(e.Host) > 253 {
		e.Host = e.Host[:253]
	}
	if e.CreatedAt == "" {
		e.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := database.ExecContext(ctx, `INSERT INTO gocms_security_event(site_id,ip,host,path,action,reason,created_at) VALUES(?,?,?,?,?,?,?)`, e.SiteID, e.IP, e.Host, e.Path, e.Action, e.Reason, e.CreatedAt)
	return err
}

func ListEvents(ctx context.Context, database *sql.DB, scope int64, page int) ([]Event, int, error) {
	var total int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM gocms_security_event WHERE site_id=?`, scope).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := database.QueryContext(ctx, `SELECT id,site_id,ip,host,path,action,reason,created_at FROM gocms_security_event WHERE site_id=? ORDER BY id DESC LIMIT 50 OFFSET ?`, scope, (page-1)*50)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.SiteID, &e.IP, &e.Host, &e.Path, &e.Action, &e.Reason, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, e)
	}
	return items, total, rows.Err()
}
