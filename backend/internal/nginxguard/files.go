package nginxguard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const stateDir = "/etc/gocms-security"
const logFile = "/var/log/gocms-security/probes.log"

type Config struct {
	NginxBinary  string `json:"nginx_binary"`
	NginxConfig  string `json:"nginx_config"`
	ServerConfig string `json:"server_config"`
	RunUser      string `json:"run_user"`
}

func (c Config) Validate() error {
	for _, p := range []string{c.NginxBinary, c.NginxConfig, c.ServerConfig} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p || len(p) > 512 || strings.ContainsAny(p, " \t\r\n\x00\"'`;${}\\#*?[]") {
			return errors.New("Nginx 路径必须为不含特殊字符的绝对路径")
		}
	}
	if c.NginxConfig == c.ServerConfig || c.RunUser == "" || len(c.RunUser) > 64 || strings.Trim(c.RunUser, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-") != "" {
		return errors.New("Nginx 配置路径或 GoCMS 运行用户无效")
	}
	return nil
}

func commandEnvironment() []string {
	return []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
}

func command(c Config, mode string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	args := []string{"-t", "-c", c.NginxConfig}
	if mode != "test" {
		args = []string{"-s", mode, "-c", c.NginxConfig}
	}
	cmd := exec.CommandContext(ctx, c.NginxBinary, args...)
	cmd.Env = commandEnvironment()
	out, err := cmd.CombinedOutput()
	if err != nil {
		if len(out) > 2048 {
			out = out[:2048]
		}
		return fmt.Errorf("nginx %s: %s (%w)", mode, strings.TrimSpace(string(out)), err)
	}
	return nil
}

func atomicFile(path string, data []byte, mode os.FileMode) error {
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return errors.New("refusing to replace non-regular file")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".gocms-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
