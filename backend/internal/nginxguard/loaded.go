package nginxguard

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type diagnosticOutput struct{ bytes.Buffer }

func (b *diagnosticOutput) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := 2048 - b.Len(); remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.Buffer.Write(p)
	}
	return n, nil
}

func canonicalConfigPath(path, base string) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

// Called only after Nginx confirms the site is not included. Place the include
// last so the site can use log formats and other earlier http declarations.
func includeUnloadedSite(data []byte, path string) ([]byte, error) {
	_, end, err := blockBounds(data, "http")
	if err != nil {
		return nil, err
	}
	result := append([]byte{}, data[:end]...)
	result = append(result, []byte("\n    include "+path+"; # GoCMS security\n")...)
	return append(result, data[end:]...), nil
}

// configurationLoadsSite asks Nginx which files it actually loads. This handles
// quoted/relative includes, globs and nested includes using Nginx's own parser.
// Dumped configuration bodies may contain secrets: inspect only file headers,
// never persist them or include stdout in an error returned to the admin.
func configurationLoadsSite(c Config) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.NginxBinary, "-T", "-c", c.NginxConfig)
	var diagnostic diagnosticOutput
	cmd.Stderr = &diagnostic
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return false, err
	}
	if err := cmd.Start(); err != nil {
		return false, err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	base := filepath.Dir(c.NginxConfig)
	main, site := canonicalConfigPath(c.NginxConfig, base), canonicalConfigPath(c.ServerConfig, base)
	seenMain, seenSite := false, false
	for scanner.Scan() {
		line := scanner.Text()
		const prefix = "# configuration file "
		if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, ":") {
			continue
		}
		path := canonicalConfigPath(strings.TrimSuffix(strings.TrimPrefix(line, prefix), ":"), base)
		seenMain = seenMain || path == main
		seenSite = seenSite || path == site
	}
	if scanner.Err() != nil {
		cancel()
	}
	if err := cmd.Wait(); err != nil {
		return false, fmt.Errorf("Nginx 配置检查（-T）失败：%s（%w）", strings.TrimSpace(diagnostic.String()), err)
	}
	if scanner.Err() != nil {
		return false, fmt.Errorf("无法读取 Nginx 配置文件清单：%w", scanner.Err())
	}
	if !seenMain {
		return false, fmt.Errorf("Nginx 未返回所选主配置的文件清单，无法确认站点引用")
	}
	return seenSite, nil
}
