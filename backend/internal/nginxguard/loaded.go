package nginxguard

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
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
	files, err := configurationFiles(c)
	return files[canonicalConfigPath(c.ServerConfig, filepath.Dir(c.NginxConfig))], err
}

func configurationFiles(c Config) (map[string]bool, error) {
	files := map[string]bool{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.NginxBinary, "-T", "-c", c.NginxConfig)
	cmd.Env = commandEnvironment()
	var diagnostic diagnosticOutput
	cmd.Stderr = &diagnostic
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	base := filepath.Dir(c.NginxConfig)
	main := canonicalConfigPath(c.NginxConfig, base)
	for scanner.Scan() {
		line := scanner.Text()
		const prefix = "# configuration file "
		if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, ":") {
			continue
		}
		path := canonicalConfigPath(strings.TrimSuffix(strings.TrimPrefix(line, prefix), ":"), base)
		files[path] = true
	}
	if scanner.Err() != nil {
		cancel()
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("Nginx 配置检查（-T）失败：%s（%w）", strings.TrimSpace(diagnostic.String()), err)
	}
	if scanner.Err() != nil {
		return nil, fmt.Errorf("无法读取 Nginx 配置文件清单：%w", scanner.Err())
	}
	if !files[main] {
		return nil, fmt.Errorf("Nginx 未返回所选主配置的文件清单，无法确认站点引用")
	}
	return files, nil
}

// A panel edit can remove the rules include or fixed log declaration, making
// -T fail on the remaining hooks. Inspect include directives only to prepare a
// repair; the final Nginx -T remains mandatory before reloading or acknowledging.
func configurationReferencesFile(c Config) (bool, error) {
	base := filepath.Dir(c.NginxConfig)
	target := canonicalConfigPath(c.ServerConfig, base)
	visited := map[string]bool{}
	var visit func(string, int) (bool, error)
	visit = func(path string, depth int) (bool, error) {
		path = canonicalConfigPath(path, base)
		if path == target {
			return true, nil
		}
		if visited[path] {
			return false, nil
		}
		if depth > 64 || len(visited) >= 4096 {
			return false, fmt.Errorf("Nginx include 层级或文件数量超出限制")
		}
		visited[path] = true
		data, err := os.ReadFile(path)
		if err != nil {
			return false, err
		}
		tokens, err := configTokens(data)
		if err != nil {
			return false, err
		}
		var statement []string
		for _, token := range tokens {
			if !token.symbol {
				statement = append(statement, token.value)
				continue
			}
			if token.value == ";" && len(statement) == 2 && statement[0] == "include" {
				pattern := statement[1]
				if !filepath.IsAbs(pattern) {
					pattern = filepath.Join(base, pattern)
				}
				paths, err := filepath.Glob(pattern)
				if err != nil {
					return false, err
				}
				for _, included := range paths {
					found, err := visit(included, depth+1)
					if err != nil || found {
						return found, err
					}
				}
			}
			statement = nil
		}
		return false, nil
	}
	return visit(c.NginxConfig, 0)
}
