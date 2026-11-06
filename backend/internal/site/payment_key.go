package site

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gocms/internal/payment"
)

// ConfigurePaymentKey must run before serving requests or starting maintenance.
// The key stays outside both the database and all publicly served directories.
func (s *Server) ConfigurePaymentKey(data string) error {
	s.paymentKeyMu.Lock()
	defer s.paymentKeyMu.Unlock()
	if strings.TrimSpace(data) == "" {
		return errors.New("payment key data directory is empty")
	}
	if err := os.MkdirAll(data, 0700); err != nil {
		return fmt.Errorf("prepare payment key directory: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(data)
	if err != nil {
		return err
	}
	path, err := filepath.Abs(filepath.Join(resolved, "payment.key"))
	if err != nil {
		return err
	}
	// A bad -data/-root combination must never expose the master key as a file.
	for _, root := range []string{s.siteRoot, s.frontendRoot, s.assetsRoot} {
		if root == "" {
			continue
		}
		absolute, err := filepath.Abs(root)
		if err != nil {
			return err
		}
		if resolvedRoot, err := filepath.EvalSymlinks(absolute); err == nil {
			absolute = resolvedRoot
		}
		rel, err := filepath.Rel(absolute, path)
		if err != nil {
			return err
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return errors.New("payment key data directory must be outside public resource directories")
		}
	}
	s.paymentKeyPath = path
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if s.paymentKeyEnvironmentError != nil {
			return s.paymentKeyEnvironmentError
		}
		if s.commerce.Cipher.Configured() {
			return s.validatePaymentKey(context.Background(), s.commerce.Cipher)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("read payment key file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 256 {
		return errors.New("payment key file must be a regular private file (0600)")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read payment key file: %w", err)
	}
	key := strings.TrimSpace(string(raw))
	cipher, err := payment.NewCipher(key)
	if err != nil {
		return err
	}
	if err := s.validatePaymentKey(context.Background(), cipher); err != nil {
		return err
	}
	// Startup only: a persisted key takes precedence after admin takeover.
	s.commerce.Cipher = cipher
	s.paymentKeySource = "file"
	return nil
}

func (s *Server) validatePaymentKey(ctx context.Context, cipher *payment.Cipher) error {
	if s.database == nil {
		return nil
	}
	rows, err := s.database.QueryContext(ctx, `SELECT site_id,public_id,secret_ciphertext FROM gocms_payment_account`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var site int64
		var publicID, encrypted string
		if err := rows.Scan(&site, &publicID, &encrypted); err != nil {
			return err
		}
		if _, err := cipher.Open(encrypted, fmt.Sprintf("%d:%s", site, publicID)); err != nil {
			return errors.New("支付密钥无法解密已有渠道，请恢复原密钥")
		}
	}
	return rows.Err()
}

func (s *Server) paymentStatus(w http.ResponseWriter, r *http.Request) {
	s.paymentKeyMu.Lock()
	defer s.paymentKeyMu.Unlock()
	var count int
	if err := s.database.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM gocms_payment_account`).Scan(&count); err != nil {
		commerceError(w, err)
		return
	}
	configured := s.commerce.Cipher.Configured()
	source := s.paymentKeySource
	if source == "" {
		source = "none"
	}
	u := s.currentAdmin(r)
	writeJSON(w, 200, map[string]any{"key_configured": configured, "key_source": source, "key_can_initialize": u != nil && u.IsSuper && !configured && s.paymentKeyPath != "", "key_requires_restore": !configured && count > 0, "key_can_export": u != nil && u.IsSuper && configured, "key_can_adopt": u != nil && u.IsSuper && configured && source == "environment" && s.paymentKeyPath != "", "providers": []string{"pingpong"}, "api_version": "checkout-v4"})
}

func (s *Server) initializePaymentKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Mode string `json:"mode"`
		Key  string `json:"key"`
	}
	if !paymentDecode(w, r, &in) {
		return
	}
	s.paymentKeyMu.Lock()
	defer s.paymentKeyMu.Unlock()
	if s.paymentKeyPath == "" || s.commerce.Cipher.Configured() {
		writeJSON(w, 409, map[string]string{"error": "密钥已配置或服务未配置持久化目录，不能覆盖现有密钥"})
		return
	}
	key := strings.TrimSpace(in.Key)
	if in.Mode == "generate" && key == "" {
		var count int
		if err := s.database.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM gocms_payment_account`).Scan(&count); err != nil {
			commerceError(w, err)
			return
		}
		if count > 0 {
			writeJSON(w, 409, map[string]string{"error": "已有支付渠道，请导入原密钥恢复，不能生成新密钥"})
			return
		}
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			commerceError(w, err)
			return
		}
		key = base64.StdEncoding.EncodeToString(raw)
	} else if in.Mode != "import" || key == "" {
		writeJSON(w, 400, map[string]string{"error": "请选择自动生成或导入有效密钥"})
		return
	}
	cipher, err := payment.NewCipher(key)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "密钥必须是 32 字节随机数据的 Base64 编码"})
		return
	}
	if err := s.validatePaymentKey(r.Context(), cipher); err != nil {
		writeJSON(w, 400, map[string]string{"error": "密钥无法验证，请导入能够解密全部已有支付渠道的原密钥"})
		return
	}
	if err := savePaymentKey(s.paymentKeyPath, key); err != nil {
		writeJSON(w, 503, map[string]string{"error": "密钥文件保存失败，请检查数据目录权限或已有密钥文件；现有文件不会覆盖"})
		return
	}
	if err := s.commerce.Cipher.Initialize(key); err != nil {
		commerceError(w, err)
		return
	}
	s.paymentKeySource = "file"
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func savePaymentKey(path, key string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".payment-key-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.WriteString(key + "\n"); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	// Link publishes a complete private file atomically and never overwrites.
	return os.Link(f.Name(), path)
}

