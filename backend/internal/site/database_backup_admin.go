package site

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	databaseBackupRoute       = "/api/admin/database-backup"
	backupObjectsRoute        = databaseBackupRoute + "/objects"
	backupDownloadRoute       = backupObjectsRoute + "/download"
	databaseBackupSecretScope = "database-backup:platform"
)

var (
	backupBucket = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	backupRegion = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
)

type backupSettings struct {
	Endpoint  string `json:"endpoint"`
	Bucket    string `json:"bucket"`
	Region    string `json:"region"`
	AccessKey string `json:"access_key"`
	Automatic bool   `json:"automatic"`
	DailyAt   string `json:"daily_at"`
	TimeZone  string `json:"time_zone"`
}

type backupConfig struct {
	backupSettings
	namespace        string
	ciphertext       string
	lastScheduledDay string
	HasSecret        bool         `json:"has_secret"`
	Prefix           string       `json:"prefix"`
	Running          bool         `json:"running"`
	LastRun          backupResult `json:"last_run"`
}

type backupResult struct {
	At      string `json:"at"`
	Source  string `json:"source"`
	State   string `json:"state"`
	Message string `json:"message"`
}

func (s *Server) ConfigureDatabaseBackup(ctx context.Context, data string, location *time.Location) error {
	keyPath, err := s.platformPrivateKeyPath(data, "database-backup.key")
	if err != nil {
		return err
	}
	s.databaseBackup = &databaseBackupService{ctx: ctx, database: s.database, keyPath: keyPath, defaultLocation: location}
	return nil
}

func (b *databaseBackupService) loadConfig(ctx context.Context) (backupConfig, error) {
	config := backupConfig{
		backupSettings: backupSettings{Region: "auto", DailyAt: "03:00", TimeZone: b.defaultLocation.String()},
		Running:        b.running.Load(),
	}
	err := b.database.QueryRowContext(ctx, `SELECT namespace,endpoint,bucket,region,access_key,secret_ciphertext,
		automatic,daily_at,time_zone,last_scheduled_day,last_run_at,last_source,last_state,last_message
		FROM gocms_database_backup WHERE id=1`).Scan(
		&config.namespace, &config.Endpoint, &config.Bucket, &config.Region, &config.AccessKey, &config.ciphertext,
		&config.Automatic, &config.DailyAt, &config.TimeZone, &config.lastScheduledDay,
		&config.LastRun.At, &config.LastRun.Source, &config.LastRun.State, &config.LastRun.Message)
	if errors.Is(err, sql.ErrNoRows) {
		return config, nil
	}
	if err != nil {
		return config, err
	}
	config.Prefix = "gocms-database-backups/" + config.namespace + "/"
	config.HasSecret = config.ciphertext != ""
	return config, nil
}

func (c *backupSettings) validate() error {
	c.Endpoint = strings.TrimRight(strings.TrimSpace(c.Endpoint), "/")
	c.Bucket = strings.TrimSpace(c.Bucket)
	c.Region = strings.TrimSpace(c.Region)
	c.AccessKey = strings.TrimSpace(c.AccessKey)
	c.TimeZone = strings.TrimSpace(c.TimeZone)
	endpoint, err := url.Parse(c.Endpoint)
	if err != nil || len(c.Endpoint) > 512 || endpoint.Scheme != "https" || endpoint.Hostname() == "" ||
		endpoint.User != nil || endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.ForceQuery ||
		strings.Contains(c.Endpoint, "#") {
		return errors.New("请输入完整的 HTTPS S3 Endpoint，地址只包含主机名和可选端口")
	}
	if !backupBucket.MatchString(c.Bucket) || strings.Contains(c.Bucket, "..") {
		return errors.New("请输入有效的 S3 存储桶名称")
	}
	if !backupRegion.MatchString(c.Region) {
		return errors.New("请输入有效的区域，Cloudflare R2 使用 auto")
	}
	if c.AccessKey == "" || len(c.AccessKey) > 128 {
		return errors.New("请输入有效的 Access Key ID")
	}
	if _, err := time.Parse("15:04", c.DailyAt); err != nil || len(c.DailyAt) != 5 {
		return errors.New("请输入有效的每日备份时间")
	}
	if c.TimeZone == "" || len(c.TimeZone) > 128 {
		return errors.New("请输入时区，如 Asia/Shanghai")
	}
	if _, err := time.LoadLocation(c.TimeZone); err != nil {
		return errors.New("请输入有效的时区，如 Asia/Shanghai")
	}
	return nil
}

func (b *databaseBackupService) storage(settings backupSettings, ciphertext, secret string) (*s3.Client, error) {
	if secret == "" {
		cipher, err := localCredentialCipher(b.keyPath, false)
		if err != nil {
			return nil, err
		}
		secret, err = cipher.Open(ciphertext, databaseBackupSecretScope)
		if err != nil {
			return nil, err
		}
	}
	return newObjectStorageClient(settings.Endpoint, settings.Region, settings.AccessKey, secret, b.httpClient), nil
}

