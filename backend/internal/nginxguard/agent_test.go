package nginxguard

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func exampleSnapshot() Snapshot {
	return Snapshot{Tenants: []Tenant{{ID: 7, Hosts: []string{"alpha.example.test"}, Paths: []string{"/server.key", "/.git/*"}}, {ID: 91, Hosts: []string{"beta.example.test"}}}, Rules: []Rule{{SiteID: 7, Network: "203.0.113.0/24", Kind: "deny"}, {SiteID: 7, Network: "203.0.113.8/32", Kind: "allow"}, {SiteID: 0, Network: "203.0.113.8/32", Kind: "deny"}, {SiteID: 0, Network: "203.0.113.9/32", Kind: "allow"}}}
}

func TestNginxTenantRulesWithRealServer(t *testing.T) {
	testNginxTenantRulesWithRealServer(t, false)
}

func TestNginxSingleFileTenantRulesWithRealServer(t *testing.T) {
	testNginxTenantRulesWithRealServer(t, true)
}

func testNginxTenantRulesWithRealServer(t *testing.T, single bool) {
	binary, err := exec.LookPath("nginx")
	if err != nil {
		t.Skip("nginx unavailable")
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "probes.log")
	h, s, err := Render(exampleSnapshot(), time.Now(), logPath)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	config := fmt.Sprintf("daemon off; master_process off; pid %s/nginx.pid; error_log %s/error.log; events {} http { %s server { listen 127.0.0.1:%d; set_real_ip_from 127.0.0.1; real_ip_header X-Test-IP; %s location / { return 200 'ok'; } } }", dir, dir, h, port, s)
	if single {
		site := fmt.Sprintf("server { listen 127.0.0.1:%d; set_real_ip_from 127.0.0.1; real_ip_header X-Test-IP; location / { return 200 'ok'; } }", port)
		data, err := RenderSingleFile([]byte(site), exampleSnapshot(), time.Now(), logPath)
		if err != nil {
			t.Fatal(err)
		}
		config = fmt.Sprintf("daemon off; master_process off; pid %s/nginx.pid; error_log %s/error.log; events {} http { %s }", dir, dir, data)
	}
	filename := filepath.Join(dir, "nginx.conf")
	if err := os.WriteFile(filename, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(binary, "-t", "-c", filename, "-p", dir).CombinedOutput(); err != nil {
		t.Fatalf("generated Nginx configuration rejected: %s %v", out, err)
	}
	cmd := exec.Command(binary, "-c", filename, "-p", dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})
	client := &http.Client{Timeout: time.Second}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(3 * time.Second)
	for {
		res, err := client.Get(endpoint)
		if err == nil {
			res.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("nginx did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, tc := range []struct {
		host, ip string
		want     int
	}{{"alpha.example.test", "203.0.113.10", 403}, {"beta.example.test", "203.0.113.10", 200}, {"alpha.example.test", "203.0.113.8", 403}, {"beta.example.test", "203.0.113.8", 403}, {"alpha.example.test", "203.0.113.9", 200}, {"alpha.example.test", "198.51.100.5", 200}} {
		req, _ := http.NewRequest("GET", endpoint+"/ordinary", nil)
		req.Host = tc.host
		req.Header.Set("X-Test-IP", tc.ip)
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != tc.want {
			t.Fatalf("%s %s: %d want %d", tc.host, tc.ip, res.StatusCode, tc.want)
		}
	}
}

func TestAgentRollbackExpirationAndNoRedundantReload(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "nginx")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' \"$1\" >> \"$0.calls\"\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	a := &agent{config: Config{NginxBinary: binary, NginxConfig: filepath.Join(dir, "nginx.conf")}, httpPath: filepath.Join(dir, "http.conf"), serverPath: filepath.Join(dir, "server.conf"), statePath: filepath.Join(dir, "state.json"), logPath: filepath.Join(dir, "probes.log")}
	for _, p := range []string{a.httpPath, a.serverPath} {
		if err := os.WriteFile(p, []byte("previous"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s := exampleSnapshot()
	if err := a.apply(s); err == nil {
		t.Fatal("failed nginx validation was accepted")
	}
	for _, p := range []string{a.httpPath, a.serverPath} {
		b, _ := os.ReadFile(p)
		if string(b) != "previous" {
			t.Fatal("old configuration was not restored")
		}
	}
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' \"$1\" >> \"$0.calls\"\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := a.apply(s); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(binary + ".calls")
	if err := a.apply(s); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(binary + ".calls")
	if string(before) != string(after) {
		t.Fatal("unchanged configuration caused reload")
	}
	if a.result.Digest != Digest(s) {
		t.Fatal("wrong applied snapshot")
	}
	s.Rules[0].ExpiresAt = time.Now().Add(-time.Second).UTC().Format(time.RFC3339)
	if err := a.apply(s); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(a.httpPath)
	if strings.Contains(string(b), "203.0.113.0/24") {
		t.Fatal("expired IP retained in nginx")
	}
	// A reload failure also restores both files and keeps the last acknowledgement.
	old, _ := os.ReadFile(a.httpPath)
	oldDigest := a.result.Digest
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nif [ \"$1\" = '-s' ]; then exit 1; fi\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	s.Rules = append(s.Rules, Rule{SiteID: 91, Kind: "deny", Network: "192.0.2.5/32"})
	if err := a.apply(s); err == nil {
		t.Fatal("reload failure accepted")
	}
	b, _ = os.ReadFile(a.httpPath)
	if string(old) != string(b) || a.result.Digest != oldDigest {
		t.Fatal("reload failure changed acknowledged state")
	}
}

func TestAgentLogCursorAndRotation(t *testing.T) {
	dir := t.TempDir()
	a := &agent{logPath: filepath.Join(dir, "probes.log")}
	line := `{"time":"2026-11-17T10:00:00+08:00","host":"alpha.example.test","ip":"203.0.113.1","path":"/server.key","status":404}` + "\n"
	if err := os.WriteFile(a.logPath, []byte(line+`{"time":`), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := a.events("")
	if err != nil || len(b.Items) != 1 {
		t.Fatalf("read: %+v %v", b, err)
	}
	next, err := a.events(b.Cursor)
	if err != nil || len(next.Items) != 0 || next.Cursor != b.Cursor {
		t.Fatalf("partial line advanced cursor: %+v %v", next, err)
	}
	restarted := &agent{logPath: a.logPath}
	next, err = restarted.events(b.Cursor)
	if err != nil || len(next.Items) != 0 {
		t.Fatalf("restart replayed acknowledged probes: %+v %v", next, err)
	}
	if err := os.Rename(a.logPath, a.logPath+".1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.logPath, []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	next, err = a.events(b.Cursor)
	if err != nil || len(next.Items) != 1 {
		t.Fatalf("rotation lost events: %+v %v", next, err)
	}
}

func TestInstallIncludePreservesPanelConfiguration(t *testing.T) {
	data := []byte("# http { ignored\nhttp\n{\n log_format x 'text { }';\n server { listen 80; }\n}\n")
	updated, err := IncludeInBlock(data, "http", "/etc/gocms-security/http.conf")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "log_format x 'text { }';") || !strings.Contains(string(updated), "# GoCMS security") {
		t.Fatal("panel configuration changed")
	}
	again, err := IncludeInBlock(updated, "http", "/etc/gocms-security/http.conf")
	if err != nil || string(again) != string(updated) {
		t.Fatal("installation is not idempotent")
	}
	if _, err := IncludeInBlock([]byte("server {} server {}"), "server", "/x"); err == nil {
		t.Fatal("ambiguous server selection accepted")
	}
	s := exampleSnapshot()
	s.Tenants[0].Hosts = []string{"example.test;include /etc/passwd"}
	if Validate(s) == nil {
		t.Fatal("nginx configuration injection accepted")
	}
}