// activePaymentKey is only called while holding paymentKeyMu. Check the source
// against the live cipher before allowing a backup or persisting an env key.
func (s *Server) activePaymentKey() (string, error) {
	var raw []byte
	var err error
	switch s.paymentKeySource {
	case "environment":
		raw = []byte(os.Getenv("GOCMS_PAYMENT_KEY"))
	case "file":
		info, statErr := os.Lstat(s.paymentKeyPath)
		if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 256 {
			return "", errors.New("payment key file unavailable")
		}
		raw, err = os.ReadFile(s.paymentKeyPath)
	default:
		return "", payment.ErrUnavailable
	}
	if err != nil {
		return "", err
	}
	key := strings.TrimSpace(string(raw))
	candidate, err := payment.NewCipher(key)
	if err != nil {
		return "", err
	}
	probe, err := s.commerce.Cipher.Seal("payment-key-backup", "key-verification")
	if err != nil {
		return "", err
	}
	if _, err := candidate.Open(probe, "key-verification"); err != nil {
		return "", err
	}
	return key, nil
}

func (s *Server) exportPaymentKey(w http.ResponseWriter, r *http.Request) {
	s.paymentKeyMu.Lock()
	defer s.paymentKeyMu.Unlock()
	key, err := s.activePaymentKey()
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": "当前密钥来源不可用，无法导出备份"})
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="gocms-payment.key"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(key + "\n"))
}

func (s *Server) adoptPaymentKey(w http.ResponseWriter, r *http.Request) {
	s.paymentKeyMu.Lock()
	defer s.paymentKeyMu.Unlock()
	if s.paymentKeySource != "environment" || s.paymentKeyPath == "" {
		writeJSON(w, 409, map[string]string{"error": "当前密钥无需接管或未配置持久化目录"})
		return
	}
	key, err := s.activePaymentKey()
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": "无法验证当前环境变量密钥"})
		return
	}
	if err := savePaymentKey(s.paymentKeyPath, key); err != nil {
		writeJSON(w, 503, map[string]string{"error": "接管失败，数据目录不可写或已有密钥文件；未覆盖原文件"})
		return
	}
	s.paymentKeySource = "file"
	writeJSON(w, 200, map[string]bool{"ok": true})
}
