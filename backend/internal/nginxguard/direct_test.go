package nginxguard

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func directFixture(t *testing.T) *Direct {
	t.Helper()
	dir, err := os.MkdirTemp("", "gocms-direct-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	c := Config{SocketPath: filepath.Join(dir, "agent.sock"), NginxBinary: filepath.Join(dir, "nginx"), NginxConfig: filepath.Join(dir, "nginx.conf"), ServerConfig: filepath.Join(dir, "site.conf"), AgentUser: "www"}
	script := "#!/bin/sh\nprintf '%s\\n' \"$1\" >> \"$0.calls\"\nif [ -f \"$0.fail-test\" ] && [ -f \"$0.prepared\" ]; then exit 1; fi\ntouch \"$0.prepared\"\nif [ \"$1\" = '-s' ] && [ -f \"$0.fail-reload\" ]; then exit 1; fi\nexit 0\n"
	main := fmt.Sprintf("pid %s/nginx.pid;\nerror_log %s/error.log;\nevents {}\nhttp {\n include %s;\n}\n", dir, dir, c.ServerConfig)
	for path, text := range map[string]string{c.NginxConfig: main, c.ServerConfig: "server {\n listen 8080;\n # Existing panel and TLS settings stay in place.\n location / { return 200 'ok'; }\n}\n", c.NginxBinary: script} {
		if err := os.WriteFile(path, []byte(text), 0700); err != nil {
			t.Fatal(err)
		}
	}
	d := NewDirect(c)
	d.engine.httpPath = filepath.Join(dir, "state", "http.conf")
	d.engine.serverPath = filepath.Join(dir, "state", "server.conf")
	d.engine.statePath = filepath.Join(dir, "state", "snapshot.json")
	d.engine.logPath = filepath.Join(dir, "logs", "probes.log")
	d.rotation = ""
	return d
}

func TestDirectManageConfigAndRules(t *testing.T) {
	d := directFixture(t)
	c := d.engine.config
	originalMain, _ := os.ReadFile(c.NginxConfig)
	originalServer, _ := os.ReadFile(c.ServerConfig)
	s := exampleSnapshot()
	result, err := d.Apply(s)
	if err != nil || result.Digest != Digest(s) {
		t.Fatalf("apply: %+v %v", result, err)
	}
	for _, item := range []struct{ path, include string }{{c.NginxConfig, d.engine.httpPath}, {c.ServerConfig, d.engine.serverPath}} {
		text, err := os.ReadFile(item.path)
		if err != nil || strings.Count(string(text), "include "+item.include+";") != 1 {
			t.Fatalf("missing dedicated include: %s %v", text, err)
		}
	}
	for name, original := range map[string][]byte{"nginx.conf.before": originalMain, "server.conf.before": originalServer} {
		backup, err := os.ReadFile(filepath.Join(filepath.Dir(d.engine.statePath), name))
		if err != nil || !bytes.Equal(backup, original) {
			t.Fatalf("original backup %s: %v", name, err)
		}
	}
	server, _ := os.ReadFile(c.ServerConfig)
	if !bytes.Contains(server, []byte("# Existing panel and TLS settings stay in place.")) {
		t.Fatal("existing configuration lost")
	}
	before, _ := os.ReadFile(c.NginxBinary + ".calls")
	if _, err := d.Apply(s); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(c.NginxBinary + ".calls")
	if !bytes.Equal(before, after) {
		t.Fatal("unchanged direct sync reloaded Nginx")
	}
	if _, err := os.Lstat(c.SocketPath); !os.IsNotExist(err) {
		t.Fatalf("direct mode created an agent socket: %v", err)
	}
	if _, err := d.Apply(Snapshot{}); err != nil {
		t.Fatal(err)
	}
	http, _ := os.ReadFile(d.engine.httpPath)
	if bytes.Contains(http, []byte("203.0.113.")) {
		t.Fatal("disable did not clear Nginx rules")
	}
	line := `{"time":"2026-11-17T10:00:00+08:00","host":"alpha.example.test","ip":"203.0.113.1","path":"/server.key","status":404}` + "\n"
	if err := os.WriteFile(d.engine.logPath, []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	batch, err := d.Events("")
	if err != nil || len(batch.Items) != 1 {
		t.Fatalf("direct log collection: %+v %v", batch, err)
	}
}

func TestDirectInitialFailureRestoresPanelConfiguration(t *testing.T) {
	for _, stage := range []string{"test", "reload"} {
		t.Run(stage, func(t *testing.T) {
			d := directFixture(t)
			c := d.engine.config
			main, _ := os.ReadFile(c.NginxConfig)
			server, _ := os.ReadFile(c.ServerConfig)
			flag := c.NginxBinary + ".fail-" + stage
			if err := os.WriteFile(flag, nil, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := d.Apply(exampleSnapshot()); err == nil {
				t.Fatal("failed Nginx command accepted")
			}
			for path, original := range map[string][]byte{c.NginxConfig: main, c.ServerConfig: server} {
				text, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(text, original) {
					t.Fatalf("panel file not restored: %s %v", path, err)
				}
			}
			for _, path := range []string{d.engine.httpPath, d.engine.serverPath, d.engine.statePath} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("partial installation retained: %s %v", path, err)
				}
			}
			if err := os.Remove(flag); err != nil {
				t.Fatal(err)
			}
			if _, err := d.Apply(exampleSnapshot()); err != nil {
				t.Fatalf("retry failed: %v", err)
			}
		})
	}
}

func TestDirectRejectsConcurrentAgent(t *testing.T) {
	d := directFixture(t)
	listener, err := net.Listen("unix", d.engine.config.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if _, err := d.Apply(exampleSnapshot()); err == nil || !strings.Contains(err.Error(), "停止 gocms-security-agent") {
		t.Fatalf("active agent not rejected: %v", err)
	}
}

func TestDirectIncludesPassRealNginxValidation(t *testing.T) {
	binary, err := exec.LookPath("nginx")
	if err != nil {
		t.Skip("nginx unavailable")
	}
	d := directFixture(t)
	// Validate real configuration without reloading a machine-wide Nginx process.
	quoted := "'" + strings.ReplaceAll(binary, "'", "'\\''") + "'"
	script := "#!/bin/sh\nif [ \"$1\" = '-s' ]; then exit 0; fi\nexec " + quoted + " \"$@\"\n"
	if err := os.WriteFile(d.engine.config.NginxBinary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Apply(exampleSnapshot()); err != nil {
		t.Fatalf("direct include configuration rejected by Nginx: %v", err)
	}
}
