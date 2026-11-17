package site

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"gocms/internal/nginxguard"
	"gocms/internal/security"
)

func (s *Server) StartSecurityMaintenance(ctx context.Context) {
	if s.security != nil {
		go s.security.Run(ctx)
	}
}

func (s *Server) authorizeSecurity(w http.ResponseWriter, r *http.Request, u *AdminUser) bool {
	route := strings.ToLower(r.URL.Path)
	platform := strings.HasPrefix(route, "/api/admin/security/platform") || strings.HasPrefix(route, "/api/admin/security/nginx") || r.URL.Query().Get("scope") == "global"
	if platform {
		if u.IsSuper {
			return true
		}
		writeJSON(w, 403, map[string]string{"error": "平台安全设置和全局名单需要超级管理员权限"})
		return false
	}
	siteID, err := s.resolveSiteID(r, u)
	if err == nil && u.HasPermissionInSite(siteID, "security") && (r.Method == http.MethodGet || u.HasPermissionInSite(siteID, "security.manage")) {
		return true
	}
	writeJSON(w, 403, map[string]string{"error": "当前用户没有此站点的安全防护权限"})
	return false
}

func (s *Server) securityScope(r *http.Request) (int64, error) {
	switch r.URL.Query().Get("scope") {
	case "global":
		if u := s.currentAdmin(r); u != nil && u.IsSuper {
			return 0, nil
		}
		return 0, fmt.Errorf("无权访问全局名单")
	case "", "site":
		return s.resolveSiteID(r, s.currentAdmin(r))
	default:
		return 0, fmt.Errorf("安全规则范围无效")
	}
}

