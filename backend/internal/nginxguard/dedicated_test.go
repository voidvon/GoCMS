package nginxguard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func dedicatedFixture(t *testing.T, reference string) (controlLayout, Config, string, string, string) {
	t.Helper()
	l, c, source, visudo := controlFixture(t)
	realNginx, err := exec.LookPath("nginx")
	if err != nil {
		t.Skip("nginx unavailable")
	}
	dir := filepath.Dir(c.NginxConfig)
	vhosts := filepath.Join(dir, "vhosts")
	if err := os.Mkdir(vhosts, 0700); err != nil {
		t.Fatal(err)
	}
	c.ServerConfig = filepath.Join(vhosts, "gocms.conf")
	site := filepath.Join(vhosts, "alpha.conf")
	// Multiple HTTP/HTTPS-style servers in one file, plus an unrelated server.
	data := "# Panel-owned TLS and proxy configuration\nserver {\nlisten 18081;\nserver_name alpha.example.test;\nlocation / { return 200 'alpha'; }\n}\nserver {\nlisten 18082;\nserver_name beta.example.test;\nlocation / { return 200 'beta'; }\n}\nserver {\nlisten 18083;\nserver_name unrelated.example.test;\nlocation / { return 200 'unrelated'; }\n}\n"
	if err := os.WriteFile(site, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	include := "include " + site + ";"
	if reference == "wildcard" {
		include = "include " + vhosts + "/*.conf;"
	} else if reference == "exact" {
		include += "\ninclude " + c.ServerConfig + ";"
	}
	main := fmt.Sprintf("pid %s/nginx.pid;\nerror_log %s/error.log;\nevents {}\nhttp {\n%s\n}\n", dir, dir, include)
	if err := os.WriteFile(c.NginxConfig, []byte(main), 0600); err != nil {
		t.Fatal(err)
	}
	quoted := "'" + strings.ReplaceAll(realNginx, "'", "'\\''") + "'"
	script := "#!/bin/sh\nprintf '%s\\n' \"$1\" >> \"$0.calls\"\nif [ \"$1\" = '-s' ]; then\n if [ -f \"$0.fail-reload\" ]; then exit 1; fi\n exit 0\nfi\nif [ -f \"$0.fail-test\" ] && grep -q GOCMS-SECURITY-SERVER-BEGIN \"$(dirname \"$0\")/vhosts/alpha.conf\"; then exit 1; fi\nexec " + quoted + " \"$@\"\n"
	if err := os.WriteFile(c.NginxBinary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return l, c, source, visudo, site
}

func TestRootSetupCreatesDedicatedRulesAndHooks(t *testing.T) {
	for _, reference := range []string{"missing", "wildcard", "exact"} {
		t.Run(reference, func(t *testing.T) {
			l, c, source, visudo, site := dedicatedFixture(t, reference)
			original, _ := os.ReadFile(site)
			verify := func(installation) error { return nil }
			for i := 0; i < 2; i++ {
				if err := initializeWithLayout(context.Background(), l, c, exampleSnapshot(), source, visudo, 123, verify); err != nil {
					t.Fatal(err)
				}
			}
			rules, err := os.ReadFile(c.ServerConfig)
			if err != nil || !bytes.HasPrefix(rules, []byte(dedicatedHeader)) || !bytes.Contains(rules, []byte("203.0.113.")) {
				t.Fatalf("dedicated rules not generated: %s %v", rules, err)
			}
			if bytes.Contains(rules, []byte("listen ")) || bytes.Contains(rules, []byte("proxy_pass ")) {
				t.Fatal("setup invented a new virtual host")
			}
			updated, _ := os.ReadFile(site)
			if strings.Count(string(updated), serverBegin) != 2 || !bytes.Contains(updated, []byte("# Panel-owned TLS and proxy configuration")) {
				t.Fatal("matching servers not hooked or panel settings lost")
			}
			_, hook, _, _ := dedicatedFragments(exampleSnapshot(), l.log)
			if !bytes.Equal(bytes.ReplaceAll(updated, []byte(hook), nil), original) {
				t.Fatal("setup changed existing configuration beyond fixed hooks")
			}
			main, _ := os.ReadFile(c.NginxConfig)
			if strings.Count(string(main), logBegin) != 1 {
				t.Fatal("log declaration duplicated")
			}
			calls, _ := os.ReadFile(c.NginxBinary + ".calls")
			if bytes.Count(calls, []byte("-s\n")) != 1 {
				t.Fatal("repeated setup reloaded unchanged configuration")
			}
			installed, err := l.load()
			if err != nil || len(installed.Targets) != 1 || installed.Targets[0].Path != site {
				t.Fatalf("discovered target not pinned: %+v %v", installed.Targets, err)
			}
			empty := Snapshot{}
			if _, err := l.execute(context.Background(), installed, controlRequest{Protocol: controlProtocol, Action: "apply", Config: c, Snapshot: &empty}); err != nil {
				t.Fatal(err)
			}
			rules, _ = os.ReadFile(c.ServerConfig)
			if bytes.Contains(rules, []byte("203.0.113.")) {
				t.Fatal("disabled rules remained")
			}
			// Ordinary web synchronization must repair panel-removed hooks.
			if err := os.WriteFile(site, original, 0600); err != nil {
				t.Fatal(err)
			}
			snapshot := exampleSnapshot()
			if _, err := l.execute(context.Background(), installed, controlRequest{Protocol: controlProtocol, Action: "apply", Config: c, Snapshot: &snapshot}); err != nil {
				t.Fatal(err)
			}
			updated, _ = os.ReadFile(site)
			if strings.Count(string(updated), serverBegin) != 2 {
				t.Fatal("panel-removed hooks not repaired")
			}
			// Removing a loaded rule include or log format makes the baseline
			// invalid. Sync must repair it without duplicating a wildcard include.
			main, _ = os.ReadFile(c.NginxConfig)
			withoutLog, err := removeManagedBlock(string(main), logBegin, logEnd)
			if err != nil {
				t.Fatal(err)
			}
			if reference == "missing" {
				withoutLog = strings.ReplaceAll(withoutLog, "    include "+c.ServerConfig+"; # GoCMS security\n", "")
			}
			if err := os.WriteFile(c.NginxConfig, []byte(withoutLog), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := l.execute(context.Background(), installed, controlRequest{Protocol: controlProtocol, Action: "apply", Config: c, Snapshot: &snapshot}); err != nil {
				t.Fatalf("cannot repair invalid baseline: %v", err)
			}
		})
	}
}

func TestDedicatedSyncRejectsUnloadedAuthorizedSite(t *testing.T) {
	l, c, source, visudo, site := dedicatedFixture(t, "missing")
	if err := initializeWithLayout(context.Background(), l, c, exampleSnapshot(), source, visudo, 123, func(installation) error { return nil }); err != nil {
		t.Fatal(err)
	}
	installed, err := l.load()
	if err != nil {
		t.Fatal(err)
	}
	main, _ := os.ReadFile(c.NginxConfig)
	main = bytes.ReplaceAll(main, []byte("include "+site+";"), nil)
	if err := os.WriteFile(c.NginxConfig, main, 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(c.ServerConfig)
	snapshot := exampleSnapshot()
	snapshot.Rules = append(snapshot.Rules, Rule{SiteID: 0, Network: "192.0.2.1/32", Kind: "deny"})
	if _, err := l.execute(context.Background(), installed, controlRequest{Protocol: controlProtocol, Action: "apply", Config: c, Snapshot: &snapshot}); err == nil || !strings.Contains(err.Error(), "未加载已授权站点") {
		t.Fatalf("unloaded enforcement acknowledged: %v", err)
	}
	after, _ := os.ReadFile(c.ServerConfig)
	if !bytes.Equal(before, after) {
		t.Fatal("unloaded-site failure did not restore rules")
	}
}

func TestRootDedicatedFailureRestoresMissingFileAndAuthorization(t *testing.T) {
	for _, stage := range []string{"authorization", "test", "reload", "unmatched"} {
		t.Run(stage, func(t *testing.T) {
			l, c, source, visudo, site := dedicatedFixture(t, "wildcard")
			previous := map[string][]byte{}
			for _, path := range []string{c.NginxConfig, site, l.sudoersMain} {
				previous[path], _ = os.ReadFile(path)
			}
			if stage == "test" || stage == "reload" {
				if err := os.WriteFile(c.NginxBinary+".fail-"+stage, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			verify := func(installation) error {
				if stage == "authorization" {
					return errors.New("denied")
				}
				return nil
			}
			snapshot := exampleSnapshot()
			if stage == "unmatched" {
				snapshot.Tenants[0].Hosts = []string{"missing.example.test"}
			}
			if err := initializeWithLayout(context.Background(), l, c, snapshot, source, visudo, 123, verify); err == nil {
				t.Fatal("failed setup accepted")
			}
			for path, want := range previous {
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(want, got) {
					t.Fatalf("configuration not restored: %s %v", path, err)
				}
			}
			for _, path := range []string{c.ServerConfig, l.binary, l.sudoers, filepath.Join(l.state, "control.json")} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("partial setup retained: %s %v", path, err)
				}
			}
		})
	}
}

func TestVisudoLookupWithoutSbinInPATHAndInstallationHelp(t *testing.T) {
	path, err := findVisudo(func(candidate string) (string, error) {
		if candidate == "/usr/sbin/visudo" {
			return candidate, nil
		}
		return "", exec.ErrNotFound
	})
	if err != nil || path != "/usr/sbin/visudo" {
		t.Fatal("standard installation requires sbin in PATH")
	}
	_, err = findVisudo(func(string) (string, error) { return "", exec.ErrNotFound })
	for _, text := range []string{"visudo 随 sudo", "root", "apt install -y sudo", "dnf install -y sudo", "yum install -y sudo", "nginx setup", "-db"} {
		if err == nil || !strings.Contains(err.Error(), text) {
			t.Fatalf("missing installation step %q: %v", text, err)
		}
	}
}
