package nginxguard

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestControlReadsOnlyPinnedConfigurationWithoutChangingNginx(t *testing.T) {
	l, c, source, visudo := controlFixture(t)
	if err := initializeWithLayout(context.Background(), l, c, exampleSnapshot(), source, visudo, 123, func(installation) error { return nil }); err != nil {
		t.Fatal(err)
	}
	installed, err := l.load()
	if err != nil {
		t.Fatal(err)
	}
	want := "# Custom administrator comment\n" + strings.Repeat("# 完整内容 <script> & \"test\"\n", 1000) + "# End of actual file\n"
	if err := os.WriteFile(c.ServerConfig, []byte(want), 0600); err != nil {
		t.Fatal(err)
	}
	calls, _ := os.ReadFile(c.NginxBinary + ".calls")
	request := controlRequest{Protocol: controlProtocol, Action: "config", Config: c}
	response, err := l.execute(context.Background(), installed, request)
	if err != nil || response.File == nil || response.File.Path != c.ServerConfig || response.File.Content != want || response.File.ModifiedAt == "" {
		t.Fatalf("actual file not returned in full: %v", err)
	}
	for _, invalid := range []controlRequest{
		{Protocol: controlProtocol, Action: "config", Config: Config{NginxBinary: c.NginxBinary, NginxConfig: c.NginxConfig, ServerConfig: l.sudoersMain, RunUser: c.RunUser}},
		{Protocol: controlProtocol, Action: "config", Config: c, Cursor: "0"},
		{Protocol: controlProtocol, Action: "config", Config: c, Snapshot: &Snapshot{}},
	} {
		response, err := l.execute(context.Background(), installed, invalid)
		if err == nil || response.File != nil {
			t.Fatal("configuration read accepted arbitrary path or extra arguments")
		}
	}
	after, _ := os.ReadFile(c.NginxBinary + ".calls")
	if !bytes.Equal(calls, after) {
		t.Fatal("viewing configuration tested or reloaded Nginx")
	}
	actual, _ := os.ReadFile(c.ServerConfig)
	if string(actual) != want {
		t.Fatal("viewing configuration changed its contents")
	}
	if err := os.Remove(c.ServerConfig); err != nil {
		t.Fatal(err)
	}
	if response, err := l.execute(context.Background(), installed, request); err == nil || response.File != nil || !strings.Contains(err.Error(), "nginx setup") {
		t.Fatalf("missing file lacked setup instructions: %v", err)
	}
	if err := os.Symlink(l.sudoersMain, c.ServerConfig); err != nil {
		t.Fatal(err)
	}
	if response, err := l.execute(context.Background(), installed, request); err == nil || response.File != nil {
		t.Fatal("configuration read followed a symbolic link")
	}
}

func TestConfigurationReadNeverReturnsPartialOrInvalidText(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "gocms.conf")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxConfigBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if file, err := readConfigFile(path, os.Geteuid()); err == nil || file.Content != "" || !strings.Contains(err.Error(), "8 MiB") {
		t.Fatalf("oversize file silently truncated: %v", err)
	}
	if err := os.WriteFile(path, []byte{0xff}, 0600); err != nil {
		t.Fatal(err)
	}
	if file, err := readConfigFile(path, os.Geteuid()); err == nil || file.Content != "" {
		t.Fatal("invalid UTF-8 file could not be shown without data loss")
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if file, err := readConfigFile(path, os.Geteuid()); err != nil || file.Content != "" || file.Path != path {
		t.Fatalf("empty file is not a valid configuration read: %v", err)
	}
}
