package nginxguard

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func directWithRealNginx(t *testing.T) *Direct {
	t.Helper()
	binary, err := exec.LookPath("nginx")
	if err != nil {
		t.Skip("nginx unavailable")
	}
	d := directFixture(t)
	quoted := "'" + strings.ReplaceAll(binary, "'", "'\\''") + "'"
	// Test configurations with real Nginx, while keeping the machine's running
	// Nginx instance untouched. Record reloads for idempotency assertions.
	script := "#!/bin/sh\nprintf '%s\\n' \"$1\" >> \"$0.calls\"\nif [ \"$1\" = '-s' ]; then exit 0; fi\nexec " + quoted + " \"$@\"\n"
	if err := os.WriteFile(d.engine.config.NginxBinary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestDirectAutomaticallyIncludesSite(t *testing.T) {
	for _, kind := range []string{"missing", "comment", "quoted-text", "exact", "wildcard", "relative", "nested", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			d := directWithRealNginx(t)
			c := d.engine.config
			dir := filepath.Dir(c.NginxConfig)
			reference, alreadyLoaded := "", true
			switch kind {
			case "missing":
				alreadyLoaded = false
			case "comment":
				alreadyLoaded = false
				reference = "# include " + c.ServerConfig + "; # GoCMS security\n"
			case "quoted-text":
				alreadyLoaded = false
				reference = "log_format sample '\n    include " + c.ServerConfig + "; # GoCMS security\n';\n"
			case "exact":
				reference = "include " + c.ServerConfig + ";\n"
			case "wildcard":
				reference = "include " + dir + "/site*.conf;\n"
			case "relative":
				reference = "include \"site.conf\";\n"
			case "nested":
				reference = "include \"nested.conf\";\n"
				if err := os.WriteFile(filepath.Join(dir, "nested.conf"), []byte("include 'site.conf';\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				reference = "include " + dir + "/alias.conf;\n"
				if err := os.Symlink(c.ServerConfig, filepath.Join(dir, "alias.conf")); err != nil {
					t.Fatal(err)
				}
			}
			// The added include must follow existing declarations needed by the
			// site, including custom log formats defined in the main config.
			main := []byte(fmt.Sprintf("pid %s/nginx.pid;\nerror_log %s/error.log;\nevents {}\nhttp {\nlog_format panel '$remote_addr';\n%s}\n", dir, dir, reference))
			site := []byte(fmt.Sprintf("server {\nlisten 8080;\naccess_log %s/access.log panel;\nlocation / { return 200 'ok'; }\n}\n", dir))
			for path, data := range map[string][]byte{c.NginxConfig: main, c.ServerConfig: site} {
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := d.Apply(exampleSnapshot()); err != nil {
				t.Fatal(err)
			}
			updatedMain, _ := os.ReadFile(c.NginxConfig)
			include := []byte("\n    include " + c.ServerConfig + "; # GoCMS security\n")
			backupPath := filepath.Join(filepath.Dir(d.engine.statePath), "nginx.conf.before")
			if alreadyLoaded {
				if !bytes.Equal(main, updatedMain) {
					t.Fatal("already-loaded site changed the main configuration")
				}
				if _, err := os.Lstat(backupPath); !os.IsNotExist(err) {
					t.Fatalf("unneeded main backup: %v", err)
				}
			} else {
				want := []byte(strings.TrimSuffix(string(main), "}\n") + string(include) + "}\n")
				if !bytes.Equal(updatedMain, want) {
					t.Fatalf("main configuration changed beyond the added include: %s", updatedMain)
				}
				backup, err := os.ReadFile(backupPath)
				if err != nil || !bytes.Equal(main, backup) {
					t.Fatalf("main configuration backup: %v", err)
				}
			}
			if _, err := d.Apply(exampleSnapshot()); err != nil {
				t.Fatal(err)
			}
			again, _ := os.ReadFile(c.NginxConfig)
			calls, _ := os.ReadFile(c.NginxBinary + ".calls")
			if !bytes.Equal(updatedMain, again) || bytes.Count(calls, []byte("-s\n")) != 1 {
				t.Fatal("repeated sync duplicated the include or reloaded unchanged configuration")
			}
			if !alreadyLoaded {
				// A later panel edit may remove the reference. Repair it even when
				// the rules themselves have not changed, preserving the first backup.
				if err := os.WriteFile(c.NginxConfig, main, 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := d.Apply(exampleSnapshot()); err != nil {
					t.Fatalf("repair removed include: %v", err)
				}
				repaired, _ := os.ReadFile(c.NginxConfig)
				calls, _ := os.ReadFile(c.NginxBinary + ".calls")
				backup, _ := os.ReadFile(backupPath)
				if !bytes.Equal(repaired, updatedMain) || bytes.Count(calls, []byte("-s\n")) != 2 || !bytes.Equal(backup, main) {
					t.Fatal("removed include not repaired correctly")
				}
			}
		})
	}
}

func TestDirectMissingIncludeFailureRestoresBothFiles(t *testing.T) {
	for _, stage := range []string{"test", "reload", "not-loaded"} {
		t.Run(stage, func(t *testing.T) {
			d := directFixture(t)
			c := d.engine.config
			main, _ := os.ReadFile(c.NginxConfig)
			main = bytes.Replace(main, []byte(" include "+c.ServerConfig+";\n"), nil, 1)
			if err := os.WriteFile(c.NginxConfig, main, 0600); err != nil {
				t.Fatal(err)
			}
			site, _ := os.ReadFile(c.ServerConfig)
			script, _ := os.ReadFile(c.NginxBinary)
			if stage == "not-loaded" {
				// A successful -T without the target file must not be acknowledged.
				broken := bytes.Replace(script, []byte("grep -F"), []byte("false && grep -F"), 1)
				if err := os.WriteFile(c.NginxBinary, broken, 0700); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(c.NginxBinary+".fail-"+stage, nil, 0600); err != nil {
				t.Fatal(err)
			}
			if result, err := d.Apply(exampleSnapshot()); err == nil || result.Digest != "" {
				t.Fatalf("failed sync acknowledged: %+v %v", result, err)
			}
			for path, original := range map[string][]byte{c.NginxConfig: main, c.ServerConfig: site} {
				data, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(data, original) {
					t.Fatalf("failed sync did not restore %s: %v", path, err)
				}
			}
			if stage == "not-loaded" {
				if err := os.WriteFile(c.NginxBinary, script, 0700); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Remove(c.NginxBinary + ".fail-" + stage); err != nil {
				t.Fatal(err)
			}
			if _, err := d.Apply(exampleSnapshot()); err != nil {
				t.Fatalf("retry after rollback: %v", err)
			}
		})
	}
}

func TestDirectConfigDumpFailureDoesNotExposeContents(t *testing.T) {
	d := directFixture(t)
	c := d.engine.config
	main, _ := os.ReadFile(c.NginxConfig)
	site, _ := os.ReadFile(c.ServerConfig)
	script := "#!/bin/sh\nprintf 'private-token-from-config\\n'\nprintf 'configuration invalid\\n' >&2\nexit 1\n"
	if err := os.WriteFile(c.NginxBinary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	_, err := d.Apply(exampleSnapshot())
	if err == nil || strings.Contains(err.Error(), "private-token") || !strings.Contains(err.Error(), "configuration invalid") {
		t.Fatalf("unsafe configuration diagnostic: %v", err)
	}
	for path, original := range map[string][]byte{c.NginxConfig: main, c.ServerConfig: site} {
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(data, original) {
			t.Fatalf("invalid baseline changed %s: %v", path, err)
		}
	}
}

func TestDirectRejectsMissingIncludeWithoutUniqueHTTPBlock(t *testing.T) {
	for _, main := range []string{"events {}\n", "events {}\nhttp {}\nhttp {}\n"} {
		t.Run(main, func(t *testing.T) {
			d := directFixture(t)
			c := d.engine.config
			if err := os.WriteFile(c.NginxConfig, []byte(main), 0600); err != nil {
				t.Fatal(err)
			}
			site, _ := os.ReadFile(c.ServerConfig)
			if _, err := d.Apply(exampleSnapshot()); err == nil {
				t.Fatal("ambiguous main configuration accepted")
			}
			for path, original := range map[string][]byte{c.NginxConfig: []byte(main), c.ServerConfig: site} {
				data, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(data, original) {
					t.Fatalf("ambiguous main configuration changed %s: %v", path, err)
				}
			}
		})
	}
}
