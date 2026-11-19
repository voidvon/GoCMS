package site

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"gocms/internal/payment"
)

var (
	r2Endpoint = regexp.MustCompile(`^[a-f0-9]{32}(\.(eu|fedramp))?\.r2\.cloudflarestorage\.com$`)
	r2Bucket   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)
)

const sharedMediaStorageScope = "media-storage:platform"

type mediaStorageSettings struct {
	Enabled    bool   `json:"enabled"`
	Images     bool   `json:"images"`
	Endpoint   string `json:"endpoint"`
	Bucket     string `json:"bucket"`
	PublicURL  string `json:"public_url"`
	AccessKey  string `json:"access_key"`
	Secret     string `json:"secret,omitempty"`
	HasSecret  bool   `json:"has_secret"`
	ciphertext string
	scope      string
}

func (s *Server) ConfigureMediaStorage(data string) error {
	root, err := filepath.EvalSymlinks(data)
	if err != nil {
		return err
	}
	keyPath, err := filepath.Abs(filepath.Join(root, "media-storage.key"))
	if err != nil {
		return err
	}
	for _, public := range []string{s.siteRoot, s.assetsRoot, s.frontendRoot, s.storefrontRoot} {
		if public == "" {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(public); err == nil {
			public = resolved
		}
		absolute, err := filepath.Abs(public)
		if err != nil {
			return err
		}
		if relative, err := filepath.Rel(absolute, keyPath); err == nil && filepath.IsLocal(relative) {
			return errors.New("文件存储密钥必须位于公开目录之外")
		}
	}
	s.mediaStorageKey = keyPath
	return nil
}

func (s *Server) mediaStorageCipher(create bool) (*payment.Cipher, error) {
	if s.mediaStorageKey == "" {
		return nil, errors.New("服务端文件存储密钥目录不可用")
	}
	info, err := os.Lstat(s.mediaStorageKey)
	if errors.Is(err, os.ErrNotExist) && create {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		if err := savePrivateKey(s.mediaStorageKey, base64.StdEncoding.EncodeToString(raw)); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		info, err = os.Lstat(s.mediaStorageKey)
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 256 {
		return nil, errors.New("文件存储密钥不可用")
	}
	raw, err := os.ReadFile(s.mediaStorageKey)
	if err != nil {
		return nil, err
	}
	return payment.NewCipher(strings.TrimSpace(string(raw)))
}

func scanMediaStorage(row *sql.Row, scope string) (mediaStorageSettings, error) {
	config := mediaStorageSettings{scope: scope}
	err := row.Scan(&config.Enabled, &config.Images, &config.Endpoint, &config.Bucket,
		&config.PublicURL, &config.AccessKey, &config.ciphertext)
	config.HasSecret = config.ciphertext != ""
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return config, err
}

func (s *Server) loadMediaStorage(ctx context.Context) (mediaStorageSettings, error) {
	return scanMediaStorage(s.database.QueryRowContext(ctx, `SELECT enabled, images, endpoint, bucket, public_url, access_key, secret_ciphertext
		FROM gocms_platform_media_storage WHERE id=1`), sharedMediaStorageScope)
}

func (s *Server) loadAttachmentStorage(ctx context.Context, asset MediaAsset) (mediaStorageSettings, error) {
	if asset.sharedStorage {
		return s.loadMediaStorage(ctx)
	}
	// Attachments stored before the platform-wide setting still use their saved bucket.
	return scanMediaStorage(s.database.QueryRowContext(ctx, `SELECT enabled, images, endpoint, bucket, public_url, access_key, secret_ciphertext
		FROM gocms_media_storage WHERE site_id=?`, asset.SiteID), fmt.Sprintf("media-storage:%d", asset.SiteID))
}

func (s *Server) r2Client(config mediaStorageSettings) (*s3.Client, error) {
	if config.Secret == "" {
		vault, err := s.mediaStorageCipher(false)
		if err != nil {
			return nil, err
		}
		config.Secret, err = vault.Open(config.ciphertext, config.scope)
		if err != nil {
			return nil, err
		}
	}
	httpClient := s.mediaStorageClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 120 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}}
	}
	return s3.New(s3.Options{
		BaseEndpoint: aws.String(config.Endpoint), Region: "auto", UsePathStyle: true, HTTPClient: httpClient,
		Credentials:                aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(config.AccessKey, config.Secret, "")),
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	}), nil
}

func validateMediaStorage(config *mediaStorageSettings) error {
	config.Endpoint = strings.TrimRight(strings.TrimSpace(config.Endpoint), "/")
	config.PublicURL = strings.TrimRight(strings.TrimSpace(config.PublicURL), "/")
	config.Bucket = strings.TrimSpace(config.Bucket)
	config.AccessKey = strings.TrimSpace(config.AccessKey)
	config.Secret = strings.TrimSpace(config.Secret)
	if config.Images && !config.Enabled {
		return errors.New("启用 R2 后才能将图片存入 R2")
	}
	if config.Endpoint == "" && config.Bucket == "" && config.AccessKey == "" && config.Secret == "" && !config.HasSecret &&
		config.PublicURL == "" && !config.Enabled {
		return nil
	}
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || endpoint.Scheme != "https" || !r2Endpoint.MatchString(endpoint.Host) || endpoint.User != nil ||
		endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" {
		return errors.New("请输入 Cloudflare R2 提供的 HTTPS S3 Endpoint")
	}
	if !r2Bucket.MatchString(config.Bucket) {
		return errors.New("请输入有效的 R2 存储桶名称")
	}
	if config.AccessKey == "" || len(config.AccessKey) > 128 || len(config.Secret) > 256 || (!config.HasSecret && config.Secret == "") {
		return errors.New("请输入 R2 Access Key ID 和 Secret Access Key")
	}
	if config.PublicURL != "" {
		public, err := url.Parse(config.PublicURL)
		if err != nil || public.Scheme != "https" || public.Hostname() == "" || public.User != nil ||
			public.Path != "" || public.RawQuery != "" || public.ForceQuery || public.Fragment != "" {
			return errors.New("下载域名须为完整 HTTPS 域名，不附加路径或查询参数")
		}
	}
	return nil
}