func (s *Server) adminSecurity(w http.ResponseWriter, r *http.Request) {
	if s.security == nil {
		writeJSON(w, 503, map[string]string{"error": "安全防护不可用"})
		return
	}
	if !memberRequest(w, r) {
		return
	}
	endpoint := strings.TrimSuffix(strings.TrimPrefix(strings.ToLower(r.URL.Path), "/api/admin/security"), "/")
	if endpoint == "/platform" {
		switch r.Method {
		case http.MethodGet:
			p, err := security.LoadPlatform(r.Context(), s.database)
			if err != nil {
				accountError(w, err)
				return
			}
			writeJSON(w, 200, p)
		case http.MethodPut:
			var p security.Platform
			if err := decodeRequest(r, &p); err != nil {
				accountInputError(w, "请求格式无效")
				return
			}
			if err := security.ValidatePlatform(&p); err != nil {
				accountInputError(w, err.Error())
				return
			}
			old, err := security.LoadPlatform(r.Context(), s.database)
			if err != nil {
				accountError(w, err)
				return
			}
			changed := old.NginxMode != p.NginxMode || old.SocketPath != p.SocketPath || old.NginxBinary != p.NginxBinary || old.NginxConfig != p.NginxConfig || old.ServerConfig != p.ServerConfig
			status := s.security.Status(r.Context())
			if changed && status.Digest != "" && status.State != "disabled" {
				accountInputError(w, "请先关闭 Nginx 同步并确认清理成功，再更换管理方式或配置路径")
				return
			}
			if err := security.SaveConfig(r.Context(), s.database, 0, p); err != nil {
				accountError(w, err)
				return
			}
			s.security.Invalidate()
			writeJSON(w, 200, map[string]bool{"ok": true})
		default:
			methodNotAllowed(w)
		}
		return
	}
	if endpoint == "/nginx" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		h, server, err := s.security.Preview(r.Context())
		if err != nil {
			accountError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"status": s.security.Status(r.Context()), "http_config": h, "server_config": server, "runtime_user": nginxguard.ExecutionUser()})
		return
	}
	if endpoint == "/nginx/sync" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		if err := s.security.Sync(r.Context()); err != nil {
			var syncError *security.SyncError
			if errors.As(err, &syncError) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			} else {
				accountError(w, err)
			}
			return
		}
		writeJSON(w, 200, s.security.Status(r.Context()))
		return
	}
	if endpoint == "/nginx/install" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		s.securityInstallBundle(w, r)
		return
	}
	scope, err := s.securityScope(r)
	if err != nil {
		writeJSON(w, 403, map[string]string{"error": err.Error()})
		return
	}
	page := positiveInt(r.URL.Query().Get("page"), 1)
	if page > 100000 {
		page = 100000
	}
	switch {
	case endpoint == "/policy":
		if scope == 0 {
			accountInputError(w, "自动检测规则需绑定具体站点")
			return
		}
		switch r.Method {
		case http.MethodGet:
			p, err := security.LoadPolicy(r.Context(), s.database, scope)
			if err != nil {
				accountError(w, err)
				return
			}
			writeJSON(w, 200, p)
		case http.MethodPut:
			var p security.Policy
			if err := decodeRequest(r, &p); err != nil {
				accountInputError(w, "请求格式无效")
				return
			}
			if err := security.ValidatePolicy(&p); err != nil {
				accountInputError(w, err.Error())
				return
			}
			old, err := security.LoadPolicy(r.Context(), s.database, scope)
			if err != nil {
				accountError(w, err)
				return
			}
			if u := s.currentAdmin(r); u == nil || (!u.IsSuper && (p.BanScope == "global" || (old.Enabled && old.BanScope == "global"))) {
				writeJSON(w, 403, map[string]string{"error": "全平台自动封禁需由超级管理员配置"})
				return
			}
			if err := security.SaveConfig(r.Context(), s.database, scope, p); err != nil {
				accountError(w, err)
				return
			}
			s.security.Invalidate()
			writeJSON(w, 200, map[string]bool{"ok": true})
		default:
			methodNotAllowed(w)
		}
	case endpoint == "/entries":
		switch r.Method {
		case http.MethodGet:
			items, total, err := security.ListEntries(r.Context(), s.database, scope, r.URL.Query().Get("history") != "true", page)
			if err != nil {
				accountError(w, err)
				return
			}
			writeJSON(w, 200, map[string]any{"items": items, "total": total, "page_size": 50})
		case http.MethodPost:
			var in struct {
				Network   string `json:"network"`
				Kind      string `json:"kind"`
				Reason    string `json:"reason"`
				ExpiresAt string `json:"expires_at"`
			}
			if err := decodeRequest(r, &in); err != nil {
				accountInputError(w, "请求格式无效")
				return
			}
			network, err := security.Network(in.Network)
			if err != nil {
				accountInputError(w, err.Error())
				return
			}
			if ip, err := netip.ParseAddr(clientIP(r)); err == nil && in.Kind == "deny" && network.Contains(ip) {
				accountInputError(w, "该规则包含当前操作者 IP，请先检查封禁范围")
				return
			}
			e, err := security.AddEntry(r.Context(), s.database, security.Entry{SiteID: scope, Network: in.Network, Kind: in.Kind, Reason: in.Reason, ExpiresAt: in.ExpiresAt})
			if err != nil {
				accountInputError(w, err.Error())
				return
			}
			s.security.Invalidate()
			if err := security.Record(r.Context(), s.database, security.Event{SiteID: scope, IP: e.Network, Action: e.Kind, Reason: e.Reason}); err != nil {
				accountError(w, err)
				return
			}
			writeJSON(w, 201, e)
		default:
			methodNotAllowed(w)
		}
	case strings.HasPrefix(endpoint, "/entries/"):
		if r.Method != http.MethodDelete {
			methodNotAllowed(w)
			return
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(endpoint, "/entries/"), 10, 64)
		if err != nil || id <= 0 {
			accountInputError(w, "名单 ID 无效")
			return
		}
		ok, err := security.RevokeEntry(r.Context(), s.database, scope, id)
		if err != nil {
			accountError(w, err)
			return
		}
		if !ok {
			writeJSON(w, 404, map[string]string{"error": "名单不存在或已解除"})
			return
		}
		s.security.Invalidate()
		if err := security.Record(r.Context(), s.database, security.Event{SiteID: scope, Action: "revoke", Reason: fmt.Sprintf("移除名单 #%d", id)}); err != nil {
			accountError(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	case endpoint == "/events":
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		items, total, err := security.ListEvents(r.Context(), s.database, scope, page)
		if err != nil {
			accountError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"items": items, "total": total, "page_size": 50})
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) securityInstallBundle(w http.ResponseWriter, r *http.Request) {
	p, err := security.LoadPlatform(r.Context(), s.database)
	if err != nil {
		accountError(w, err)
		return
	}
	if err := security.ValidatePlatform(&p); err != nil {
		accountInputError(w, err.Error())
		return
	}
	c := nginxguard.Config{SocketPath: p.SocketPath, NginxBinary: p.NginxBinary, NginxConfig: p.NginxConfig, ServerConfig: p.ServerConfig, AgentUser: p.AgentUser}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		accountError(w, err)
		return
	}
	files := map[string][]byte{
		"agent.json": raw,
		"install.sh": []byte("#!/bin/sh\nset -eu\nif [ \"$#\" -ne 1 ]; then echo 'Usage: sudo sh install.sh /absolute/path/to/site'; exit 1; fi\ncd -- \"$(dirname -- \"$0\")\"\nexec \"$1\" security-agent install -config \"$(pwd)/agent.json\"\n"),
		"README.txt": []byte("GoCMS Nginx 接入包\n\n1. 确认 agent.json 中的路径和 GoCMS 系统运行用户正确。\n2. 将解压后的文件放在服务器上，以 root 执行：sh install.sh /绝对路径/site\n3. 安装器会备份原 Nginx 主配置和站点配置，添加 include，检查并平滑重载，安装 systemd 同步服务。\n4. 返回后台开启 Nginx 同步，点击立即同步。\n\n日常规则、名单与解封均在后台管理；GoCMS 无需 root 或宝塔 API 密钥。\n仅支持 Linux/systemd；Nginx 主配置需包含一个顶层 http 块，所选站点配置需包含一个顶层 server 块。\n使用 CDN 时，需先在 Nginx 配置受信任的真实 IP 来源；本接入包不会自动信任代理头。\n回退：停止 gocms-security-agent 服务，从 /etc/gocms-security/ 中的 nginx.conf.before 和 server.conf.before 恢复对应配置，检查通过后重载。\n"),
	}
	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	for _, name := range []string{"agent.json", "install.sh", "README.txt"} {
		f, err := archive.Create(name)
		if err != nil {
			accountError(w, err)
			return
		}
		if _, err = f.Write(files[name]); err != nil {
			accountError(w, err)
			return
		}
	}
	if err := archive.Close(); err != nil {
		accountError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="gocms-security-install.zip"`)
	_, _ = w.Write(buf.Bytes())
}
