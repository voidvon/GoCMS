package nginxguard

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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

func TestLogCursorAndRotation(t *testing.T) {
	dir := t.TempDir()
	a := &engine{logPath: filepath.Join(dir, "probes.log")}
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
	restarted := &engine{logPath: a.logPath}
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

func TestRejectConfigurationInjection(t *testing.T) {
	s := exampleSnapshot()
	s.Tenants[0].Hosts = []string{"example.test;include /etc/passwd"}
	if Validate(s) == nil {
		t.Fatal("nginx configuration injection accepted")
	}
}
