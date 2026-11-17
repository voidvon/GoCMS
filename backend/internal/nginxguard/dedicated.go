package nginxguard

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const dedicatedHeader = "# GOCMS-SECURITY-RULES\n"
const logBegin = "\n# GOCMS-SECURITY-LOG-BEGIN\n"
const logEnd = "# GOCMS-SECURITY-LOG-END\n"

// Targets are discovered only during root setup and pinned in control.json.
// The web process cannot nominate additional files for privileged writes.
type serverTarget struct {
	Path  string   `json:"path"`
	Names []string `json:"names"`
}

func matchesServerName(name, host string) bool {
	name, host = strings.ToLower(name), strings.ToLower(host)
	if name == host {
		return true
	}
	if strings.HasPrefix(name, "*.") {
		return strings.HasSuffix(host, name[1:]) && len(host) > len(name)-1
	}
	if strings.HasPrefix(name, ".") {
		return host == name[1:] || strings.HasSuffix(host, name)
	}
	if strings.HasSuffix(name, ".*") && !strings.Contains(host, "*") {
		return strings.HasPrefix(host, strings.TrimSuffix(name, "*"))
	}
	return false
}

func coversHosts(names []string, s Snapshot) error {
	for _, tenant := range s.Tenants {
		for _, host := range tenant.Hosts {
			found := false
			for _, name := range names {
				found = found || matchesServerName(name, host)
			}
			if !found {
				return fmt.Errorf("未找到域名 %s 对应的已授权 Nginx 站点，请确认宝塔站点已配置该域名后重新执行 nginx setup", host)
			}
		}
	}
	return nil
}

func discoverTargets(c Config, s Snapshot, owner int) ([]serverTarget, error) {
	files, err := configurationFiles(c)
	if err != nil {
		return nil, err
	}
	var paths []string
	for path := range files {
		if path != c.ServerConfig {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	var targets []serverTarget
	var allNames []string
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		servers, err := httpServers(data)
		if err != nil {
			return nil, fmt.Errorf("读取 Nginx 站点结构失败（%s）：%w", path, err)
		}
		names := map[string]bool{}
		for _, server := range servers {
			for _, name := range server.names {
				for _, tenant := range s.Tenants {
					for _, host := range tenant.Hosts {
						if matchesServerName(name, host) {
							names[name] = true
						}
					}
				}
			}
		}
		if len(names) == 0 {
			continue
		}
		if err := checkTrustedPath(path, false, owner); err != nil {
			return nil, err
		}
		target := serverTarget{Path: path}
		for name := range names {
			target.Names = append(target.Names, name)
		}
		sort.Strings(target.Names)
		targets = append(targets, target)
		allNames = append(allNames, target.Names...)
	}
	if err := coversHosts(allNames, s); err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, errors.New("未找到 GoCMS 对应的 Nginx 站点，请先在后台配置站点域名，并确认宝塔已为这些域名配置站点；无需手工创建 gocms.conf")
	}
	return targets, nil
}

func renderTarget(data []byte, target serverTarget, hook string) ([]byte, []string, error) {
	servers, err := httpServers(data)
	if err != nil {
		return nil, nil, err
	}
	var names []string
	result := append([]byte{}, data...)
	for i := len(servers) - 1; i >= 0; i-- {
		server := servers[i]
		matched := false
		for _, name := range server.names {
			for _, authorized := range target.Names {
				matched = matched || name == authorized
			}
		}
		if !matched {
			continue
		}
		body, err := removeManagedBlock(string(data[server.start:server.end]), serverBegin, serverEnd)
		if err != nil {
			return nil, nil, err
		}
		result = append(append(append([]byte{}, result[:server.start]...), []byte(hook+body)...), result[server.end:]...)
		names = append(names, server.names...)
	}
	if len(names) == 0 {
		return nil, nil, fmt.Errorf("已授权站点 %s 的 server_name 已变更，请重新执行 nginx setup", target.Path)
	}
	return result, names, nil
}

func dedicatedFragments(s Snapshot, logPath string) (rules []byte, hook, format string, err error) {
	h, hook, err := SingleFileFragments(s, time.Now(), logPath)
	if err != nil {
		return nil, "", "", err
	}
	// log_format must be declared before access_log is parsed. A panel wildcard
	// may load gocms.conf after its vhosts, so keep this fixed declaration first
	// in the main http block. All variable maps and lists stay in gocms.conf.
	for _, line := range strings.Split(h, "\n") {
		if strings.HasPrefix(line, "log_format ") {
			format = line + "\n"
			h = strings.Replace(h, format, "", 1)
			break
		}
	}
	return []byte(dedicatedHeader + h), hook, format, nil
}

