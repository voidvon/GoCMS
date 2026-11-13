package site

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gocms/internal/memberoauth"
)

func (s *Server) ConfigureMemberLoginKey(data string) error {
	if strings.TrimSpace(data) == "" {
		return errors.New("登录密钥数据目录为空")
	}
	if err := os.MkdirAll(data, 0700); err != nil {
		return err
	}
	root, err := filepath.EvalSymlinks(data)
	if err != nil {
		return err
	}
	p, err := filepath.Abs(filepath.Join(root, "member-login.key"))
	if err != nil {
		return err
	}
	for _, public := range []string{s.siteRoot, s.frontendRoot, s.assetsRoot, s.storefrontRoot} {
		if public == "" {
			continue
		}
		abs, e := filepath.Abs(public)
		if e != nil {
			return e
		}
		if resolved, e := filepath.EvalSymlinks(abs); e == nil {
			abs = resolved
		}
		rel, e := filepath.Rel(abs, p)
		if e != nil {
			return e
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return errors.New("登录密钥必须位于公开目录之外")
		}
	}
	s.oauthMu.Lock()
	defer s.oauthMu.Unlock()
	s.oauthKeyPath = p
	info, err := os.Lstat(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 256 {
		return errors.New("登录密钥文件必须为私有普通文件（0600）")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	v, err := memberoauth.NewVault(string(b))
	if err != nil {
		return err
	}
	if err = s.validateLoginKey(context.Background(), v); err != nil {
		return err
	}
	s.oauthVault = v
	return nil
}
func (s *Server) validateLoginKey(ctx context.Context, v *memberoauth.Vault) error {
	if s.database == nil {
		return nil
	}
	rows, err := s.database.QueryContext(ctx, `SELECT site_id,secret_ciphertext FROM gocms_member_oauth_config WHERE secret_ciphertext<>'' UNION ALL SELECT CASE WHEN provider='facebook' THEN -1 WHEN provider='microsoft' THEN -3 ELSE secret_scope END,secret_ciphertext FROM gocms_platform_oauth_config WHERE secret_ciphertext<>'' UNION ALL SELECT -2,secret_ciphertext FROM gocms_platform_mail_config WHERE secret_ciphertext<>''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var site int64
		var enc string
		if err = rows.Scan(&site, &enc); err != nil {
			return err
		}
		if _, err = v.Open(site, enc); err != nil {
			return errors.New("密钥无法解密已有登录配置，请恢复原密钥")
		}
	}
	return rows.Err()
}
func (s *Server) authorizeMemberLogin(w http.ResponseWriter, u *AdminUser) bool {
	if u.IsSuper {
		return true
	}
	writeJSON(w, 403, map[string]string{"error": "平台安全配置仅允许超级管理员通过后台管理"})
	return false
}
func loginAdminError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
func (s *Server) adminMemberLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if provider := r.URL.Query().Get("provider"); provider != "" && !memberoauth.Supported(provider) {
		loginAdminError(w, 400, "不支持的登录提供商")
		return
	}
	if s.database == nil {
		loginAdminError(w, 503, "数据库不可用")
		return
	}
	if r.URL.Path == "/api/admin/member-login/test" {
		s.startGoogle(w, r, true)
		return
	}
	if !memberRequest(w, r) {
		return
	}
	if r.URL.Path == "/api/admin/member-login/key/export" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		s.oauthMu.RLock()
		defer s.oauthMu.RUnlock()
		if s.oauthVault == nil {
			loginAdminError(w, 409, "登录加密密钥尚未配置")
			return
		}
		info, e := os.Lstat(s.oauthKeyPath)
		if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 256 {
			loginAdminError(w, 503, "密钥备份不可用")
			return
		}
		b, e := os.ReadFile(s.oauthKeyPath)
		if e != nil {
			loginAdminError(w, 503, "密钥备份不可用")
			return
		}
		candidate, e := memberoauth.NewVault(string(b))
		if e != nil {
			loginAdminError(w, 503, "密钥备份不可用")
			return
		}
		probe, e := s.oauthVault.Seal(0, "verify")
		if e != nil {
			loginAdminError(w, 503, "密钥备份不可用")
			return
		}
		if _, e = candidate.Open(0, probe); e != nil {
			loginAdminError(w, 503, "密钥备份不可用")
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="gocms-member-login.key"`)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(b)
		return
	}
	if r.URL.Path == "/api/admin/member-login/key" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var in struct {
			Mode string `json:"mode"`
			Key  string `json:"key"`
		}
		if memberDecode(r, &in) != nil {
			loginAdminError(w, 400, "无效配置")
			return
		}
		s.oauthMu.Lock()
		defer s.oauthMu.Unlock()
		if s.oauthVault != nil || s.oauthKeyPath == "" {
			loginAdminError(w, 409, "密钥已配置或持久化目录不可用，不能覆盖")
			return
		}
		key := strings.TrimSpace(in.Key)
		if in.Mode == "generate" && key == "" {
			var count int
			if err := s.database.QueryRowContext(r.Context(), `SELECT (SELECT COUNT(*) FROM gocms_member_oauth_config WHERE secret_ciphertext<>'')+(SELECT COUNT(*) FROM gocms_platform_oauth_config WHERE secret_ciphertext<>'')+(SELECT COUNT(*) FROM gocms_platform_mail_config WHERE secret_ciphertext<>'')`).Scan(&count); err != nil {
				loginAdminError(w, 500, "无法检查已有配置")
				return
			}
			if count > 0 {
				loginAdminError(w, 409, "已有登录配置，请导入原密钥")
				return
			}
			b := make([]byte, 32)
			if _, err := rand.Read(b); err != nil {
				loginAdminError(w, 500, "密钥生成失败")
				return
			}
			key = base64.StdEncoding.EncodeToString(b)
		} else if in.Mode != "import" || key == "" {
			loginAdminError(w, 400, "请选择生成或导入密钥")
			return
		}
		v, err := memberoauth.NewVault(key)
		if err != nil {
			loginAdminError(w, 400, err.Error())
			return
		}
		if err = s.validateLoginKey(r.Context(), v); err != nil {
			loginAdminError(w, 400, err.Error())
			return
		}
		if err = savePrivateKey(s.oauthKeyPath, key); err != nil {
			loginAdminError(w, 503, "无法保存登录密钥，未覆盖现有文件")
			return
		}
		s.oauthVault = v
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}

	s.oauthMu.RLock()
	defer s.oauthMu.RUnlock()
	if r.URL.Path == "/api/admin/member-login/migrate" {
		s.migrateMemberLogin(w, r)
		return
	}
	if r.URL.Path != "/api/admin/member-login" {
		http.NotFound(w, r)
		return
	}
	c, err := memberoauth.LoadProvider(r.Context(), s.database, 0, loginProvider(r))
	if err != nil {
		loginAdminError(w, 500, "读取平台登录配置失败")
		return
	}
	switch r.Method {
	case http.MethodGet:
		ready := false
		if c.Encrypted != "" {
			_, e := s.oauthVault.Open(memberoauth.SecretScope(c), c.Encrypted)
			ready = e == nil
		}
		callbacks, e := s.memberLoginCallbacks(r.Context())
		if e != nil {
			loginAdminError(w, 500, "读取站点回调地址失败")
			return
		}
		for i := range callbacks {
			callbacks[i].URL = callbacks[i].Origin + "/api/auth/" + c.ProviderName() + "/callback"
		}
		legacy := []legacyMemberLogin{}
		if c.Revision == "" && c.ProviderName() == "google" {
			legacy, e = s.legacyMemberLogins(r.Context())
			if e != nil {
				loginAdminError(w, 500, "读取旧登录配置失败")
				return
			}
		}
		writeJSON(w, 200, map[string]any{"config": c, "callbacks": callbacks, "legacy_configs": legacy, "key_configured": s.oauthVault != nil, "key_can_initialize": s.oauthKeyPath != "" && s.oauthVault == nil, "secret_readable": ready})
		return
	case http.MethodPut:
		var in struct {
			Enabled      bool   `json:"enabled"`
			AutoRegister bool   `json:"auto_register"`
			ClientID     string `json:"client_id"`
			Secret       string `json:"secret"`
			Revision     string `json:"revision"`
		}
		if memberDecode(r, &in) != nil {
			loginAdminError(w, 400, "无效配置")
			return
		}
		c.Enabled = in.Enabled
		c.AutoRegister = in.AutoRegister
		c.ClientID = strings.TrimSpace(in.ClientID)
		if len(in.Secret) > 4096 {
			loginAdminError(w, 400, "密钥过长")
			return
		}
		if in.Secret != "" && strings.TrimSpace(in.Secret) == "" {
			loginAdminError(w, 400, "Client Secret 不能为空白字符")
			return
		}
		if in.Secret != "" {
			c.SecretScope = 0
			c.Encrypted, err = s.oauthVault.Seal(memberoauth.SecretScope(c), strings.TrimSpace(in.Secret))
			if err != nil {
				loginAdminError(w, 409, err.Error())
				return
			}
		}
		if err = memberoauth.Validate(c); err != nil {
			loginAdminError(w, 400, err.Error())
			return
		}
		if c.Enabled {
			if _, err = s.oauthVault.Open(memberoauth.SecretScope(c), c.Encrypted); err != nil {
				loginAdminError(w, 409, "请先恢复能够解密登录配置的密钥")
				return
			}
		}
		revision, e := memberoauth.Random()
		if e != nil {
			loginAdminError(w, 500, "保存失败")
			return
		}
		tx, e := s.database.BeginTx(r.Context(), nil)
		if e != nil {
			loginAdminError(w, 500, "保存失败")
			return
		}
		defer tx.Rollback()
		result, e := tx.ExecContext(r.Context(), `INSERT INTO gocms_platform_oauth_config(provider,enabled,auto_register,client_id,secret_ciphertext,secret_scope,revision) SELECT ?,?,?,?,?,?,? WHERE ?='' ON CONFLICT(provider) DO NOTHING`, c.ProviderName(), c.Enabled, c.AutoRegister, c.ClientID, c.Encrypted, c.SecretScope, revision, in.Revision)
		if e == nil {
			var n int64
			n, e = result.RowsAffected()
			if e == nil && n == 0 {
				result, e = tx.ExecContext(r.Context(), `UPDATE gocms_platform_oauth_config SET enabled=?,auto_register=?,client_id=?,secret_ciphertext=?,secret_scope=?,revision=? WHERE provider=? AND revision=?`, c.Enabled, c.AutoRegister, c.ClientID, c.Encrypted, c.SecretScope, revision, c.ProviderName(), in.Revision)
				if e == nil {
					n, e = result.RowsAffected()
					if e == nil && n == 0 {
						loginAdminError(w, 409, "平台配置已被其他管理员修改，请刷新后重试")
						return
					}
				}
			}
		}
		if e == nil {
			_, e = tx.ExecContext(r.Context(), `DELETE FROM gocms_member_oauth_state WHERE provider=?`, c.ProviderName())
		}
		if e == nil {
			e = tx.Commit()
		}
		if e != nil {
			loginAdminError(w, 500, "保存平台登录配置失败")
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	default:
		methodNotAllowed(w)
	}
}

type legacyMemberLogin struct {
	SiteID   int64  `json:"site_id"`
	SiteName string `json:"site_name"`
	ClientID string `json:"client_id"`
	Revision string `json:"revision"`
}

func (s *Server) legacyMemberLogins(ctx context.Context) ([]legacyMemberLogin, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT c.site_id,COALESCE(s.name,''),c.client_id,c.revision FROM gocms_member_oauth_config c LEFT JOIN gocms_site s ON s.id=c.site_id WHERE c.provider='google' AND (c.client_id<>'' OR c.secret_ciphertext<>'') ORDER BY c.site_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []legacyMemberLogin{}
	for rows.Next() {
		var item legacyMemberLogin
		if err = rows.Scan(&item.SiteID, &item.SiteName, &item.ClientID, &item.Revision); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
func (s *Server) migrateMemberLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var in struct {
		SiteID   int64  `json:"site_id"`
		Revision string `json:"revision"`
	}
	if memberDecode(r, &in) != nil || in.SiteID <= 0 {
		loginAdminError(w, 400, "请选择需要沿用的旧配置")
		return
	}
	tx, err := s.database.BeginTx(r.Context(), nil)
	if err != nil {
		loginAdminError(w, 500, "迁移失败")
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `INSERT INTO gocms_platform_oauth_config(provider,enabled,auto_register,client_id,secret_ciphertext,secret_scope,revision) SELECT provider,enabled,auto_register,client_id,secret_ciphertext,site_id,revision FROM gocms_member_oauth_config WHERE site_id=? AND provider='google' AND revision=? ON CONFLICT(provider) DO NOTHING`, in.SiteID, in.Revision)
	if err != nil {
		loginAdminError(w, 500, "迁移失败")
		return
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		loginAdminError(w, 409, "平台已有配置或旧配置已变化，请刷新后重试")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM gocms_member_oauth_state`); err == nil {
		err = tx.Commit()
	}
	if err != nil {
		loginAdminError(w, 500, "迁移失败")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
