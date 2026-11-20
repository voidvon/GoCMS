package site

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gocms/internal/db"
)

func TestDatabaseBackupAdminAndSnapshot(t *testing.T) {
	ctx := t.Context()
	data := t.TempDir()
	database, err := db.Open(filepath.Join(data, "live with space.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.CreateSchema(ctx, database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`PRAGMA journal_mode=WAL;
		CREATE TABLE backup_fixture (value TEXT);
		INSERT INTO backup_fixture (rowid, value) VALUES (1234,'committed')`); err != nil {
		t.Fatal(err)
	}
	server, err := New(database, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seedTestAdmin(t, server)
	token, err := server.createSession("gocms")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO gocms_admin_user (username) VALUES ('editor')`); err != nil {
		t.Fatal(err)
	}
	editor, err := server.createSession("editor")
	if err != nil {
		t.Fatal(err)
	}
	if err := server.ConfigureDatabaseBackup(ctx, data, time.UTC); err != nil {
		t.Fatal(err)
	}
	backup := server.databaseBackup
	now := time.Now().UTC()
	type storedObject struct {
		at   time.Time
		data []byte
	}
	objects := map[string]storedObject{}
	failUpload, failDelete := false, false
	temporaryBackups := filepath.Join(data, ".database-backup-*")
	backup.httpClient = &http.Client{Transport: r2TestTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "object.example.test" || !strings.HasPrefix(request.URL.Path, "/example-backups") ||
			!strings.Contains(request.Header.Get("Authorization"), "Credential=backup-id/") {
			t.Errorf("unexpected destination or missing signature: %s %s", request.Method, request.URL)
		}
		key := strings.TrimPrefix(request.URL.Path, "/example-backups/")
		headers := http.Header{}
		status, body := http.StatusOK, ""
		switch request.Method {
		case http.MethodGet:
			if request.URL.Query().Get("list-type") == "" {
				item, exists := objects[key]
				if !exists {
					status = http.StatusNotFound
					break
				}
				headers.Set("Accept-Ranges", "bytes")
				content := item.data
				if request.Header.Get("Range") == "bytes=0-15" {
					status = http.StatusPartialContent
					headers.Set("Content-Range", fmt.Sprintf("bytes 0-15/%d", len(content)))
					content = content[:16]
				}
				headers.Set("Content-Length", strconv.Itoa(len(content)))
				body = string(content)
				break
			}
			if dirs, err := filepath.Glob(temporaryBackups); err != nil || len(dirs) != 0 {
				t.Errorf("local backup files remain during S3 listing: %v, %v", dirs, err)
			}
			type entry struct {
				Key          string
				LastModified string
				Size         int
			}
			listing := struct {
				XMLName  xml.Name `xml:"ListBucketResult"`
				Contents []entry
			}{}
			for name, item := range objects {
				if strings.HasPrefix(name, request.URL.Query().Get("prefix")) {
					listing.Contents = append(listing.Contents, entry{name, item.at.Format(time.RFC3339), len(item.data)})
				}
			}
			raw, err := xml.Marshal(listing)
			if err != nil {
				return nil, err
			}
			body = string(raw)
		case http.MethodPut:
			if backupObjectName.MatchString(filepath.Base(key)) {
				dirs, err := filepath.Glob(temporaryBackups)
				if err != nil || len(dirs) != 1 {
					t.Errorf("expected one backup directory during upload: %v, %v", dirs, err)
				} else if files, err := os.ReadDir(dirs[0]); err != nil || len(files) != 1 || files[0].Name() != "snapshot.tar.gz" {
					t.Errorf("expected only the compressed archive during upload: %v, %v", files, err)
				}
			}
			if failUpload {
				status = http.StatusForbidden
				break
			}
			if request.Header.Get("If-None-Match") != "*" {
				t.Error("upload could overwrite an existing backup")
			}
			if backupObjectName.MatchString(filepath.Base(key)) &&
				(!strings.HasSuffix(key, ".tar.gz") || request.Header.Get("Content-Type") != "application/gzip" || request.Header.Get("Content-Encoding") != "") {
				t.Errorf("uploaded backup is not labeled as a gzip archive: %s %v", key, request.Header)
			}
			content, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			objects[key] = storedObject{now, content}
		case http.MethodHead:
			if item, ok := objects[key]; ok {
				headers.Set("Content-Length", strconv.Itoa(len(item.data)))
			} else {
				status = http.StatusNotFound
			}
		case http.MethodDelete:
			if failDelete {
				status = http.StatusForbidden
				break
			}
			delete(objects, key)
			status = http.StatusNoContent
		default:
			t.Errorf("unexpected S3 method %s", request.Method)
		}
		return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	call := func(token, method, suffix, body string, want int) string {
		t.Helper()
		response := categoryRequest(t, server, token, method, databaseBackupRoute+suffix, body)
		if response.Code != want {
			t.Fatalf("%s %q got HTTP %d, want %d: %s", method, suffix, response.Code, want, response.Body)
		}
		return response.Body.String()
	}
	for _, route := range []struct{ method, suffix string }{
		{"GET", ""}, {"PUT", ""}, {"POST", "/test"}, {"POST", "/run"}, {"GET", "/objects"},
		{"DELETE", "/objects"}, {"POST", "/objects/download"}, {"GET", "/objects/download"},
	} {
		call(editor, route.method, route.suffix, "{}", http.StatusForbidden)
	}
	call(token, "POST", "/run", "{}", http.StatusConflict)
	const settings = `{"endpoint":"https://object.example.test","bucket":"example-backups","region":"auto","access_key":"backup-id","secret":"backup-secret","automatic":true,"daily_at":"00:00","time_zone":"UTC"}`
	call(token, "POST", "/test", settings, http.StatusOK)
	response := call(token, "PUT", "", settings, http.StatusOK)
	if len(objects) != 0 || strings.Contains(response, "backup-secret") {
		t.Fatal("connection test left an object or API returned the secret")
	}
	var config backupConfig
	if err := json.Unmarshal([]byte(response), &config); err != nil || !config.HasSecret || config.Prefix == "" {
		t.Fatalf("unexpected configuration: %s, %v", response, err)
	}
	var secret string
	if err := database.QueryRow(`SELECT secret_ciphertext FROM gocms_database_backup WHERE id=1`).Scan(&secret); err != nil || secret == "" || secret == "backup-secret" {
		t.Fatalf("credential was not encrypted: %v", err)
	}
	staleKey := config.Prefix + "daily-" + now.AddDate(-4, 0, 0).Format(time.DateOnly) + ".sqlite3"
	objects[staleKey] = storedObject{now.AddDate(-4, 0, 0), []byte("old backup")}
	for day := 1; day <= 10; day++ {
		at := now.Add(-time.Duration(day) * time.Hour)
		key := config.Prefix + fmt.Sprintf("manual-%s-ABCDEFGHIJKLMNOPQRSTUVWXYZ.sqlite3", at.Format(backupKeyTime))
		objects[key] = storedObject{at, []byte("existing backup")}
	}
	otherKey := "other-installation/daily-2000-01-01.sqlite3"
	objects[otherKey] = storedObject{now.AddDate(-4, 0, 0), []byte("other installation")}
	t.Run("compression failure keeps remote backups", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		settings, err := backup.loadConfig(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		result := backup.backup(t.Context(), settings, "")
		if result.State != "failed" || !strings.Contains(result.Message, "压缩备份失败") || len(objects) != 12 {
			t.Fatalf("unexpected result after compression failed: %+v", result)
		}
		if dirs, err := filepath.Glob(temporaryBackups); err != nil || len(dirs) != 0 {
			t.Fatalf("compression failure left temporary backups: %v, %v", dirs, err)
		}
	})
	wait := func(state, source string) {
		t.Helper()
		backup.gate.Lock()
		defer backup.gate.Unlock()
		current, err := backup.loadConfig(ctx)
		if err != nil || current.Running || current.LastRun.State != state || current.LastRun.Source != source {
			t.Fatalf("unexpected backup result %+v, %v", current.LastRun, err)
		}
		if dirs, err := filepath.Glob(temporaryBackups); err != nil || len(dirs) != 0 {
			t.Fatalf("backup left temporary backups: %v, %v", dirs, err)
		}
	}
	failUpload = true
	call(token, "POST", "/run", "{}", http.StatusAccepted)
	wait("failed", "manual")
	if len(objects) != 12 {
		t.Fatal("failed upload modified retention")
	}
	failUpload, failDelete = false, true
	call(token, "POST", "/run", "{}", http.StatusAccepted)
	wait("warning", "manual")
	if _, ok := objects[staleKey]; !ok {
		t.Fatal("cleanup unexpectedly removed a backup")
	}
	failDelete = false
	call(token, "POST", "/run", "{}", http.StatusAccepted)
	wait("success", "manual")
	if _, ok := objects[staleKey]; ok {
		t.Fatal("outdated backup was kept")
	}
	if _, ok := objects[otherKey]; !ok {
		t.Fatal("cleanup removed an unrelated object")
	}
	var archive []byte
	for key, item := range objects {
		if strings.Contains(key, "/manual-") && strings.HasSuffix(key, ".tar.gz") {
			archive = item.data
			break
		}
	}
	compressed, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatalf("manual backup was not gzip compressed: %v", err)
	}
	defer compressed.Close()
	files := tar.NewReader(compressed)
	entry, err := files.Next()
	if err != nil || entry.Name != "snapshot.sqlite3" || entry.Typeflag != tar.TypeReg {
		t.Fatalf("unexpected backup archive entry: %+v, %v", entry, err)
	}
	raw, err := io.ReadAll(files)
	if err != nil || len(archive) >= len(raw) {
		t.Fatalf("backup fixture did not compress: %d archive bytes, %d SQLite bytes, %v", len(archive), len(raw), err)
	}
	if entry, err := files.Next(); err != io.EOF {
		t.Fatalf("unexpected extra backup archive entry: %+v, %v", entry, err)
	}
	if _, err := io.Copy(io.Discard, compressed); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(t.TempDir(), "downloaded.sqlite3")
	if err := os.WriteFile(snapshot, raw, 0600); err != nil {
		t.Fatal(err)
	}
	restored, err := db.Open(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var result string
	if err := restored.QueryRow(`PRAGMA integrity_check`).Scan(&result); err != nil || result != "ok" {
		t.Fatalf("invalid SQLite backup: %s, %v", result, err)
	}
	if err := restored.QueryRow(`SELECT value FROM backup_fixture WHERE rowid=1234`).Scan(&result); err != nil || result != "committed" {
		t.Fatalf("WAL data or rowid missing: %s, %v", result, err)
	}
	backup.runScheduled(now)
	wait("success", "automatic")
	key := config.Prefix + "daily-" + now.Format(time.DateOnly) + ".tar.gz"
	before := len(objects)
	backup.runScheduled(now)
	wait("success", "automatic")
	if len(objects[key].data) == 0 || len(objects) != before {
		t.Fatal("daily backup was missing or ran twice")
	}
	list := call(token, "GET", "/objects", "", http.StatusOK)
	if !strings.Contains(list, key) || strings.Contains(list, otherKey) {
		t.Fatal("backup list did not stay in its storage prefix")
	}
	legacyKey := config.Prefix + "manual-" + now.Add(-time.Hour).Format(backupKeyTime) + "-ABCDEFGHIJKLMNOPQRSTUVWXYZ.sqlite3"
	if !strings.Contains(list, legacyKey) {
		t.Fatal("legacy SQLite backup is missing from the list")
	}
	var listing struct {
		Target string `json:"target"`
	}
	if err := json.Unmarshal([]byte(list), &listing); err != nil || listing.Target == "" {
		t.Fatalf("missing storage target: %v", err)
	}
	selectionBody := func(key, target string) string {
		raw, err := json.Marshal(backupObjectSelection{Key: key, Target: target})
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	selection := selectionBody(key, listing.Target)
	call(token, "POST", "/objects/download", selection, http.StatusOK)
	call(token, "POST", "/objects/download", selectionBody(otherKey, listing.Target), http.StatusNotFound)
	call(token, "DELETE", "/objects", selectionBody(otherKey, listing.Target), http.StatusNotFound)
	call(token, "DELETE", "/objects", selectionBody(config.Prefix+".connection-test/daily-2000-01-01.sqlite3", listing.Target), http.StatusNotFound)
	call(token, "DELETE", "/objects", selectionBody(key, "old-target"), http.StatusConflict)

	fileURL := backupDownloadRoute + "?" + url.Values{"key": {key}, "target": {listing.Target}}.Encode()
	request := httptest.NewRequest(http.MethodGet, fileURL, nil)
	request.AddCookie(&http.Cookie{Name: "gocms_admin", Value: token})
	download := httptest.NewRecorder()
	server.Handler().ServeHTTP(download, request)
	disposition, params, err := mime.ParseMediaType(download.Header().Get("Content-Disposition"))
	if download.Code != http.StatusOK || err != nil || disposition != "attachment" || params["filename"] != strings.TrimPrefix(key, config.Prefix) ||
		download.Header().Get("Cache-Control") != "private, no-store" || download.Header().Get("Content-Type") != "application/gzip" ||
		!bytes.Equal(download.Body.Bytes(), objects[key].data) {
		t.Fatal("download did not return the private snapshot as an attachment")
	}
	request = httptest.NewRequest(http.MethodGet, fileURL, nil)
	request.AddCookie(&http.Cookie{Name: "gocms_admin", Value: token})
	request.Header.Set("Range", "bytes=0-15")
	download = httptest.NewRecorder()
	server.Handler().ServeHTTP(download, request)
	if download.Code != http.StatusPartialContent || download.Header().Get("Content-Range") == "" ||
		!bytes.Equal(download.Body.Bytes(), objects[key].data[:16]) {
		t.Fatal("download did not honor the byte range")
	}

	failDelete = true
	call(token, "DELETE", "/objects", selection, http.StatusBadGateway)
	if _, ok := objects[key]; !ok {
		t.Fatal("a failed delete removed the backup")
	}
	failDelete = false
	call(token, "DELETE", "/objects", selection, http.StatusOK)
	call(token, "DELETE", "/objects", selection, http.StatusOK)
	call(token, "POST", "/objects/download", selection, http.StatusNotFound)
	if _, ok := objects[key]; ok || len(objects[otherKey].data) == 0 {
		t.Fatal("manual deletion did not stay within the selected backup")
	}
	var downloads, deletions int
	err = database.QueryRow(`SELECT COUNT(*) FILTER (WHERE method='GET' AND path=?),
		COUNT(*) FILTER (WHERE method='DELETE' AND path=?)
		FROM gocms_admin_operation WHERE site_id=0`, backupDownloadRoute, backupObjectsRoute).Scan(&downloads, &deletions)
	if err != nil || downloads != 2 || deletions == 0 {
		t.Fatalf("backup operations were not audited: downloads=%d deletions=%d, %v", downloads, deletions, err)
	}

	legacyURL := backupDownloadRoute + "?" + url.Values{"key": {legacyKey}, "target": {listing.Target}}.Encode()
	request = httptest.NewRequest(http.MethodGet, legacyURL, nil)
	request.AddCookie(&http.Cookie{Name: "gocms_admin", Value: token})
	download = httptest.NewRecorder()
	server.Handler().ServeHTTP(download, request)
	if download.Code != http.StatusOK || download.Header().Get("Content-Type") != "application/vnd.sqlite3" ||
		!bytes.Equal(download.Body.Bytes(), objects[legacyKey].data) {
		t.Fatal("legacy SQLite backup could not be downloaded")
	}
	call(token, "DELETE", "/objects", selectionBody(legacyKey, listing.Target), http.StatusOK)
	if _, exists := objects[legacyKey]; exists {
		t.Fatal("legacy SQLite backup could not be deleted")
	}

	nextDay := now.AddDate(0, 0, 1)
	nextKey := config.Prefix + "daily-" + nextDay.Format(time.DateOnly)
	objects[nextKey+".sqlite3"] = storedObject{now, raw}
	backup.runScheduled(nextDay)
	wait("success", "automatic")
	if _, exists := objects[nextKey+".tar.gz"]; exists {
		t.Fatal("the scheduler duplicated an existing legacy backup for the same day")
	}
}

func TestDatabaseBackupRetentionAndSchedule(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, loc)
	objects := make([]backupObject, 0, 84)
	for day := 0; day < 11; day++ {
		objects = append(objects, backupObject{Key: "recent", At: now.AddDate(0, 0, -day)})
	}
	for month := 1; month <= 37; month++ {
		previous := now.AddDate(0, -month, 0)
		objects = append(objects, backupObject{Key: "older", At: previous})
		objects = append(objects, backupObject{Key: "newer", At: previous.Add(time.Hour)})
	}
	classifyBackupObjects(objects, now)
	recent, monthly := 0, 0
	for _, object := range objects {
		if object.Recent {
			recent++
		}
		if object.Monthly {
			monthly++
			if object.Key != "newer" || object.At.Before(now.AddDate(0, -36, 0)) {
				t.Fatalf("wrong monthly backup: %+v", object)
			}
		}
	}
	if recent != 10 || monthly != 36 {
		t.Fatalf("retained %d recent and %d monthly backups", recent, monthly)
	}
	atBoundary := time.Date(2026, 10, 31, 16, 0, 0, 0, time.UTC)
	objects = []backupObject{{At: atBoundary.Add(-time.Second)}, {At: atBoundary}}
	classifyBackupObjects(objects, now)
	if objects[0].Monthly || !objects[1].Monthly || !objects[1].Recent {
		t.Fatal("retention ignored the local month boundary or removed the overlap")
	}

	config := backupConfig{HasSecret: true, backupSettings: backupSettings{Automatic: true, DailyAt: "09:00", TimeZone: loc.String()}}
	if scheduledBackupDay(config, now.Add(-time.Minute)) != "" || scheduledBackupDay(config, now) != "2026-11-20" ||
		scheduledBackupDay(config, now.Add(time.Hour)) != "2026-11-20" {
		t.Fatal("daily scheduler did not honor its local time or same-day catch-up")
	}
	config.lastScheduledDay = "2026-11-20"
	if scheduledBackupDay(config, now.Add(time.Hour)) != "" || scheduledBackupDay(config, now.AddDate(0, 0, 1)) != "2026-11-21" {
		t.Fatal("daily scheduler did not honor its last successful day")
	}
}
