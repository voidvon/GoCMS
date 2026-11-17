package nginxguard

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"
)

// IncludeInBlock preserves panel-managed text and only inserts our include at
// the beginning of exactly one top-level block. Quotes/comments are skipped.
func IncludeInBlock(data []byte, block, include string) ([]byte, error) {
	line := "include " + include + ";"
	if strings.Contains(string(data), "\n    "+line+" # GoCMS security\n") {
		return data, nil
	}
	offset, err := blockOffset(data, block)
	if err != nil {
		return nil, err
	}
	insert := []byte("\n    " + line + " # GoCMS security\n")
	result := append([]byte{}, data[:offset]...)
	result = append(result, insert...)
	result = append(result, data[offset:]...)
	return result, nil
}

func blockOffset(data []byte, block string) (int, error) {
	depth, count, offset := 0, 0, -1
	previous := ""
	for i := 0; i < len(data); {
		c := data[i]
		if c == '#' {
			for i < len(data) && data[i] != '\n' {
				i++
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote := c
			i++
			for i < len(data) {
				if data[i] == '\\' {
					i += 2
					continue
				}
				if data[i] == quote {
					i++
					break
				}
				i++
			}
			previous = ""
			continue
		}
		if c == '{' {
			if depth == 0 && previous == block {
				count++
				offset = i + 1
			}
			depth++
			previous = ""
			i++
			continue
		}
		if c == '}' {
			depth--
			if depth < 0 {
				return 0, errors.New("unbalanced Nginx blocks")
			}
			previous = ""
			i++
			continue
		}
		if c == ';' {
			previous = ""
			i++
			continue
		}
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			i++
			continue
		}
		start := i
		for i < len(data) && !strings.ContainsRune(" \t\r\n{};#\"'", rune(data[i])) {
			if data[i] == '\\' && i+1 < len(data) {
				i++
			}
			i++
		}
		if start == i {
			i++
		}
		previous = string(data[start:i])
	}
	if depth != 0 || count != 1 {
		return 0, fmt.Errorf("expected exactly one top-level %s block, found %d", block, count)
	}
	return offset, nil
}

// Install is an explicit, one-time root operation from the downloaded bundle.
// Later API calls cannot change paths, executable names, or installation files.
func Install(c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if _, err := user.Lookup(c.AgentUser); err != nil {
		return fmt.Errorf("GoCMS service user: %w", err)
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errors.New("this installer requires Linux with systemd")
	}
	if _, err := os.Stat(c.NginxBinary); err != nil {
		return err
	}
	if err := command(c, "test"); err != nil {
		return err
	}
	mainData, err := os.ReadFile(c.NginxConfig)
	if err != nil {
		return err
	}
	serverData, err := os.ReadFile(c.ServerConfig)
	if err != nil {
		return err
	}
	mainNew, err := IncludeInBlock(mainData, "http", httpFile)
	if err != nil {
		return err
	}
	serverNew, err := IncludeInBlock(serverData, "server", serverFile)
	if err != nil {
		return err
	}
	for _, dir := range []string{agentDir, "/usr/local/libexec", "/var/log/gocms-security"} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	if err := os.Chmod(agentDir, 0700); err != nil {
		return err
	}
	for name, data := range map[string][]byte{"nginx.conf.before": mainData, "server.conf.before": serverData} {
		path := filepath.Join(agentDir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if err := atomicFile(path, data, 0600); err != nil {
				return err
			}
		}
	}
	snapshot := Snapshot{Tenants: []Tenant{}, Rules: []Rule{}}
	if old, err := os.ReadFile(agentDir + "/snapshot.json"); err == nil {
		if err := json.Unmarshal(old, &snapshot); err != nil {
			return err
		}
	}
	h, s, err := Render(snapshot, time.Now(), logFile)
	if err != nil {
		return err
	}
	if err := atomicFile(httpFile, []byte(h), 0600); err != nil {
		return err
	}
	if err := atomicFile(serverFile, []byte(s), 0600); err != nil {
		return err
	}
	rollback := func() {
		_ = atomicFile(c.NginxConfig, mainData, 0644)
		_ = atomicFile(c.ServerConfig, serverData, 0644)
	}
	if err := atomicFile(c.NginxConfig, mainNew, 0644); err != nil {
		return err
	}
	if err := atomicFile(c.ServerConfig, serverNew, 0644); err != nil {
		rollback()
		return err
	}
	if err := command(c, "test"); err != nil {
		rollback()
		return err
	}
	if err := command(c, "reload"); err != nil {
		rollback()
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	binary, err := os.ReadFile(executable)
	if err != nil {
		return err
	}
	const installed = "/usr/local/libexec/gocms-security-agent"
	if err := atomicFile(installed, binary, 0755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicFile(agentDir+"/agent.json", raw, 0600); err != nil {
		return err
	}
	unit := "[Unit]\nDescription=GoCMS Nginx security agent\nAfter=network.target\n\n[Service]\nType=simple\nUser=root\nExecStart=" + installed + " security-agent serve -config " + agentDir + "/agent.json\nRestart=on-failure\nRestartSec=5\nUMask=0077\n\n[Install]\nWantedBy=multi-user.target\n"
	if err := atomicFile("/etc/systemd/system/gocms-security-agent.service", []byte(unit), 0644); err != nil {
		return err
	}
	if _, err := os.Stat("/etc/logrotate.d"); err == nil {
		if err := atomicFile("/etc/logrotate.d/gocms-security", []byte(rotationConfig(c, logFile)), 0644); err != nil {
			return err
		}
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", "gocms-security-agent"}, {"restart", "gocms-security-agent"}} {
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl: %s: %w", out, err)
		}
	}
	fmt.Println("GoCMS security agent installed. Return to Security Protection and test synchronization.")
	return nil
}

func rotationConfig(c Config, path string) string {
	return fmt.Sprintf("%s {\n    daily\n    maxsize 20M\n    rotate 3\n    missingok\n    notifempty\n    compress\n    delaycompress\n    create 0600 root root\n    postrotate\n        '%s' -s reopen -c '%s'\n    endscript\n}\n", path, c.NginxBinary, c.NginxConfig)
}
