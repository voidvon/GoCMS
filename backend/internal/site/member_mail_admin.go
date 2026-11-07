package site

import (
	"encoding/json"
	"net/http"
	"strings"

	"gocms/internal/membermail"
	"gocms/internal/memberoauth"
)

func (s *Server) adminMail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.database == nil {
		loginAdminError(w, 503, "数据库不可用")
		return
	}
	if !memberRequest(w, r) {
		return
	}
	s.oauthMu.RLock()
	defer s.oauthMu.RUnlock()
	c, e := membermail.Load(r.Context(), s.database)
	if e != nil {
		loginAdminError(w, 500, "读取邮件配置失败")
		return
	}
	if r.URL.Path == "/api/admin/mail/test" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var in struct {
			Email string `json:"email"`
		}
		if memberDecode(r, &in) != nil || !membermail.Address(in.Email) {
			loginAdminError(w, 400, "请填写有效的测试收件邮箱")
			return
		}
		check := c
		check.Enabled = true
		if membermail.Validate(check) != nil {
			loginAdminError(w, 400, "请先保存完整 SMTP 配置")
			return
		}
		password := ""
		if c.Encrypted != "" {
			password, e = s.oauthVault.Open(membermail.SecretScope, c.Encrypted)
			if e != nil {
				loginAdminError(w, 409, "请先恢复凭据加密密钥")
				return
			}
		}
		if !s.memberAttempt(w, r, "mail-test", "platform", 5) {
			return
		}
		send := s.mailSend
		if send == nil {
			send = membermail.Send
		}
		if e = send(r.Context(), c, password, in.Email, "SMTP test", "SMTP configuration test succeeded."); e != nil {
			loginAdminError(w, 502, "测试邮件发送失败，请检查 SMTP 主机、端口、TLS、认证信息及发件邮箱权限")
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	switch r.Method {
	case http.MethodGet:
		readable := c.Encrypted == ""
		if !readable {
			_, e = s.oauthVault.Open(membermail.SecretScope, c.Encrypted)
			readable = e == nil
		}
		writeJSON(w, 200, map[string]any{"config": c, "key_configured": s.oauthVault != nil, "key_can_initialize": s.oauthKeyPath != "" && s.oauthVault == nil, "secret_readable": readable})
	case http.MethodPut:
		var in struct {
			membermail.Config
			Password string `json:"password"`
		}
		if memberDecode(r, &in) != nil || len(in.Password) > 4096 {
			loginAdminError(w, 400, "无效邮件配置")
			return
		}
		next := in.Config
		next.Host = strings.TrimSpace(next.Host)
		next.From = strings.TrimSpace(next.From)
		next.SupportEmail = strings.TrimSpace(next.SupportEmail)
		next.Username = strings.TrimSpace(next.Username)
		next.Name = strings.TrimSpace(next.Name)
		next.Encrypted = c.Encrypted
		if in.Password != "" {
			next.Encrypted, e = s.oauthVault.Seal(membermail.SecretScope, in.Password)
			if e != nil {
				loginAdminError(w, 409, "请先生成或恢复凭据加密密钥")
				return
			}
		}
		if e = membermail.Validate(next); e != nil {
			loginAdminError(w, 400, e.Error())
			return
		}
		if next.Enabled && next.Encrypted != "" {
			if _, e = s.oauthVault.Open(membermail.SecretScope, next.Encrypted); e != nil {
				loginAdminError(w, 409, "请先恢复能够解密 SMTP 密码的密钥")
				return
			}
		}
		rev, e := memberoauth.Random()
		if e != nil {
			loginAdminError(w, 500, "保存失败")
			return
		}
		next.Revision = rev
		next.PasswordSet = next.Encrypted != ""
		raw, e := json.Marshal(next)
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
		result, e := tx.ExecContext(r.Context(), `INSERT INTO gocms_platform_mail_config(id,config,secret_ciphertext,revision) SELECT 1,?,?,? WHERE ?='' ON CONFLICT(id) DO NOTHING`, string(raw), next.Encrypted, rev, in.Revision)
		if e == nil {
			n, _ := result.RowsAffected()
			if n == 0 {
				result, e = tx.ExecContext(r.Context(), `UPDATE gocms_platform_mail_config SET config=?,secret_ciphertext=?,revision=? WHERE id=1 AND revision=?`, string(raw), next.Encrypted, rev, in.Revision)
				if e == nil {
					n, _ = result.RowsAffected()
					if n != 1 {
						loginAdminError(w, 409, "邮件配置已变化，请刷新后重试")
						return
					}
				}
			}
		}
		if e == nil && !next.Enabled {
			_, e = tx.ExecContext(r.Context(), `DELETE FROM gocms_member_password_reset WHERE channel='email'`)
		}
		if e == nil {
			e = tx.Commit()
		}
		if e != nil {
			loginAdminError(w, 500, "保存邮件配置失败")
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	default:
		methodNotAllowed(w)
	}
}
