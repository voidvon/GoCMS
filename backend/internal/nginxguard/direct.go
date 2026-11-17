package nginxguard

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"time"
)

// Direct keeps security rules in the selected site file and adds a main-config
// include only when Nginx does not already load that file.
type Direct struct {
	mu      sync.Mutex
	engine  *engine
	ready   bool
	targets []serverTarget
}

func NewDirect(c Config) *Direct {
	return &Direct{engine: &engine{config: c, backupDir: stateDir, logPath: logFile}}
}

func ExecutionUser() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return fmt.Sprintf("uid=%d", os.Geteuid())
}

type savedFile struct {
	path   string
	data   []byte
	mode   os.FileMode
	exists bool
}

func rememberFile(path string) (savedFile, error) {
	f := savedFile{path: path, mode: 0600}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return f, nil
	}
	if err != nil {
		return f, err
	}
	if !info.Mode().IsRegular() {
		return f, fmt.Errorf("配置必须为普通文件：%s", path)
	}
	f.exists, f.mode = true, info.Mode().Perm()
	f.data, err = os.ReadFile(path)
	return f, err
}

func (f savedFile) restore() error {
	if f.exists {
		return atomicFile(f.path, f.data, f.mode)
	}
	if err := os.Remove(f.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (d *Direct) initialize() error {
	a := d.engine
	if err := a.config.Validate(); err != nil {
		return err
	}
	server, err := rememberFile(a.config.ServerConfig)
	if err != nil {
		return err
	}
	if !server.exists {
		return fmt.Errorf("站点配置不存在：%s", a.config.ServerConfig)
	}
	if _, err := RenderSingleFile(server.data, Snapshot{}, time.Now(), a.logPath); err != nil {
		return err
	}
	for _, dir := range []string{a.backupDir, filepath.Dir(a.logPath)} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		if info, err := os.Lstat(dir); err != nil || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("安全配置和日志目录不能为符号链接或允许其他用户写入：%s", dir)
		}
	}
	backup := filepath.Join(a.backupDir, "server.conf.before")
	if _, err := os.Lstat(backup); os.IsNotExist(err) {
		return atomicFile(backup, server.data, 0600)
	} else {
		return err
	}
}

func (d *Direct) apply(s Snapshot) error {
	if err := Validate(s); err != nil {
		return err
	}
	if len(d.targets) > 0 {
		return d.applyDedicated(s)
	}
	if !d.ready {
		if err := d.initialize(); err != nil {
			return err
		}
		d.ready = true
	}
	a := d.engine
	previous, err := rememberFile(a.config.ServerConfig)
	if err != nil {
		return err
	}
	if !previous.exists {
		return fmt.Errorf("站点配置不存在：%s", a.config.ServerConfig)
	}
	updated, err := RenderSingleFile(previous.data, s, time.Now(), a.logPath)
	if err != nil {
		return err
	}
	loaded, err := configurationLoadsSite(a.config)
	if err != nil {
		return err
	}
	var main *savedFile
	var mainUpdated []byte
	if !loaded {
		file, err := rememberFile(a.config.NginxConfig)
		if err != nil {
			return err
		}
		if !file.exists {
			return fmt.Errorf("Nginx 主配置不存在：%s", file.path)
		}
		mainUpdated, err = includeUnloadedSite(file.data, a.config.ServerConfig)
		if err != nil {
			return err
		}
		backup := filepath.Join(a.backupDir, "nginx.conf.before")
		if _, err := os.Lstat(backup); os.IsNotExist(err) {
			if err := atomicFile(backup, file.data, 0600); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		main = &file
	}
	mainWritten, siteWritten := false, false
	rollback := func(cause error) error {
		if siteWritten {
			cause = errors.Join(cause, previous.restore())
		}
		if mainWritten {
			cause = errors.Join(cause, main.restore())
		}
		return cause
	}
	if main != nil && !bytes.Equal(main.data, mainUpdated) {
		if err := atomicFile(main.path, mainUpdated, main.mode); err != nil {
			return err
		}
		mainWritten = true
	}
	if !bytes.Equal(previous.data, updated) {
		if err := atomicFile(previous.path, updated, previous.mode); err != nil {
			return rollback(err)
		}
		siteWritten = true
	}
	if mainWritten || siteWritten {
		loaded, err = configurationLoadsSite(a.config)
		if err != nil {
			return rollback(err)
		}
		if !loaded {
			return rollback(errors.New("Nginx 检查后仍未加载所选站点配置，已恢复原文件"))
		}
		if err := command(a.config, "reload"); err != nil {
			return rollback(err)
		}
	} else if !loaded {
		return errors.New("Nginx 未加载所选站点配置，无法自动添加引用")
	}
	a.result = Result{Digest: Digest(s), AppliedAt: time.Now().UTC().Format(time.RFC3339)}
	return nil
}

func (d *Direct) Apply(s Snapshot) (Result, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	err := d.apply(s)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return Result{}, fmt.Errorf("GoCMS 当前系统用户 %s 无权直接管理 Nginx，请检查配置文件写入、日志读取和 Nginx 检查/重载权限：%w", ExecutionUser(), err)
		}
		return Result{}, fmt.Errorf("GoCMS 直接同步 Nginx 失败（系统用户 %s）：%w", ExecutionUser(), err)
	}
	return d.engine.result, nil
}

func (d *Direct) Events(cursor string) (LogBatch, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	batch, err := d.engine.events(cursor)
	if err != nil {
		return batch, err
	}
	// Bound the direct-mode probe log without installing a system logrotate
	// configuration. Truncate the same inode, retaining Nginx's open descriptor.
	f, err := os.OpenFile(d.engine.logPath, os.O_WRONLY, 0)
	if os.IsNotExist(err) {
		return batch, nil
	}
	if err != nil {
		return batch, err
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil {
		return batch, err
	} else if info.Size() >= 20<<20 {
		if err := f.Truncate(0); err != nil {
			return batch, err
		}
		batch.Cursor = ""
	}
	return batch, nil
}
