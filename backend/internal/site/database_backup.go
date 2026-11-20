package site

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"gocms/internal/db"
)

type databaseBackupService struct {
	ctx             context.Context
	database        *sql.DB
	keyPath         string
	defaultLocation *time.Location
	httpClient      *http.Client
	gate            sync.Mutex // Serializes configuration changes and backup jobs.
	running         atomic.Bool
	retryAt         time.Time
}

func (s *Server) StartDatabaseBackupMaintenance() {
	b := s.databaseBackup
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			b.runScheduled(time.Now())
			select {
			case <-b.ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (b *databaseBackupService) runScheduled(now time.Time) {
	if b.ctx.Err() != nil || !b.gate.TryLock() {
		return
	}
	if now.Before(b.retryAt) {
		b.gate.Unlock()
		return
	}
	config, err := b.loadConfig(b.ctx)
	if err != nil {
		log.Printf("read database backup schedule: %v", err)
		b.gate.Unlock()
		return
	}
	day := scheduledBackupDay(config, now)
	if day == "" {
		b.gate.Unlock()
		return
	}
	b.start(config, day)
}

func scheduledBackupDay(config backupConfig, now time.Time) string {
	if !config.Automatic || !config.HasSecret {
		return ""
	}
	loc, err := time.LoadLocation(config.TimeZone)
	if err != nil {
		return ""
	}
	local := now.In(loc)
	day := local.Format(time.DateOnly)
	if day <= config.lastScheduledDay || local.Format("15:04") < config.DailyAt {
		return ""
	}
	return day
}

// start takes ownership of gate until the result has been persisted.
func (b *databaseBackupService) start(config backupConfig, day string) {
	b.running.Store(true)
	go func() {
		defer b.gate.Unlock()
		defer b.running.Store(false)
		ctx, cancel := context.WithTimeout(b.ctx, time.Hour)
		defer cancel()
		result := b.backup(ctx, config, day)
		finished := time.Now()
		result.At = finished.UTC().Format(time.RFC3339Nano)
		if day != "" {
			if result.State == "failed" {
				b.retryAt = finished.Add(15 * time.Minute)
			} else {
				config.lastScheduledDay = day
			}
		}
		saveCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_, err := b.database.ExecContext(saveCtx, `UPDATE gocms_database_backup SET last_scheduled_day=?,
			last_run_at=?,last_source=?,last_state=?,last_message=? WHERE id=1`,
			config.lastScheduledDay, result.At, result.Source, result.State, result.Message)
		if err != nil {
			log.Printf("save database backup result: %v", err)
		}
	}()
}

func (b *databaseBackupService) backup(ctx context.Context, config backupConfig, day string) backupResult {
	result := backupResult{Source: "manual", State: "failed"}
	key := config.Prefix + "manual-" + time.Now().UTC().Format(backupKeyTime) + "-" + rand.Text() + ".sqlite3"
	if day != "" {
		result.Source = "automatic"
		key = config.Prefix + "daily-" + day + ".sqlite3"
	}
	fail := func(message string, err error) backupResult {
		log.Printf("database backup: %s: %v", message, err)
		result.Message = message
		return result
	}
	loc, err := time.LoadLocation(config.TimeZone)
	if err != nil {
		return fail("备份时区不可用，请检查备份配置", err)
	}
	client, err := b.storage(config.backupSettings, config.ciphertext, "")
	if err != nil {
		return fail("无法读取已保存的密钥，请重新填写 Secret Access Key", err)
	}
	exists, err := backupObjectExists(ctx, client, config.Bucket, key)
	if err != nil {
		return fail("检查 S3 备份对象失败，请检查存储配置和读取权限", err)
	}
	if !exists {
		directory, err := os.MkdirTemp(filepath.Dir(b.keyPath), ".database-backup-")
		if err != nil {
			return fail("创建数据库快照失败，请检查磁盘空间和权限", err)
		}
		defer os.RemoveAll(directory)
		filename := filepath.Join(directory, "snapshot.sqlite3")
		if err := db.Snapshot(ctx, b.database, filename); err != nil {
			return fail("创建数据库快照失败，请检查磁盘空间和权限", err)
		}
		file, err := os.Open(filename)
		if err != nil {
			return fail("读取数据库快照失败", err)
		}
		err = uploadBackupObject(ctx, client, config.Bucket, key, file)
		file.Close()
		if err != nil && !hasS3Status(err, http.StatusPreconditionFailed) {
			return fail("上传数据库快照失败，请检查 S3 连接和写入权限", err)
		}
	}
	if err := pruneBackupObjects(ctx, client, config, key, time.Now().In(loc)); err != nil {
		log.Printf("prune database backups: %v", err)
		result.State = "warning"
		result.Message = "备份已上传，旧备份清理未完成。请检查 S3 连接和权限"
		return result
	}
	result.State = "success"
	result.Message = "数据库已备份"
	return result
}

func hasS3Status(err error, code int) bool {
	var response interface{ HTTPStatusCode() int }
	return errors.As(err, &response) && response.HTTPStatusCode() == code
}
