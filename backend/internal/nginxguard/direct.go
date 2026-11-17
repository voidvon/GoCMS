package nginxguard

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Direct updates only the selected Nginx site configuration. It runs within
// GoCMS, using the process's existing operating-system permissions.
type Direct struct {
	mu     sync.Mutex
	engine *agent
	ready  bool
}

func NewDirect(c Config) *Direct {
	return &Direct{engine: &agent{config: c, statePath: agentDir + "/snapshot.json", logPath: logFile}}
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
	if info, err := os.Lstat(a.config.SocketPath); err == nil && info.Mode()&os.ModeSocket != 0 {
		connection, err := net.DialTimeout("unix", a.config.SocketPath, time.Second)
		if err == nil {
			connection.Close()
			return errors.New("检测到旧版 Nginx 同步代理，请先关闭代理同步并停止 gocms-security-agent 服务，再使用 GoCMS 直接管理")
		}
		if !errors.Is(err, syscall.ECONNREFUSED) && !os.IsNotExist(err) {
			return fmt.Errorf("无法确认旧版同步代理是否已停止：%w", err)
		}
	}
	if err := command(a.config, "test"); err != nil {
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
	for _, dir := range []string{filepath.Dir(a.statePath), filepath.Dir(a.logPath)} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		if info, err := os.Lstat(dir); err != nil || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("安全配置和日志目录不能为符号链接或允许其他用户写入：%s", dir)
		}
	}
	backup := filepath.Join(filepath.Dir(a.statePath), "server.conf.before")
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
	if !bytes.Equal(previous.data, updated) {
		if err := atomicFile(previous.path, updated, previous.mode); err != nil {
			return err
		}
		if err = command(a.config, "test"); err == nil {
			err = command(a.config, "reload")
		}
		if err != nil {
			return errors.Join(err, previous.restore())
		}
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
