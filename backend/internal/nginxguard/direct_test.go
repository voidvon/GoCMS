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
	"time"
)

func directFixture(t *testing.T) *Direct {
	t.Helper()
	dir, err := os.MkdirTemp("", "gocms-direct-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	c := Config{SocketPath: filepath.Join(dir, "agent.sock"), NginxBinary: filepath.Join(dir, "nginx"), NginxConfig: filepath.Join(dir, "nginx.conf"), ServerConfig: filepath.Join(dir, "site.conf"), AgentUser: "www"}
	script := "#!/bin/sh\nprintf '%s\\n' \"$1\" >> \"$0.calls\"\nif [ -f \"$0.fail-test\" ] && [ -f \"$0.prepared\" ]; then exit 1; fi\ntouch \"$0.prepared\"\nif [ \"$1\" = '-s' ] && [ -f \"$0.fail-reload\" ]; then exit 1; fi\nif [ \"$1\" = '-T' ]; then\n  printf '# configuration file %s:\\n' \"$3\"\n  site=\"$(dirname \"$0\")/site.conf\"\n  if grep -F \"include $site;\" \"$3\" > /dev/null; then printf '# configuration file %s:\\n' \"$site\"; fi\nfi\nexit 0\n"
	main := fmt.Sprintf("pid %s/nginx.pid;\nerror_log %s/error.log;\nevents {}\nhttp {\n include %s;\n}\n", dir, dir, c.ServerConfig)
	for path, text := range map[string]string{c.NginxConfig: main, c.ServerConfig: "server {\n listen 8080;\n # Existing panel and TLS settings stay in place.\n location / { return 200 'ok'; }\n}\n", c.NginxBinary: script} {
		if err := os.WriteFile(path, []byte(text), 0700); err != nil {
			t.Fatal(err)
		}
	}
	d := NewDirect(c)
	d.engine.statePath = filepath.Join(dir, "state", "snapshot.json")
	d.engine.logPath = filepath.Join(dir, "logs", "probes.log")
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
	main, err := os.ReadFile(c.NginxConfig)
	if err != nil || !bytes.Equal(main, originalMain) {
		t.Fatalf("main configuration changed: %s %v", main, err)
	}
	backup, err := os.ReadFile(filepath.Join(filepath.Dir(d.engine.statePath), "server.conf.before"))
	if err != nil || !bytes.Equal(backup, originalServer) {
		t.Fatalf("original backup: %v", err)
	}
	files, err := os.ReadDir(filepath.Dir(d.engine.statePath))
	if err != nil || len(files) != 1 || files[0].Name() != "server.conf.before" {
		t.Fatalf("unexpected extra configuration or state file: %+v %v", files, err)
	}
	server, _ := os.ReadFile(c.ServerConfig)
	for _, marker := range []string{httpBegin, httpEnd, serverBegin, serverEnd} {
		if strings.Count(string(server), marker) != 1 {
			t.Fatalf("missing single-file managed block: %s", marker)
		}
	}
	if !bytes.Contains(server, []byte("# Existing panel and TLS settings stay in place.")) {
		t.Fatal("existing configuration lost")
	}
	before, _ := os.ReadFile(c.NginxBinary + ".calls")
	if _, err := d.Apply(s); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(c.NginxBinary + ".calls")
	if bytes.Count(before, []byte("-s\n")) != bytes.Count(after, []byte("-s\n")) {
		t.Fatal("unchanged direct sync reloaded Nginx")
	}
	// Panel changes outside the managed blocks survive subsequent rule updates.
	server = bytes.ReplaceAll(server, []byte("# Existing panel and TLS settings stay in place."), []byte("# Changed in the panel after synchronization."))
	if err := os.WriteFile(c.ServerConfig, server, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(c.SocketPath); !os.IsNotExist(err) {
		t.Fatalf("direct mode created an agent socket: %v", err)
	}
	if _, err := d.Apply(Snapshot{}); err != nil {
		t.Fatal(err)
	}
	cleared, _ := os.ReadFile(c.ServerConfig)
	if bytes.Contains(cleared, []byte("203.0.113.")) || !bytes.Contains(cleared, []byte("# Changed in the panel after synchronization.")) {
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
			for _, path := range []string{filepath.Join(filepath.Dir(d.engine.statePath), "http.conf"), filepath.Join(filepath.Dir(d.engine.statePath), "server.conf"), d.engine.statePath} {
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

func TestDirectSingleFilePassesRealNginxValidation(t *testing.T) {
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
		t.Fatalf("single site configuration rejected by Nginx: %v", err)
	}
}

func TestDirectMigratesLegacySiteIncludeWithoutChangingMain(t *testing.T) {
	binary, err := exec.LookPath("nginx")
	if err != nil {
		t.Skip("nginx unavailable")
	}
	d := directFixture(t)
	c := d.engine.config
	// Load the old http declarations alongside the new file to detect variable
	// or log-format collisions left by v0.1.61's main-config include.
	h, _, err := Render(exampleSnapshot(), time.Now(), d.engine.logPath)
	if err != nil {
		t.Fatal(err)
	}
	main, _ := os.ReadFile(c.NginxConfig)
	main = bytes.Replace(main, []byte("http {"), []byte("http {\n"+h), 1)
	if err := os.WriteFile(c.NginxConfig, main, 0400); err != nil {
		t.Fatal(err)
	}
	site, _ := os.ReadFile(c.ServerConfig)
	site, err = IncludeInBlock(site, "server", serverFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.ServerConfig, site, 0600); err != nil {
		t.Fatal(err)
	}
	// Skip only the pre-change check; the generated result uses real nginx -t.
	quoted := "'" + strings.ReplaceAll(binary, "'", "'\\''") + "'"
	script := "#!/bin/sh\nif [ \"$1\" = '-s' ]; then exit 0; fi\nif [ ! -f \"$0.initial\" ]; then touch \"$0.initial\"; printf '# configuration file %s:\\n# configuration file %s:\\n' \"$3\" \"$(dirname \"$0\")/site.conf\"; exit 0; fi\nexec " + quoted + " \"$@\"\n"
	if err := os.WriteFile(c.NginxBinary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Apply(exampleSnapshot()); err != nil {
		t.Fatal(err)
	}
	updatedMain, _ := os.ReadFile(c.NginxConfig)
	if !bytes.Equal(main, updatedMain) {
		t.Fatal("legacy main config was changed")
	}
	updatedSite, _ := os.ReadFile(c.ServerConfig)
	if bytes.Contains(updatedSite, []byte("include "+serverFile)) {
		t.Fatal("legacy site include still active")
	}
}

func TestSingleFileRejectsIncompleteMarkersAndAmbiguousServers(t *testing.T) {
	for _, text := range []string{httpBegin + "server {}", "server {}" + serverEnd, "server {} server {}"} {
		if _, err := RenderSingleFile([]byte(text), exampleSnapshot(), time.Now(), "/tmp/probes.log"); err == nil {
			t.Fatalf("unsafe site file accepted: %q", text)
		}
	}
}

func TestDirectBoundsProbeLogWithoutLogrotateConfiguration(t *testing.T) {
	d := directFixture(t)
	if err := os.MkdirAll(filepath.Dir(d.engine.logPath), 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(d.engine.logPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(20 << 20); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	batch, err := d.Events("")
	if err != nil || batch.Cursor != "" {
		t.Fatalf("log trim: %+v %v", batch, err)
	}
	info, err := os.Stat(d.engine.logPath)
	if err != nil || info.Size() != 0 {
		t.Fatalf("log unbounded: %v %v", info, err)
	}
}