func (s *Server) adminDatabaseBackup(w http.ResponseWriter, r *http.Request, route string) {
	b := s.databaseBackup
	if b == nil {
		backupError(w, http.StatusServiceUnavailable, "备份服务不可用")
		return
	}
	switch route {
	case databaseBackupRoute:
		if r.Method != http.MethodGet && r.Method != http.MethodPut {
			methodNotAllowed(w)
			return
		}
	case backupObjectsRoute:
		if r.Method != http.MethodGet && r.Method != http.MethodDelete {
			methodNotAllowed(w)
			return
		}
	case backupDownloadRoute:
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
	default:
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
	}
	if !memberRequest(w, r) {
		return
	}
	run := route == databaseBackupRoute+"/run"
	exclusive := run || r.Method == http.MethodPut || r.Method == http.MethodDelete
	if exclusive {
		if !b.gate.TryLock() {
			backupError(w, http.StatusConflict, "备份任务或配置保存正在进行，请稍后重试")
			return
		}
		defer func() {
			if exclusive {
				b.gate.Unlock()
			}
		}()
	}
	config, err := b.loadConfig(r.Context())
	if err != nil {
		backupError(w, http.StatusInternalServerError, "读取备份设置失败")
		return
	}
	switch {
	case run:
		if !config.HasSecret || b.ctx.Err() != nil {
			backupError(w, http.StatusConflict, "请先保存备份存储配置并确认服务正在运行")
			return
		}
		exclusive = false
		b.start(config, "")
		writeJSON(w, http.StatusAccepted, map[string]bool{"running": true})
	case route == backupObjectsRoute && r.Method == http.MethodGet:
		b.adminObjects(w, r, config)
	case route == backupObjectsRoute || route == backupDownloadRoute:
		b.adminBackupObject(w, r, config)
	case r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, config)
	default:
		b.adminSaveOrTest(w, r, config)
	}
}

func (b *databaseBackupService) adminSaveOrTest(w http.ResponseWriter, r *http.Request, current backupConfig) {
	var input struct {
		backupSettings
		Secret string `json:"secret"`
	}
	if err := memberDecode(r, &input); err != nil || len(input.Secret) > 256 {
		backupError(w, http.StatusBadRequest, "备份设置格式无效")
		return
	}
	settings := input.backupSettings
	if err := settings.validate(); err != nil {
		backupError(w, http.StatusBadRequest, err.Error())
		return
	}
	secret := strings.TrimSpace(input.Secret)
	if secret == "" && (!current.HasSecret || settings.Endpoint != current.Endpoint || settings.AccessKey != current.AccessKey) {
		backupError(w, http.StatusBadRequest, "请填写 Secret Access Key")
		return
	}
	config := current
	config.backupSettings = settings
	if config.namespace == "" {
		config.namespace = rand.Text()
		config.Prefix = "gocms-database-backups/" + config.namespace + "/"
	}
	test := r.Method == http.MethodPost
	if test || settings.Endpoint != current.Endpoint || settings.Bucket != current.Bucket ||
		settings.Region != current.Region || settings.AccessKey != current.AccessKey || secret != "" {
		client, err := b.storage(settings, current.ciphertext, secret)
		if err != nil {
			backupError(w, http.StatusConflict, "无法读取已保存的密钥，请重新填写 Secret Access Key")
			return
		}
		if err := testBackupStorage(r.Context(), client, config); err != nil {
			log.Printf("database backup storage test: %v", err)
			backupError(w, http.StatusBadGateway, "S3 连接测试失败，请检查存储桶、区域、凭据及列出、读写和删除权限")
			return
		}
	}
	if test {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	if secret != "" {
		cipher, err := localCredentialCipher(b.keyPath, true)
		if err == nil {
			config.ciphertext, err = cipher.Seal(secret, databaseBackupSecretScope)
		}
		if err != nil {
			backupError(w, http.StatusInternalServerError, "无法加密并保存备份凭据")
			return
		}
	}
	if settings.Endpoint != current.Endpoint || settings.Bucket != current.Bucket {
		config.LastRun = backupResult{}
		config.lastScheduledDay = ""
	}
	_, err := b.database.ExecContext(r.Context(), `INSERT INTO gocms_database_backup
		(id,namespace,endpoint,bucket,region,access_key,secret_ciphertext,automatic,daily_at,time_zone,
		 last_scheduled_day,last_run_at,last_source,last_state,last_message)
		VALUES (1,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET
		endpoint=excluded.endpoint,bucket=excluded.bucket,region=excluded.region,access_key=excluded.access_key,
		secret_ciphertext=excluded.secret_ciphertext,automatic=excluded.automatic,daily_at=excluded.daily_at,
		time_zone=excluded.time_zone,last_scheduled_day=excluded.last_scheduled_day,last_run_at=excluded.last_run_at,
		last_source=excluded.last_source,last_state=excluded.last_state,last_message=excluded.last_message`,
		config.namespace, settings.Endpoint, settings.Bucket, settings.Region, settings.AccessKey,
		config.ciphertext, settings.Automatic, settings.DailyAt, settings.TimeZone, config.lastScheduledDay,
		config.LastRun.At, config.LastRun.Source, config.LastRun.State, config.LastRun.Message)
	if err != nil {
		backupError(w, http.StatusInternalServerError, "保存备份设置失败")
		return
	}
	b.retryAt = time.Time{}
	config.HasSecret = true
	writeJSON(w, http.StatusOK, config)
}

func (b *databaseBackupService) adminObjects(w http.ResponseWriter, r *http.Request, config backupConfig) {
	objects := []backupObject{}
	if config.HasSecret {
		client, err := b.storage(config.backupSettings, config.ciphertext, "")
		if err != nil {
			backupError(w, http.StatusConflict, "无法读取已保存的密钥，请重新填写 Secret Access Key")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		objects, err = listBackupObjects(ctx, client, config)
		if err != nil {
			log.Printf("list database backups: %v", err)
			backupError(w, http.StatusBadGateway, "读取备份列表失败，请检查 S3 连接和列出对象权限")
			return
		}
		loc, err := time.LoadLocation(config.TimeZone)
		if err != nil {
			backupError(w, http.StatusInternalServerError, "备份时区不可用，请检查备份配置")
			return
		}
		classifyBackupObjects(objects, time.Now().In(loc))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": objects, "target": config.objectTarget()})
}

func backupError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
