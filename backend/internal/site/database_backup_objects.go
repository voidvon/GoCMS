package site

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type backupObjectSelection struct {
	Key    string `json:"key"`
	Target string `json:"target"`
}

// Bind actions to the bucket shown in the list, even if another admin changes it.
func (config backupConfig) objectTarget() string {
	if !config.HasSecret {
		return ""
	}
	target := sha256.Sum256([]byte(config.Endpoint + "\n" + config.Bucket + "\n" + config.Prefix))
	return hex.EncodeToString(target[:])
}

func (b *databaseBackupService) adminBackupObject(w http.ResponseWriter, r *http.Request, config backupConfig) {
	var selection backupObjectSelection
	if r.Method == http.MethodGet {
		query := r.URL.Query()
		selection = backupObjectSelection{Key: query.Get("key"), Target: query.Get("target")}
	} else if err := memberDecode(r, &selection); err != nil {
		backupError(w, http.StatusBadRequest, "备份文件参数无效")
		return
	}
	if !config.HasSecret || selection.Target != config.objectTarget() {
		backupError(w, http.StatusConflict, "备份存储配置已变更，请刷新列表后重试")
		return
	}
	filename, managed := managedBackupFilename(config.Prefix, selection.Key)
	if !managed {
		backupError(w, http.StatusNotFound, "备份文件不存在")
		return
	}
	client, err := b.storage(config.backupSettings, config.ciphertext, "")
	if err != nil {
		backupError(w, http.StatusConflict, "无法读取已保存的密钥，请重新填写 Secret Access Key")
		return
	}
	if r.Method == http.MethodGet {
		b.downloadBackupObject(w, r, client, config.Bucket, selection.Key, filename)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if r.Method == http.MethodDelete {
		if _, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(config.Bucket), Key: aws.String(selection.Key)}); err != nil {
			log.Printf("delete database backup: %v", err)
			backupError(w, http.StatusBadGateway, "删除备份失败，请检查 S3 连接和删除权限")
			return
		}
	} else {
		exists, err := backupObjectExists(ctx, client, config.Bucket, selection.Key)
		if err != nil {
			log.Printf("prepare database backup download: %v", err)
			backupError(w, http.StatusBadGateway, "读取备份文件失败，请检查 S3 连接和读取权限")
			return
		}
		if !exists {
			backupError(w, http.StatusNotFound, "备份文件已不存在，请刷新列表")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (b *databaseBackupService) downloadBackupObject(w http.ResponseWriter, r *http.Request, client *s3.Client, bucket, key, filename string) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Hour)
	defer cancel()
	input := &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)}
	if value := r.Header.Get("Range"); value != "" {
		input.Range = aws.String(value)
	}
	object, err := client.GetObject(ctx, input, func(options *s3.Options) {
		if b.httpClient == nil {
			// Use the download context to limit the transfer instead of the shorter S3 control request timeout.
			options.HTTPClient = objectStorageHTTPClient(0)
		}
	})
	if err != nil {
		switch {
		case hasS3Status(err, http.StatusNotFound):
			backupError(w, http.StatusNotFound, "备份文件已不存在，请刷新列表")
		case hasS3Status(err, http.StatusRequestedRangeNotSatisfiable):
			backupError(w, http.StatusRequestedRangeNotSatisfiable, "下载范围无效")
		default:
			log.Printf("download database backup: %v", err)
			backupError(w, http.StatusBadGateway, "下载备份失败，请检查 S3 连接和读取权限")
		}
		return
	}
	defer object.Body.Close()

	header := w.Header()
	header.Set("Cache-Control", "private, no-store")
	header.Set("Content-Type", "application/vnd.sqlite3")
	header.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	header.Set("X-Content-Type-Options", "nosniff")
	if object.ContentLength != nil {
		header.Set("Content-Length", strconv.FormatInt(*object.ContentLength, 10))
	}
	if object.AcceptRanges != nil {
		header.Set("Accept-Ranges", *object.AcceptRanges)
	}
	if object.ContentRange != nil {
		header.Set("Content-Range", *object.ContentRange)
		w.WriteHeader(http.StatusPartialContent)
	}
	if _, err := io.Copy(w, object.Body); err != nil && ctx.Err() == nil {
		log.Printf("stream database backup: %v", err)
	}
}