func prepareDedicatedMain(data []byte, format string) ([]byte, error) {
	text, err := removeManagedBlock(string(data), logBegin, logEnd)
	if err != nil {
		return nil, err
	}
	start, _, err := blockBounds([]byte(text), "http")
	if err != nil {
		return nil, err
	}
	return []byte(text[:start] + logBegin + format + logEnd + text[start:]), nil
}

func saveFirstBackup(dir, name string, file savedFile) error {
	if !file.exists {
		return nil
	}
	path := filepath.Join(dir, name)
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return atomicFile(path, file.data, 0600)
	} else {
		return err
	}
}

func (d *Direct) applyDedicated(s Snapshot) error {
	a := d.engine
	rules, hook, format, err := dedicatedFragments(s, a.logPath)
	if err != nil {
		return err
	}
	for _, dir := range []string{a.backupDir, filepath.Dir(a.logPath)} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	// Build every change before writing, including main-file inline servers.
	paths := []string{a.config.ServerConfig, a.config.NginxConfig}
	for _, target := range d.targets {
		if target.Path != a.config.NginxConfig {
			paths = append(paths, target.Path)
		}
	}
	previous := map[string]savedFile{}
	updated := map[string][]byte{}
	for _, path := range paths {
		file, err := rememberFile(path)
		if err != nil {
			return err
		}
		if !file.exists && path != a.config.ServerConfig {
			return fmt.Errorf("已授权 Nginx 配置不存在（%s），请重新执行 nginx setup", path)
		}
		previous[path], updated[path] = file, file.data
	}
	updated[a.config.ServerConfig] = rules
	var names []string
	for _, target := range d.targets {
		data, found, err := renderTarget(updated[target.Path], target, hook)
		if err != nil {
			return err
		}
		updated[target.Path] = data
		names = append(names, found...)
	}
	if err := coversHosts(names, s); err != nil {
		return err
	}
	main, err := prepareDedicatedMain(updated[a.config.NginxConfig], format)
	if err != nil {
		return err
	}
	files, err := configurationFiles(a.config)
	loaded := files[canonicalConfigPath(a.config.ServerConfig, filepath.Dir(a.config.NginxConfig))]
	if err != nil {
		loaded, err = configurationReferencesFile(a.config)
		if err != nil {
			return err
		}
		files = nil
	}
	if !loaded {
		main, err = includeUnloadedSite(main, a.config.ServerConfig)
		if err != nil {
			return err
		}
	}
	updated[a.config.NginxConfig] = main
	var changed []savedFile
	rollback := func(cause error) error {
		for i := len(changed) - 1; i >= 0; i-- {
			cause = errors.Join(cause, changed[i].restore())
		}
		return cause
	}
	for _, path := range paths {
		file := previous[path]
		if file.exists && bytes.Equal(file.data, updated[path]) {
			continue
		}
		name := fmt.Sprintf("vhost-%x.before", sha256.Sum256([]byte(path)))
		if path == a.config.NginxConfig {
			name = "nginx.conf.before"
		}
		if path != a.config.ServerConfig {
			if err := saveFirstBackup(a.backupDir, name, file); err != nil {
				return rollback(err)
			}
		}
		if err := atomicFile(path, updated[path], file.mode); err != nil {
			return rollback(err)
		}
		changed = append(changed, file)
	}
	if len(changed) > 0 || files == nil {
		files, err = configurationFiles(a.config)
		if err != nil {
			return rollback(err)
		}
	}
	if !files[canonicalConfigPath(a.config.ServerConfig, filepath.Dir(a.config.NginxConfig))] {
		return rollback(errors.New("Nginx 未加载 GoCMS 规则文件"))
	}
	for _, target := range d.targets {
		if !files[canonicalConfigPath(target.Path, filepath.Dir(a.config.NginxConfig))] {
			return rollback(fmt.Errorf("Nginx 未加载已授权站点 %s，请重新执行 nginx setup", target.Path))
		}
	}
	if len(changed) > 0 {
		if err := command(a.config, "reload"); err != nil {
			return rollback(err)
		}
	}
	a.result = Result{Digest: Digest(s), AppliedAt: time.Now().UTC().Format(time.RFC3339)}
	return nil
}
