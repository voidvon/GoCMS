package nginxguard

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const agentDir = "/etc/gocms-security"
const httpFile = agentDir + "/http.conf"
const serverFile = agentDir + "/server.conf"
const logFile = "/var/log/gocms-security/probes.log"

type Config struct {
	SocketPath   string `json:"socket_path"`
	NginxBinary  string `json:"nginx_binary"`
	NginxConfig  string `json:"nginx_config"`
	ServerConfig string `json:"server_config"`
	AgentUser    string `json:"agent_user"`
}

func (c Config) Validate() error {
	for _, p := range []string{c.SocketPath, c.NginxBinary, c.NginxConfig, c.ServerConfig} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p || len(p) > 512 || strings.ContainsAny(p, " \t\r\n\x00\"'`;${}\\") {
			return errors.New("invalid installation path")
		}
	}
	if c.NginxConfig == c.ServerConfig || len(c.SocketPath) > 100 || c.AgentUser == "" || strings.Trim(c.AgentUser, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-") != "" {
		return errors.New("invalid agent configuration")
	}
	if strings.ContainsAny(c.ServerConfig, "#*?[]") {
		return errors.New("站点配置路径不能包含注释或通配符字符")
	}
	return nil
}

func Run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: site security-agent install|serve -config <file>")
	}
	flags := flag.NewFlagSet("security-agent", flag.ContinueOnError)
	configPath := flags.String("config", agentDir+"/agent.json", "agent configuration")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if os.Geteuid() != 0 {
		return errors.New("Nginx agent installation and service require root; run GoCMS as its ordinary service user")
	}
	info, err := os.Lstat(*configPath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("agent config must be a regular file")
	}
	data, err := os.ReadFile(*configPath)
	if err != nil {
		return err
	}
	var c Config
	if err = json.Unmarshal(data, &c); err != nil {
		return err
	}
	if err = c.Validate(); err != nil {
		return err
	}
	switch args[0] {
	case "install":
		return Install(c)
	case "serve":
		if info.Mode().Perm()&0022 != 0 {
			return errors.New("agent config must not be writable by other users")
		}
		return serve(c)
	default:
		return errors.New("unknown security-agent command")
	}
}

func command(c Config, mode string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	args := []string{"-t", "-c", c.NginxConfig}
	if mode != "test" {
		args = []string{"-s", mode, "-c", c.NginxConfig}
	}
	out, err := exec.CommandContext(ctx, c.NginxBinary, args...).CombinedOutput()
	if err != nil {
		if len(out) > 2048 {
			out = out[:2048]
		}
		return fmt.Errorf("nginx %s: %s (%w)", mode, strings.TrimSpace(string(out)), err)
	}
	return nil
}

