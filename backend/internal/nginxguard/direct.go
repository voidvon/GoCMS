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

// Direct manages the same dedicated includes as the optional agent, within
// the GoCMS process. Commands run with GoCMS's existing operating-system rights.
type Direct struct {
	mu       sync.Mutex
	engine   *agent
	ready    bool
	rotation string
}

func NewDirect(c Config) *Direct {
	return &Direct{engine: &agent{config: c, httpPath: httpFile, serverPath: serverFile, statePath: agentDir + "/snapshot.json", logPath: logFile}, rotation: "/etc/logrotate.d/gocms-security"}
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

func (d *Direct) initialize(s Snapshot) (err error) {
	a := d.engine
	if err := a.config.Validate(); err != nil {
		return err
	}
	if err := Validate(s); err != nil {
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
	main, err := rememberFile(a.config.NginxConfig)
	if err != nil {
		return err
	}
	server, err := rememberFile(a.config.ServerConfig)
	if err != nil {
		return err
	}
	mainNew, err := IncludeInBlock(main.data, "http", a.httpPath)
	if err != nil {
		return err
	}
	serverNew, err := IncludeInBlock(server.data, "server", a.serverPath)
	if err != nil {
		return err
	}
	for _, dir := range []string{filepath.Dir(a.httpPath), filepath.Dir(a.logPath)} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		if info, err := os.Lstat(dir); err != nil || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("安全配置和日志目录不能为符号链接或允许其他用户写入：%s", dir)
		}
	}
	files := []savedFile{main, server}
	for _, path := range []string{a.httpPath, a.serverPath, a.statePath} {
		f, err := rememberFile(path)
		if err != nil {
			return err
		}
		files = append(files, f)
	}
	if d.rotation != "" {
		if info, err := os.Stat(filepath.Dir(d.rotation)); err == nil && info.IsDir() {
			f, err := rememberFile(d.rotation)
			if err != nil {
				return err
			}
			files = append(files, f)
		}
	}
	// A permanent backup is created once; per-operation rollback uses the exact
	// files read above, so panel changes made since installation remain intact.
	for name, data := range map[string][]byte{"nginx.conf.before": main.data, "server.conf.before": server.data} {
		path := filepath.Join(filepath.Dir(a.statePath), name)
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			if err := atomicFile(path, data, 0600); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}
	defer func() {
		if err == nil {
			return
		}
		for i := len(files) - 1; i >= 0; i-- {
			if restoreErr := files[i].restore(); restoreErr != nil {
				err = errors.Join(err, fmt.Errorf("恢复 %s：%w", files[i].path, restoreErr))
			}
		}
		// apply sets rendered only after a successful reload. A later state-save
		// failure also needs a reload of the restored files.
		if a.rendered != "" {
			if reloadErr := command(a.config, "reload"); reloadErr != nil {
				err = errors.Join(err, reloadErr)
			}
		}
		a.rendered, a.result = "", Result{}
	}()
	h, serverText, err := Render(Snapshot{}, time.Now(), a.logPath)
	if err != nil {
		return err
	}
	for _, f := range files {
		var data []byte
		switch f.path {
		case main.path:
			data = mainNew
		case server.path:
			data = serverNew
		case a.httpPath:
			if !f.exists {
				data = []byte(h)
			}
		case a.serverPath:
			if !f.exists {
				data = []byte(serverText)
			}
		case d.rotation:
			data = []byte(rotationConfig(a.config, a.logPath))
		}
		if data != nil && !bytes.Equal(data, f.data) {
			if err := atomicFile(f.path, data, f.mode); err != nil {
				return err
			}
		}
	}
	return a.apply(s)
}

func (d *Direct) Apply(s Snapshot) (Result, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var err error
	if d.ready {
		err = d.engine.apply(s)
	} else {
		err = d.initialize(s)
		d.ready = err == nil
	}
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
	return d.engine.events(cursor)
}
