package site

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"

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
	name := "manual-" + time.Now().UTC().Format(backupKeyTime) + "-" + rand.Text()
	if day != "" {
		result.Source = "automatic"
		name = "daily-" + day
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
	key := config.Prefix + name + backupArchiveSuffix
	exists, err := backupObjectExists(ctx, client, config.Bucket, key)
	if err != nil {
		return fail("检查 S3 备份对象失败，请检查存储配置和读取权限", err)
	}
	if !exists && day != "" {
		// A scheduled upload from an earlier version may have succeeded before its result was saved.
		legacyKey := config.Prefix + name + backupSQLiteSuffix
		exists, err = backupObjectExists(ctx, client, config.Bucket, legacyKey)
		if err != nil {
			return fail("检查 S3 备份对象失败，请检查存储配置和读取权限", err)
		}
		if exists {
			key = legacyKey
		}
	}
	if !exists {
		message, err := b.createAndUploadSnapshot(ctx, client, config.Bucket, key)
		if err != nil {
			return fail(message, err)
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

func (b *databaseBackupService) createAndUploadSnapshot(ctx context.Context, client *s3.Client, bucket, key string) (string, error) {
	const snapshotError = "创建数据库快照失败，请检查磁盘空间和权限"
	directory, err := os.MkdirTemp(filepath.Dir(b.keyPath), ".database-backup-")
	if err != nil {
		return snapshotError, err
	}
	defer func() {
		if err := os.RemoveAll(directory); err != nil {
			log.Printf("clean up temporary database backup: %v", err)
		}
	}()

	const snapshotName = "snapshot" + backupSQLiteSuffix
	snapshot := filepath.Join(directory, snapshotName)
	if err := db.Snapshot(ctx, b.database, snapshot); err != nil {
		if errors.Is(err, db.ErrSnapshotBusy) {
			return "数据库持续繁忙，未能创建快照，请稍后重试", err
		}
		return snapshotError, err
	}
	archive := filepath.Join(directory, "snapshot"+backupArchiveSuffix)
	cmd := exec.CommandContext(ctx, "tar", "-czf", archive, "-C", directory, snapshotName)
	// Use the requested archive options and omit macOS AppleDouble metadata.
	cmd.Env = append(cmd.Environ(), "TAR_OPTIONS=", "GZIP=", "COPYFILE_DISABLE=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		return "压缩备份失败，请检查系统 tar、gzip 和磁盘空间", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	if err := os.Remove(snapshot); err != nil {
		log.Printf("remove uncompressed database snapshot: %v", err)
	}
	file, err := os.Open(archive)
	if err != nil {
		return "读取压缩备份失败", err
	}
	defer file.Close()
	if err := uploadBackupObject(ctx, client, bucket, key, file); err != nil && !hasS3Status(err, http.StatusPreconditionFailed) {
		return "上传压缩备份失败，请检查 S3 连接和写入权限", err
	}
	return "", nil
}

func hasS3Status(err error, code int) bool {
	var response interface{ HTTPStatusCode() int }
	return errors.As(err, &response) && response.HTTPStatusCode() == code
}