func atomicFile(path string, data []byte, mode os.FileMode) error {
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return errors.New("refusing to replace non-regular file")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".gocms-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

type agent struct {
	mu            sync.Mutex
	config        Config
	snapshot      Snapshot
	result        Result
	rendered      string
	logInfo       os.FileInfo
	logGeneration string
	httpPath      string
	serverPath    string
	statePath     string
	logPath       string
}

func (a *agent) apply(s Snapshot) error {
	h, server, err := Render(s, time.Now(), a.logPath)
	if err != nil {
		return err
	}
	if h+server != a.rendered {
		oldHTTP, err := os.ReadFile(a.httpPath)
		if err != nil {
			return err
		}
		oldServer, err := os.ReadFile(a.serverPath)
		if err != nil {
			return err
		}
		restore := func() { _ = atomicFile(a.httpPath, oldHTTP, 0600); _ = atomicFile(a.serverPath, oldServer, 0600) }
		if err = atomicFile(a.httpPath, []byte(h), 0600); err != nil {
			return err
		}
		if err = atomicFile(a.serverPath, []byte(server), 0600); err != nil {
			restore()
			return err
		}
		if err = command(a.config, "test"); err != nil {
			restore()
			return err
		}
		if err = command(a.config, "reload"); err != nil {
			restore()
			return err
		}
		a.rendered = h + server
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if a.result.Digest != Digest(s) {
		if err = atomicFile(a.statePath, data, 0600); err != nil {
			return err
		}
	}
	a.snapshot = s
	a.result = Result{Digest: Digest(s), AppliedAt: time.Now().UTC().Format(time.RFC3339)}
	return nil
}

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (a *agent) handler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		respond(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	decoder.DisallowUnknownFields()
	switch r.URL.Path {
	case "/apply":
		var s Snapshot
		if err := decoder.Decode(&s); err != nil {
			respond(w, 400, map[string]string{"error": "invalid snapshot"})
			return
		}
		if err := a.apply(s); err != nil {
			respond(w, 502, map[string]string{"error": err.Error()})
			return
		}
		respond(w, 200, a.result)
	case "/events":
		var input struct {
			Cursor string `json:"cursor"`
		}
		if err := decoder.Decode(&input); err != nil {
			respond(w, 400, map[string]string{"error": "invalid cursor"})
			return
		}
		batch, err := a.events(input.Cursor)
		if err != nil {
			respond(w, 500, map[string]string{"error": err.Error()})
			return
		}
		respond(w, 200, batch)
	default:
		respond(w, 404, map[string]string{"error": "not found"})
	}
}

func (a *agent) events(cursor string) (LogBatch, error) {
	batch := LogBatch{Items: []LogEvent{}, Cursor: cursor}
	f, err := os.Open(a.logPath)
	if os.IsNotExist(err) {
		return batch, nil
	}
	if err != nil {
		return batch, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return batch, err
	}
	if a.logInfo == nil || !os.SameFile(a.logInfo, info) {
		a.logGeneration = logIdentity(info)
		a.logInfo = info
	}
	var offset int64
	if parts := strings.Split(cursor, ":"); len(parts) == 2 && parts[0] == a.logGeneration {
		offset, _ = strconv.ParseInt(parts[1], 10, 64)
	}
	if offset < 0 || offset > info.Size() {
		offset = 0
	}
	if offset == 0 && info.Size() > 1<<20 {
		offset = info.Size() - (1 << 20)
		if _, err = f.Seek(offset, io.SeekStart); err != nil {
			return batch, err
		}
		reader := bufio.NewReader(f)
		line, _ := reader.ReadBytes('\n')
		offset += int64(len(line))
	}
	if _, err = f.Seek(offset, io.SeekStart); err != nil {
		return batch, err
	}
	reader := bufio.NewReader(io.LimitReader(f, 1<<20))
	for i := 0; i < 500; i++ {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			break
		}
		offset += int64(len(line))
		if len(line) > 8192 {
			continue
		}
		var event LogEvent
		if json.Unmarshal(line, &event) == nil {
			batch.Items = append(batch.Items, event)
		}
	}
	batch.Cursor = a.logGeneration + ":" + strconv.FormatInt(offset, 10)
	return batch, nil
}

// The device/inode identity survives agent restarts and changes on logrotate.
// Reflection keeps this file buildable on platforms with different Stat types;
// the privileged service itself is only installed on Linux.
func logIdentity(info os.FileInfo) string {
	value := reflect.Indirect(reflect.ValueOf(info.Sys()))
	if value.IsValid() && value.Kind() == reflect.Struct {
		dev, ino := value.FieldByName("Dev"), value.FieldByName("Ino")
		if dev.IsValid() && ino.IsValid() && dev.CanInterface() && ino.CanInterface() {
			return fmt.Sprintf("%v-%v", dev.Interface(), ino.Interface())
		}
	}
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

func serve(c Config) error {
	u, err := user.Lookup(c.AgentUser)
	if err != nil {
		return err
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return err
	}
	parent := filepath.Dir(c.SocketPath)
	if err = os.MkdirAll(parent, 0711); err != nil {
		return err
	}
	if info, err := os.Lstat(parent); err != nil || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
		return errors.New("socket directory must be a real directory writable only by root")
	}
	if info, err := os.Lstat(c.SocketPath); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return errors.New("socket path is not a socket")
		}
		if err = os.Remove(c.SocketPath); err != nil {
			return err
		}
	}
	listener, err := net.Listen("unix", c.SocketPath)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err = os.Chown(c.SocketPath, uid, gid); err != nil {
		return err
	}
	if err = os.Chmod(c.SocketPath, 0600); err != nil {
		return err
	}
	a := &agent{config: c, snapshot: Snapshot{Tenants: []Tenant{}, Rules: []Rule{}}, httpPath: httpFile, serverPath: serverFile, statePath: agentDir + "/snapshot.json", logPath: logFile}
	if b, err := os.ReadFile(agentDir + "/snapshot.json"); err == nil {
		if err = json.Unmarshal(b, &a.snapshot); err != nil {
			return err
		}
	}
	if err = a.apply(a.snapshot); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := &http.Server{Handler: http.HandlerFunc(a.handler), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 15 * time.Second}
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = server.Shutdown(shutdown)
				cancel()
				return
			case <-ticker.C:
				a.mu.Lock()
				if err := a.apply(a.snapshot); err != nil {
					a.result.Error = err.Error()
				}
				a.mu.Unlock()
			}
		}
	}()
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
