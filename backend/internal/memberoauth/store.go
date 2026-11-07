// Package memberoauth owns platform-wide external login configuration and secrets.
package memberoauth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const CallbackPath = "/api/auth/google/callback"
const Issuer = "https://accounts.google.com"

func Ensure(ctx context.Context, db *sql.DB) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS gocms_member_oauth_config(site_id INTEGER NOT NULL,provider TEXT NOT NULL,enabled INTEGER NOT NULL DEFAULT 0,auto_register INTEGER NOT NULL DEFAULT 1,client_id TEXT NOT NULL DEFAULT '',secret_ciphertext TEXT NOT NULL DEFAULT '',public_url TEXT NOT NULL DEFAULT '',revision TEXT NOT NULL,PRIMARY KEY(site_id,provider))`,
		`CREATE TABLE IF NOT EXISTS gocms_member_identity(site_id INTEGER NOT NULL,user_id INTEGER NOT NULL,provider TEXT NOT NULL,issuer TEXT NOT NULL,subject TEXT NOT NULL,created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,last_login_at TEXT,PRIMARY KEY(site_id,provider,issuer,subject),UNIQUE(site_id,user_id,provider),FOREIGN KEY(user_id) REFERENCES gocms_user(id) ON DELETE CASCADE)`,
		`CREATE TABLE IF NOT EXISTS gocms_member_oauth_state(state_hash TEXT PRIMARY KEY,browser_hash TEXT NOT NULL,site_id INTEGER NOT NULL,revision TEXT NOT NULL,nonce TEXT NOT NULL,verifier TEXT NOT NULL,next_path TEXT NOT NULL,user_id INTEGER NOT NULL DEFAULT 0,session_hash TEXT NOT NULL DEFAULT '',expires_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS gocms_platform_oauth_config(provider TEXT PRIMARY KEY,enabled INTEGER NOT NULL DEFAULT 0,auto_register INTEGER NOT NULL DEFAULT 1,client_id TEXT NOT NULL DEFAULT '',secret_ciphertext TEXT NOT NULL DEFAULT '',secret_scope INTEGER NOT NULL DEFAULT 0,revision TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS gocms_platform_identity(user_id INTEGER NOT NULL,provider TEXT NOT NULL,issuer TEXT NOT NULL,subject TEXT NOT NULL,created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,last_login_at TEXT,PRIMARY KEY(provider,issuer,subject),UNIQUE(user_id,provider),FOREIGN KEY(user_id) REFERENCES gocms_user(id) ON DELETE CASCADE)`,
		`CREATE INDEX IF NOT EXISTS idx_member_oauth_expiry ON gocms_member_oauth_state(expires_at)`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	var hasOrigin int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('gocms_member_oauth_state') WHERE name='origin'`).Scan(&hasOrigin); err != nil {
		return err
	}
	if hasOrigin == 0 {
		if _, err := db.ExecContext(ctx, `ALTER TABLE gocms_member_oauth_state ADD COLUMN origin TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	var hasProvider int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('gocms_member_oauth_state') WHERE name='provider'`).Scan(&hasProvider); err != nil {
		return err
	}
	if hasProvider == 0 {
		if _, err := db.ExecContext(ctx, `ALTER TABLE gocms_member_oauth_state ADD COLUMN provider TEXT NOT NULL DEFAULT 'google'`); err != nil {
			return err
		}
	}
	// Adopt only an unambiguous legacy configuration. Preserve its encryption scope
	// so migration does not require decrypting or exposing the existing secret.
	_, err := db.ExecContext(ctx, `INSERT INTO gocms_platform_oauth_config(provider,enabled,auto_register,client_id,secret_ciphertext,secret_scope,revision)
 SELECT provider,enabled,auto_register,client_id,secret_ciphertext,site_id,revision FROM gocms_member_oauth_config
 WHERE provider='google' AND (client_id<>'' OR secret_ciphertext<>'')
 AND (SELECT COUNT(*) FROM gocms_member_oauth_config WHERE provider='google' AND (client_id<>'' OR secret_ciphertext<>''))=1
 ON CONFLICT(provider) DO NOTHING`)
	return err
}

type Config struct {
	Provider     string `json:"-"`
	SiteID       int64  `json:"-"`
	SecretScope  int64  `json:"-"`
	Enabled      bool   `json:"enabled"`
	AutoRegister bool   `json:"auto_register"`
	ClientID     string `json:"client_id"`
	PublicURL    string `json:"-"`
	SecretSet    bool   `json:"secret_set"`
	Revision     string `json:"revision"`
	Encrypted    string `json:"-"`
}

func (c Config) Callback() string {
	if c.PublicURL == "" {
		return ""
	}
	return c.PublicURL + "/api/auth/" + c.ProviderName() + "/callback"
}

func (c Config) ProviderName() string {
	if c.Provider == "" {
		return "google"
	}
	return c.Provider
}
func (c Config) Issuer() string {
	if c.ProviderName() == "facebook" {
		return "https://www.facebook.com"
	}
	return Issuer
}
func SecretScope(c Config) int64 {
	// Negative scopes are reserved for platform providers; legacy site scopes
	// and the original Google platform scope remain nonnegative.
	if c.ProviderName() == "facebook" {
		return -1
	}
	return c.SecretScope
}

// Load returns the shared configuration with the requesting site's context.
func Load(ctx context.Context, db *sql.DB, site int64) (Config, error) {
	return LoadProvider(ctx, db, site, "google")
}
func LoadProvider(ctx context.Context, db *sql.DB, site int64, provider string) (Config, error) {
	c := Config{SiteID: site, AutoRegister: true, Provider: provider}
	err := db.QueryRowContext(ctx, `SELECT enabled,auto_register,client_id,secret_ciphertext,secret_scope,revision FROM gocms_platform_oauth_config WHERE provider=?`, provider).Scan(&c.Enabled, &c.AutoRegister, &c.ClientID, &c.Encrypted, &c.SecretScope, &c.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return c, nil
	}
	c.SecretSet = c.Encrypted != ""
	return c, err
}
func Validate(c Config) error {
	if len(c.ClientID) > 512 || len(c.PublicURL) > 2048 {
		return errors.New("配置内容过长")
	}
	if c.PublicURL != "" {
		u, err := url.Parse(c.PublicURL)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || strings.ContainsAny(c.PublicURL, "?#") || u.Path != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"))) {
			return errors.New("公开地址必须是 HTTPS 站点地址，不含路径、参数或账号；本地开发可使用 HTTP localhost")
		}
	}
	if c.ProviderName() == "facebook" && c.ClientID != "" {
		for _, ch := range c.ClientID {
			if ch < '0' || ch > '9' {
				return errors.New("Facebook App ID 必须为数字")
			}
		}
	}
	if c.Enabled && (c.ClientID == "" || (c.ProviderName() == "google" && !strings.HasSuffix(c.ClientID, ".apps.googleusercontent.com")) || c.Encrypted == "") {
		return errors.New("开启前请填写客户端 ID 和密钥")
	}
	return nil
}
func Random() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func Hash(v string) string { b := sha256.Sum256([]byte(v)); return hex.EncodeToString(b[:]) }

// Vault is immutable after creation. The key is persisted outside public roots.
type Vault struct{ aead cipher.AEAD }

func NewVault(key string) (*Vault, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(key))
	if err != nil || len(b) != 32 {
		return nil, errors.New("密钥必须为 32 字节随机数据的 Base64 编码")
	}
	block, err := aes.NewCipher(b)
	if err != nil {
		return nil, err
	}
	a, err := cipher.NewGCM(block)
	return &Vault{a}, err
}
func aad(site int64) []byte { return []byte(fmt.Sprintf("member-oauth:%d:google", site)) }
func (v *Vault) Seal(site int64, secret string) (string, error) {
	if v == nil {
		return "", errors.New("请先初始化登录加密密钥")
	}
	n := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(n); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(v.aead.Seal(n, n, []byte(secret), aad(site))), nil
}
func (v *Vault) Open(site int64, encrypted string) (string, error) {
	if v == nil {
		return "", errors.New("登录加密密钥未配置")
	}
	b, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil || len(b) < v.aead.NonceSize() {
		return "", errors.New("无效登录密文")
	}
	n := v.aead.NonceSize()
	out, err := v.aead.Open(nil, b[:n], b[n:], aad(site))
	return string(out), err
}
