package nginxguard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const stateDir = "/etc/gocms-security"
const logFile = "/var/log/gocms-security/probes.log"
const maxConfigBytes = 8 << 20

type ConfigFile struct {
	Path       string `json:"path"`
	Content    string `json:"content"`
	ModifiedAt string `json:"modified_at"`
}

func readConfigFile(path string, owner int) (ConfigFile, error) {
	var result ConfigFile
	if err := checkTrustedPath(path, false, owner); err != nil {
		if os.IsNotExist(err) {
			return result, fmt.Errorf("安全规则文件不存在（%s），请先以 root 执行后台提供的 nginx setup 命令", path)
		}
		return result, err
	}
	f, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return result, err
	}
	if info.Size() > maxConfigBytes {
		return result, errors.New("安全规则文件超过 8 MiB，无法在线查看完整内容")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if err != nil {
		return result, err
	}
	if len(data) > maxConfigBytes {
		return result, errors.New("安全规则文件超过 8 MiB，无法在线查看完整内容")
	}
	if !utf8.Valid(data) {
		return result, errors.New("安全规则文件不是有效的 UTF-8 文本，无法在线查看完整内容")
	}
	return ConfigFile{Path: path, Content: string(data), ModifiedAt: info.ModTime().UTC().Format(time.RFC3339Nano)}, nil
}

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
