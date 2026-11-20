package site

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const backupKeyTime = "20060102T150405.000000000Z"

var backupObjectName = regexp.MustCompile(`^(manual-[0-9]{8}T[0-9]{6}\.[0-9]{9}Z-[A-Z2-7]{26,}|daily-[0-9]{4}-[0-9]{2}-[0-9]{2})\.sqlite3$`)

func managedBackupFilename(prefix, key string) (string, bool) {
	name, underPrefix := strings.CutPrefix(key, prefix)
	return name, prefix != "" && underPrefix && backupObjectName.MatchString(name)
}

type backupObject struct {
	Key     string    `json:"key"`
	At      time.Time `json:"at"`
	Size    int64     `json:"size"`
	Recent  bool      `json:"recent"`
	Monthly bool      `json:"monthly"`
}

func testBackupStorage(ctx context.Context, client *s3.Client, config backupConfig) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	bucket, key := aws.String(config.Bucket), aws.String(config.Prefix+".connection-test/"+rand.Text())
	_, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: bucket, Prefix: aws.String(config.Prefix), MaxKeys: aws.Int32(1)})
	if err != nil {
		return err
	}
	_, err = client.PutObject(ctx, &s3.PutObjectInput{Bucket: bucket, Key: key, Body: bytes.NewReader([]byte("GoCMS")), IfNoneMatch: aws.String("*")})
	if err != nil {
		return err
	}
	_, readErr := client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: bucket, Key: key})
	cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer stop()
	_, deleteErr := client.DeleteObject(cleanup, &s3.DeleteObjectInput{Bucket: bucket, Key: key})
	return errors.Join(readErr, deleteErr)
}

func backupObjectExists(ctx context.Context, client *s3.Client, bucket, key string) (bool, error) {
	object, err := client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if hasS3Status(err, http.StatusNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if aws.ToInt64(object.ContentLength) <= 0 {
		return false, errors.New("existing backup object is empty")
	}
	return true, nil
}

func uploadBackupObject(ctx context.Context, client *s3.Client, bucket, key string, file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	const partSize = 32 << 20
	if info.Size() <= partSize {
		_, err := client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(bucket), Key: aws.String(key), Body: file, ContentLength: aws.Int64(info.Size()),
			ContentType: aws.String("application/vnd.sqlite3"), IfNoneMatch: aws.String("*"),
		})
		return err
	}
	// S3 allows at most 10,000 parts. Every part except the last uses the same size for R2.
	return uploadBackupParts(ctx, client, bucket, key, file, info.Size(), max(partSize, (info.Size()+9999)/10000))
}

func uploadBackupParts(ctx context.Context, client *s3.Client, bucket, key string, file *os.File, size, partSize int64) (err error) {
	target, object := aws.String(bucket), aws.String(key)
	start, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: target, Key: object, ContentType: aws.String("application/vnd.sqlite3"),
	})
	if err != nil {
		return err
	}
	if aws.ToString(start.UploadId) == "" {
		return errors.New("S3 did not return a multipart upload ID")
	}
	defer func() {
		if err == nil {
			return
		}
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer stop()
		_, abortErr := client.AbortMultipartUpload(cleanup, &s3.AbortMultipartUploadInput{
			Bucket: target, Key: object, UploadId: start.UploadId,
		})
		if abortErr != nil && !hasS3Status(abortErr, http.StatusNotFound) {
			log.Printf("abort database backup upload: %v", abortErr)
		}
	}()
	parts := make([]types.CompletedPart, 0, (size+partSize-1)/partSize)
	for offset := int64(0); offset < size; offset += partSize {
		length := min(partSize, size-offset)
		number := aws.Int32(int32(len(parts) + 1))
		part, err := client.UploadPart(ctx, &s3.UploadPartInput{
			Bucket: target, Key: object, UploadId: start.UploadId, PartNumber: number, ContentLength: aws.Int64(length),
			Body: io.NewSectionReader(file, offset, length),
		})
		if err != nil {
			return err
		}
		parts = append(parts, types.CompletedPart{ETag: part.ETag, PartNumber: number})
	}
	_, err = client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: target, Key: object, UploadId: start.UploadId, IfNoneMatch: aws.String("*"),
		MultipartUpload: &types.CompletedMultipartUpload{Parts: parts},
	})
	return err
}

func listBackupObjects(ctx context.Context, client *s3.Client, config backupConfig) ([]backupObject, error) {
	objects := []backupObject{}
	input := &s3.ListObjectsV2Input{Bucket: aws.String(config.Bucket), Prefix: aws.String(config.Prefix)}
	tokens := map[string]bool{}
	for {
		page, err := client.ListObjectsV2(ctx, input)
		if err != nil {
			return nil, err
		}
		for _, object := range page.Contents {
			key := aws.ToString(object.Key)
			_, managed := managedBackupFilename(config.Prefix, key)
			if !managed || object.LastModified == nil || aws.ToInt64(object.Size) <= 0 {
				continue
			}
			objects = append(objects, backupObject{Key: key, At: object.LastModified.UTC(), Size: *object.Size})
		}
		if !aws.ToBool(page.IsTruncated) {
			return objects, nil
		}
		token := aws.ToString(page.NextContinuationToken)
		if token == "" || tokens[token] {
			return nil, errors.New("incomplete S3 backup listing")
		}
		tokens[token] = true
		input.ContinuationToken = page.NextContinuationToken
	}
}

func classifyBackupObjects(objects []backupObject, now time.Time) {
	sort.Slice(objects, func(i, j int) bool {
		if objects[i].At.Equal(objects[j].At) {
			return objects[i].Key > objects[j].Key
		}
		return objects[i].At.After(objects[j].At)
	})
	current := now.Year()*12 + int(now.Month())
	selectedMonths := map[int]bool{}
	for i := range objects {
		object := &objects[i]
		object.Recent = i < 10
		local := object.At.In(now.Location())
		month := local.Year()*12 + int(local.Month())
		object.Monthly = month >= current-36 && month < current && !selectedMonths[month]
		if object.Monthly {
			selectedMonths[month] = true
		}
	}
}

func pruneBackupObjects(ctx context.Context, client *s3.Client, config backupConfig, uploaded string, now time.Time) error {
	objects, err := listBackupObjects(ctx, client, config)
	if err != nil {
		return err
	}
	found := false
	for _, object := range objects {
		if object.Key == uploaded {
			found = true
			break
		}
	}
	if !found {
		return errors.New("uploaded object missing from S3 listing")
	}
	classifyBackupObjects(objects, now)
	for _, object := range objects {
		if object.Recent || object.Monthly {
			continue
		}
		if _, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(config.Bucket), Key: aws.String(object.Key)}); err != nil {
			return err
		}
	}
	return nil
}
