package nginxguard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func controlFixture(t *testing.T) (controlLayout, Config, string, string) {
	t.Helper()
	d := directFixture(t)
	dir, err := filepath.EvalSymlinks(filepath.Dir(d.engine.config.NginxConfig))
	if err != nil {
		t.Fatal(err)
	}
	c := d.engine.config
	c.NginxBinary, c.NginxConfig, c.ServerConfig = filepath.Join(dir, "nginx"), filepath.Join(dir, "nginx.conf"), filepath.Join(dir, "site.conf")
	main := fmt.Sprintf("events {}\nhttp { include %s; }\n", c.ServerConfig)
	if err := os.WriteFile(c.NginxConfig, []byte(main), 0600); err != nil {
		t.Fatal(err)
	}
	l := controlLayout{state: filepath.Join(dir, "state"), log: filepath.Join(dir, "logs", "probes.log"), binary: filepath.Join(dir, "libexec", "gocms-nginx"), sudoers: filepath.Join(dir, "sudoers.d", "gocms-nginx"), sudoersMain: filepath.Join(dir, "sudoers"), owner: os.Geteuid()}
	source := filepath.Join(dir, "source")
	for path, text := range map[string]string{source: "trusted executable snapshot", l.sudoersMain: "root ALL=(ALL) ALL\n"} {
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	visudo, err := exec.LookPath("visudo")
	if err != nil {
		t.Skip("visudo unavailable")
	}
	return l, c, source, visudo
}

func TestRootInitializationAndSingleFileControl(t *testing.T) {
	l, c, source, visudo := controlFixture(t)
	verify := func(installed installation) error {
		loaded, err := l.load()
		if err != nil {
			return err
		}
		if loaded != installed {
			return errors.New("installed policy differs")
		}
		response, err := l.execute(context.Background(), loaded, controlRequest{Protocol: controlProtocol, Action: "status", Config: c})
		if err != nil {
			return err
		}
		if response.Setup == nil || !response.Setup.Ready {
			return errors.New("status missing")
		}
		return nil
	}
	for i := 0; i < 2; i++ {
		if err := initializeWithLayout(context.Background(), l, c, exampleSnapshot(), source, visudo, 123, verify); err != nil {
			t.Fatal(err)
		}
	}
	rule, _ := os.ReadFile(l.sudoers)
	if !bytes.Contains(rule, []byte(l.binary+" "+controlArgument+"\n")) || bytes.Contains(rule, []byte(source)) || bytes.Contains(rule, []byte("*")) {
		t.Fatalf("sudo authorization is not restricted: %s", rule)
	}
	mainAuth, _ := os.ReadFile(l.sudoersMain)
	if strings.Count(string(mainAuth), "#includedir ") != 1 {
		t.Fatal("duplicated sudoers include")
	}
	backup, err := os.ReadFile(filepath.Join(l.state, "sudoers.before"))
	if err != nil || string(backup) != "root ALL=(ALL) ALL\n" {
		t.Fatalf("original sudoers backup: %s %v", backup, err)
	}
	if err := os.WriteFile(source, []byte("updated web executable"), 0600); err != nil {
		t.Fatal(err)
	}
	helper, _ := os.ReadFile(l.binary)
	if string(helper) != "trusted executable snapshot" {
		t.Fatal("web update changed privileged executable")
	}
	calls, _ := os.ReadFile(c.NginxBinary + ".calls")
	if bytes.Count(calls, []byte("-s\n")) != 1 {
		t.Fatal("repeated initialization reloaded unchanged Nginx")
	}
	installed, err := l.load()
	if err != nil {
		t.Fatal(err)
	}
	line := `{"time":"2026-11-17T10:00:00+08:00","host":"alpha.example.test","ip":"203.0.113.1","path":"/server.key","status":404}` + "\n"
	if err := os.WriteFile(l.log, []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	response, err := l.execute(context.Background(), installed, controlRequest{Protocol: controlProtocol, Action: "events", Config: c})
	if err != nil || response.Batch == nil || len(response.Batch.Items) != 1 {
		t.Fatalf("privileged log collection: %+v %v", response, err)
	}
	empty := Snapshot{Tenants: []Tenant{}, Rules: []Rule{}}
	response, err = l.execute(context.Background(), installed, controlRequest{Protocol: controlProtocol, Action: "apply", Config: c, Snapshot: &empty})
	if err != nil || response.Result == nil || response.Result.Digest != Digest(empty) {
		t.Fatalf("cleanup acknowledgement: %+v %v", response, err)
	}
	site, _ := os.ReadFile(c.ServerConfig)
	if bytes.Contains(site, []byte("203.0.113.")) {
		t.Fatal("disabled rules remained")
	}
}

func TestRootInitializationRollsBackAuthorizationAndNginx(t *testing.T) {
	for _, stage := range []string{"sudoers", "authorization", "test", "reload"} {
		t.Run(stage, func(t *testing.T) {
			l, c, source, visudo := controlFixture(t)
			main, _ := os.ReadFile(c.NginxConfig)
			site, _ := os.ReadFile(c.ServerConfig)
			auth, _ := os.ReadFile(l.sudoersMain)
			if stage == "sudoers" {
				visudo = filepath.Join(filepath.Dir(source), "bad-visudo")
				if err := os.WriteFile(visudo, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if stage == "test" || stage == "reload" {
				if err := os.WriteFile(c.NginxBinary+".fail-"+stage, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			verify := func(installation) error {
				if stage == "authorization" {
					return errors.New("sudo denied")
				}
				return nil
			}
			if err := initializeWithLayout(context.Background(), l, c, exampleSnapshot(), source, visudo, 123, verify); err == nil {
				t.Fatal("failed initialization accepted")
			}
			for path, original := range map[string][]byte{c.NginxConfig: main, c.ServerConfig: site, l.sudoersMain: auth} {
				data, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(data, original) {
					t.Fatalf("not restored: %s %v", path, err)
				}
			}
			for _, path := range []string{l.binary, l.sudoers, filepath.Join(l.state, "control.json")} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("partial authorization retained: %s %v", path, err)
				}
			}
		})
	}
}

func TestControlRejectsUntrustedRequestsAndPaths(t *testing.T) {
	l, c, source, visudo := controlFixture(t)
	if err := initializeWithLayout(context.Background(), l, c, exampleSnapshot(), source, visudo, 123, func(installation) error { return nil }); err != nil {
		t.Fatal(err)
	}
	installed, err := l.load()
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(c.NginxBinary + ".calls")
	snapshot := exampleSnapshot()
	snapshot.Tenants[0].Hosts = []string{"example.test;include /etc/passwd"}
	other := c
	other.NginxBinary = "/bin/sh"
	for _, request := range []controlRequest{
		{Protocol: controlProtocol, Action: "shell", Config: c},
		{Protocol: controlProtocol, Action: "apply", Config: other, Snapshot: &snapshot},
		{Protocol: controlProtocol, Action: "apply", Config: c, Snapshot: &snapshot},
		{Protocol: controlProtocol, Action: "apply", Config: c},
		{Protocol: controlProtocol, Action: "events", Config: c, Cursor: strings.Repeat("x", 129)},
		{Protocol: controlProtocol + 1, Action: "status", Config: c},
	} {
		if _, err := l.execute(context.Background(), installed, request); err == nil {
			t.Fatalf("unsafe request accepted: %+v", request)
		}
	}
	if err := os.Chmod(c.ServerConfig, 0666); err != nil {
		t.Fatal(err)
	}
	valid := exampleSnapshot()
	if _, err := l.execute(context.Background(), installed, controlRequest{Protocol: controlProtocol, Action: "apply", Config: c, Snapshot: &valid}); err == nil {
		t.Fatal("web-writable config accepted")
	}
	after, _ := os.ReadFile(c.NginxBinary + ".calls")
	if !bytes.Equal(before, after) {
		t.Fatal("rejected input ran Nginx")
	}
	for _, raw := range []string{`{"action":"status","command":"id"}`, `{} {}`, strings.Repeat(" ", maxControlInput+1)} {
		var request controlRequest
		if decodeControl(strings.NewReader(raw), &request) == nil {
			t.Fatal("invalid protocol accepted")
		}
	}
	alias := filepath.Join(filepath.Dir(source), "alias")
	if err := os.Symlink(l.binary, alias); err != nil {
		t.Fatal(err)
	}
	if checkTrustedPath(alias, false, l.owner) == nil {
		t.Fatal("symlink helper accepted")
	}
}

func TestControlLockWaitHonorsDeadline(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "control.lock")
	unlock, err := lockControl(context.Background(), path, os.Geteuid())
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if release, err := lockControl(ctx, path, os.Geteuid()); err == nil {
		release()
		t.Fatal("concurrent lock ignored")
	}
}

func TestCappedControlOutput(t *testing.T) {
	b := cappedOutput{limit: 3}
	n, err := b.Write([]byte("abcdef"))
	if err != nil || n != 6 || b.String() != "abc" || !b.exceeded {
		t.Fatal("output cap failed")
	}
}

func TestControlInputHasDeadline(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := readControlRequest(ctx, r); err == nil {
		t.Fatal("unfinished input did not time out")
	}
}

func TestRootReinitializationFailurePreservesWorkingInstallation(t *testing.T) {
	l, c, source, visudo := controlFixture(t)
	verify := func(installation) error { return nil }
	if err := initializeWithLayout(context.Background(), l, c, exampleSnapshot(), source, visudo, 123, verify); err != nil {
		t.Fatal(err)
	}
	previous := map[string][]byte{}
	for _, path := range []string{l.binary, l.sudoers, l.sudoersMain, filepath.Join(l.state, "control.json"), c.NginxConfig, c.ServerConfig} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		previous[path] = data
	}
	if err := os.WriteFile(source, []byte("updated program"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.NginxBinary+".fail-reload", nil, 0600); err != nil {
		t.Fatal(err)
	}
	snapshot := exampleSnapshot()
	snapshot.Rules = append(snapshot.Rules, Rule{SiteID: 0, Network: "192.0.2.1/32", Kind: "deny"})
	if err := initializeWithLayout(context.Background(), l, c, snapshot, source, visudo, 123, verify); err == nil {
		t.Fatal("failed update accepted")
	}
	for path, before := range previous {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("working installation changed: %s %v", path, err)
		}
	}
}