func (s *Server) adminMediaStorage(w http.ResponseWriter, r *http.Request, test bool) {
	if s.database == nil {
		http.Error(w, "文件存储配置不可用", http.StatusServiceUnavailable)
		return
	}
	if (!test && r.Method != http.MethodGet && r.Method != http.MethodPut) || (test && r.Method != http.MethodPost) {
		methodNotAllowed(w)
		return
	}
	s.mediaStorageMu.Lock()
	defer s.mediaStorageMu.Unlock()
	current, err := s.loadMediaStorage(r.Context())
	if err != nil {
		http.Error(w, "读取文件存储配置失败", http.StatusInternalServerError)
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, current)
		return
	}
	var config mediaStorageSettings
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := decodeRequest(r, &config); err != nil {
		http.Error(w, "文件存储配置格式无效", http.StatusBadRequest)
		return
	}
	config.HasSecret, config.ciphertext, config.scope = current.HasSecret, current.ciphertext, current.scope
	if err := validateMediaStorage(&config); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !test && (current.Endpoint != config.Endpoint || current.Bucket != config.Bucket) {
		var hasObjects bool
		err = s.database.QueryRowContext(r.Context(), `SELECT EXISTS (
			SELECT 1 FROM gocms_media WHERE storage_provider='r2' AND shared_storage=1)`).Scan(&hasObjects)
		if err != nil {
			http.Error(w, "检查已有 R2 附件失败", http.StatusInternalServerError)
			return
		}
		if hasObjects {
			http.Error(w, "共享存储桶已有附件，如需更换存储桶请先迁移文件", http.StatusConflict)
			return
		}
	}
	if test || config.Enabled || config.Secret != "" || (config.HasSecret &&
		(config.AccessKey != current.AccessKey || config.Endpoint != current.Endpoint || config.Bucket != current.Bucket)) {
		if config.Endpoint == "" {
			http.Error(w, "请先填写 R2 连接参数", http.StatusBadRequest)
			return
		}
		if err := s.testMediaStorage(r.Context(), config); err != nil {
			http.Error(w, "R2 读写测试失败，请检查存储桶、访问凭据和权限", http.StatusBadGateway)
			return
		}
	}
	if test {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	if config.Secret != "" {
		vault, err := s.mediaStorageCipher(!current.HasSecret)
		if err == nil {
			config.ciphertext, err = vault.Seal(config.Secret, config.scope)
		}
		if err != nil {
			http.Error(w, "无法加密文件存储凭据，请检查服务端密钥", http.StatusInternalServerError)
			return
		}
	}
	_, err = s.database.ExecContext(r.Context(), `INSERT INTO gocms_platform_media_storage
		(id, enabled, images, endpoint, bucket, public_url, access_key, secret_ciphertext)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET
		enabled=excluded.enabled, images=excluded.images, endpoint=excluded.endpoint, bucket=excluded.bucket,
		public_url=excluded.public_url, access_key=excluded.access_key, secret_ciphertext=excluded.secret_ciphertext`,
		config.Enabled, config.Images, config.Endpoint, config.Bucket, config.PublicURL, config.AccessKey, config.ciphertext)
	if err != nil {
		http.Error(w, "保存文件存储配置失败", http.StatusInternalServerError)
		return
	}
	config.HasSecret, config.Secret = config.ciphertext != "", ""
	writeJSON(w, http.StatusOK, config)
}

func (s *Server) testMediaStorage(ctx context.Context, config mediaStorageSettings) error {
	client, err := s.r2Client(config)
	if err != nil {
		return err
	}
	bucket, key := aws.String(config.Bucket), aws.String(".gocms-test/"+rand.Text())
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if _, err := client.PutObject(ctx, &s3.PutObjectInput{Bucket: bucket, Key: key, Body: bytes.NewReader([]byte("GoCMS"))}); err != nil {
		return err
	}
	object, readErr := client.GetObject(ctx, &s3.GetObjectInput{Bucket: bucket, Key: key})
	if readErr == nil {
		data, err := io.ReadAll(io.LimitReader(object.Body, 6))
		object.Body.Close()
		if err != nil || string(data) != "GoCMS" {
			readErr = errors.New("R2 test object did not match")
		}
	}
	cleanup, release := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
	defer release()
	_, deleteErr := client.DeleteObject(cleanup, &s3.DeleteObjectInput{Bucket: bucket, Key: key})
	return errors.Join(readErr, deleteErr)
}
