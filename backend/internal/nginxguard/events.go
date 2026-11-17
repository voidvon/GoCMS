package nginxguard

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type engine struct {
	config        Config
	result        Result
	logInfo       os.FileInfo
	logGeneration string
	backupDir     string
	logPath       string
}

func (a *engine) events(cursor string) (LogBatch, error) {
	batch := LogBatch{Items: []LogEvent{}, Cursor: cursor}
	f, err := os.Open(a.logPath)
	if os.IsNotExist(err) {
		return batch, nil
	}
	if err != nil {
		return batch, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return batch, err
	}
	if a.logInfo == nil || !os.SameFile(a.logInfo, info) {
		a.logGeneration = logIdentity(info)
		a.logInfo = info
	}
	var offset int64
	if parts := strings.Split(cursor, ":"); len(parts) == 2 && parts[0] == a.logGeneration {
		offset, _ = strconv.ParseInt(parts[1], 10, 64)
	}
	if offset < 0 || offset > info.Size() {
		offset = 0
	}
	if offset == 0 && info.Size() > 1<<20 {
		offset = info.Size() - (1 << 20)
		if _, err = f.Seek(offset, io.SeekStart); err != nil {
			return batch, err
		}
		reader := bufio.NewReader(f)
		line, _ := reader.ReadBytes('\n')
		offset += int64(len(line))
	}
	if _, err = f.Seek(offset, io.SeekStart); err != nil {
		return batch, err
	}
	reader := bufio.NewReader(io.LimitReader(f, 1<<20))
	for i := 0; i < 500; i++ {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			break
		}
		offset += int64(len(line))
		if len(line) > 8192 {
			continue
		}
		var event LogEvent
		if json.Unmarshal(line, &event) == nil {
			batch.Items = append(batch.Items, event)
		}
	}
	batch.Cursor = a.logGeneration + ":" + strconv.FormatInt(offset, 10)
	return batch, nil
}

// The device/inode identity survives process restarts and changes on logrotate.
// Reflection keeps this file buildable on platforms with different Stat types;
// the privileged command is only installed on Linux.
func logIdentity(info os.FileInfo) string {
	value := reflect.Indirect(reflect.ValueOf(info.Sys()))
	if value.IsValid() && value.Kind() == reflect.Struct {
		dev, ino := value.FieldByName("Dev"), value.FieldByName("Ino")
		if dev.IsValid() && ino.IsValid() && dev.CanInterface() && ino.CanInterface() {
			return fmt.Sprintf("%v-%v", dev.Interface(), ino.Interface())
		}
	}
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}
