package nginxguard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const controlProtocol = 1
const controlBinary = "/usr/local/libexec/gocms-nginx"
const controlArgument = "__nginx-control"
const maxControlInput = 4 << 20

type SetupStatus struct {
	Ready         bool   `json:"ready"`
	InitializedAt string `json:"initialized_at,omitempty"`
	Protocol      int    `json:"protocol,omitempty"`
	Message       string `json:"message,omitempty"`
}

type installation struct {
	Protocol      int    `json:"protocol"`
	InitializedAt string `json:"initialized_at"`
	UID           int    `json:"uid"`
	Config        Config `json:"config"`
}

type controlRequest struct {
	Protocol int       `json:"protocol"`
	Action   string    `json:"action"`
	Config   Config    `json:"config"`
	Snapshot *Snapshot `json:"snapshot,omitempty"`
	Cursor   string    `json:"cursor,omitempty"`
}

type controlResponse struct {
	Error  string       `json:"error,omitempty"`
	Result *Result      `json:"result,omitempty"`
	Batch  *LogBatch    `json:"batch,omitempty"`
	Setup  *SetupStatus `json:"setup,omitempty"`
}

type controlLayout struct {
	state, log, binary, sudoers, sudoersMain string
	owner                                    int
}

func systemLayout() controlLayout {
	return controlLayout{state: stateDir, log: logFile, binary: controlBinary, sudoers: "/etc/sudoers.d/gocms-nginx", sudoersMain: "/etc/sudoers", owner: 0}
}

// checkTrustedPath rejects links, writable ancestors and files that the web
// process could replace. The root-owned copy is intentionally separate from
// the self-updating web executable.
func checkTrustedPath(path string, directory bool, owner int) error {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscallStat)
		if !ok || int(stat.Uid) != owner && int(stat.Uid) != 0 {
			return fmt.Errorf("文件或目录必须由 root 管理：%s", current)
		}
		wantDir := current != path || directory
		if wantDir && !info.IsDir() || !wantDir && !info.Mode().IsRegular() {
			return fmt.Errorf("授权路径不能为符号链接或特殊文件：%s", current)
		}
		if info.Mode().Perm()&0022 != 0 && !(wantDir && info.Mode()&os.ModeSticky != 0 && stat.Uid == 0) {
			return fmt.Errorf("授权路径不能允许其他用户写入：%s", current)
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
}

func decodeControl(r io.Reader, value any) error {
	data, err := io.ReadAll(io.LimitReader(r, maxControlInput+1))
	if err != nil {
		return err
	}
	if len(data) > maxControlInput {
		return errors.New("Nginx 同步数据超过大小限制")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("Nginx 同步数据无效：%w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("Nginx 同步数据只能包含一个 JSON 对象")
	}
	return nil
}

func readControlRequest(ctx context.Context, input io.Reader) (controlRequest, error) {
	type decoded struct {
		request controlRequest
		err     error
	}
	done := make(chan decoded, 1)
	go func() {
		var request controlRequest
		err := decodeControl(input, &request)
		done <- decoded{request, err}
	}()
	select {
	case value := <-done:
		return value.request, value.err
	case <-ctx.Done():
		return controlRequest{}, errors.New("读取 Nginx 同步数据超时")
	}
}

func (l controlLayout) load() (installation, error) {
	var installed installation
	path := filepath.Join(l.state, "control.json")
	if err := checkTrustedPath(path, false, l.owner); err != nil {
		return installed, fmt.Errorf("Nginx 授权配置不可用，请以 root 重新执行 nginx setup：%w", err)
	}
	f, err := os.Open(path)
	if err != nil {
		return installed, err
	}
	defer f.Close()
	if err := decodeControl(f, &installed); err != nil {
		return installed, err
	}
	if installed.Protocol != controlProtocol || installed.UID <= 0 {
		return installed, errors.New("Nginx 授权版本或用户无效，请以 root 重新执行 nginx setup")
	}
	return installed, installed.Config.Validate()
}

func (l controlLayout) execute(ctx context.Context, installed installation, request controlRequest) (controlResponse, error) {
	var response controlResponse
	if request.Protocol != controlProtocol || request.Config != installed.Config {
		return response, errors.New("接入设置与 root 授权不一致，请保存设置后由 root 重新执行 nginx setup")
	}
	if request.Action != "apply" && request.Action != "events" && request.Action != "status" {
		return response, errors.New("不支持的 Nginx 管理操作")
	}
	if request.Action == "apply" {
		if request.Snapshot == nil || request.Cursor != "" {
			return response, errors.New("缺少同步名单或包含无效参数")
		}
		if err := Validate(*request.Snapshot); err != nil {
			return response, err
		}
	} else if request.Snapshot != nil || len(request.Cursor) > 128 || request.Action != "events" && request.Cursor != "" {
		return response, errors.New("Nginx 管理参数无效")
	}
	if request.Action == "status" {
		response.Setup = &SetupStatus{Ready: true, InitializedAt: installed.InitializedAt, Protocol: installed.Protocol}
		return response, nil
	}
	lockCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	unlock, err := lockControl(lockCtx, filepath.Join(l.state, "control.lock"), l.owner)
	if err != nil {
		return response, err
	}
	defer unlock()
	// Initialization may have changed the pinned settings while this request
	// waited for another process. Re-read them under the same filesystem lock.
	current, err := l.load()
	if err != nil {
		return response, err
	}
	if current.Config != request.Config || current.UID != installed.UID {
		return response, errors.New("Nginx 授权已变更，请重新读取接入设置后同步")
	}
	installed = current
	if err := checkTrustedPath(filepath.Dir(l.log), true, l.owner); err != nil {
		return response, err
	}
	if _, err := os.Lstat(l.log); err == nil {
		if err := checkTrustedPath(l.log, false, l.owner); err != nil {
			return response, err
		}
	} else if !os.IsNotExist(err) {
		return response, err
	}
	direct := NewDirect(installed.Config)
	direct.engine.backupDir = l.state
	direct.engine.logPath = l.log
	switch request.Action {
	case "apply":
		for _, path := range []string{installed.Config.NginxBinary, installed.Config.NginxConfig, installed.Config.ServerConfig} {
			if err := checkTrustedPath(path, false, l.owner); err != nil {
				return response, err
			}
		}
		result, err := direct.Apply(*request.Snapshot)
		if err != nil {
			return response, err
		}
		response.Result = &result
	case "events":
		batch, err := direct.Events(request.Cursor)
		if err != nil {
			return response, err
		}
		response.Batch = &batch
	}
	return response, nil
}

// RunControl is the only command allowed by sudoers. It accepts no arguments,
// reads pinned root-owned settings and never accepts paths or shell commands.
func RunControl(args []string, input io.Reader, output io.Writer) error {
	if os.Geteuid() != 0 || len(args) != 0 {
		return errors.New("Nginx 受限管理入口只允许 root 执行且不接受命令行参数")
	}
	layout := systemLayout()
	installed, err := layout.load()
	var response controlResponse
	if err == nil {
		if sudoUID := os.Getenv("SUDO_UID"); sudoUID != "" && sudoUID != "0" && sudoUID != strconv.Itoa(installed.UID) {
			err = errors.New("当前系统用户未获得 Nginx 管理授权")
		}
	}
	if err == nil {
		readCtx, stopRead := context.WithTimeout(context.Background(), 5*time.Second)
		request, readErr := readControlRequest(readCtx, input)
		stopRead()
		err = readErr
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			response, err = layout.execute(ctx, installed, request)
		}
	}
	if err != nil {
		response.Error = err.Error()
	}
	if encodeErr := json.NewEncoder(output).Encode(response); encodeErr != nil {
		return encodeErr
	}
	return err
}

// Client starts a short-lived instance of the same GoCMS executable installed
// by nginx setup. It never runs the web-writable main binary through sudo.
type Client struct{}

func (Client) call(ctx context.Context, request controlRequest) (controlResponse, error) {
	var response controlResponse
	if err := request.Config.Validate(); err != nil {
		return response, err
	}
	if err := checkTrustedPath(controlBinary, false, 0); err != nil {
		return response, fmt.Errorf("Nginx 尚未初始化或授权入口不可用，请先保存接入设置，再以 root 执行 ./gocms nginx setup：%w", err)
	}
	request.Protocol = controlProtocol
	data, err := json.Marshal(request)
	if err != nil || len(data) > maxControlInput {
		return response, errors.New("Nginx 同步数据过大或无效")
	}
	ctx, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/sudo", "-n", "-u", "root", "--", controlBinary, controlArgument)
	cmd.Dir, cmd.Env = "/", commandEnvironment()
	cmd.Stdin = bytes.NewReader(data)
	stdout := cappedOutput{limit: 8 << 20}
	var stderr diagnosticOutput
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	if stdout.exceeded {
		return response, errors.New("Nginx 管理返回数据过大")
	}
	if decodeErr := json.Unmarshal(stdout.Bytes(), &response); decodeErr == nil && response.Error != "" {
		return response, errors.New(response.Error)
	}
	if err != nil {
		return response, fmt.Errorf("Nginx 授权调用失败（系统用户 %s），请确认 root 已执行 nginx setup 且授权用户正确：%s（%w）", ExecutionUser(), strings.TrimSpace(stderr.String()), err)
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		return response, errors.New("Nginx 管理入口返回了无效数据，请以 root 重新执行 nginx setup")
	}
	return response, nil
}

type cappedOutput struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (b *cappedOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.limit - b.Len()
	if n > remaining {
		b.exceeded = true
		p = p[:remaining]
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}

func (c Client) Apply(ctx context.Context, config Config, snapshot Snapshot) (Result, error) {
	response, err := c.call(ctx, controlRequest{Action: "apply", Config: config, Snapshot: &snapshot})
	if err == nil && response.Result == nil {
		err = errors.New("Nginx 管理入口未确认同步结果")
	}
	if err != nil {
		return Result{}, err
	}
	return *response.Result, nil
}

func (c Client) Events(ctx context.Context, config Config, cursor string) (LogBatch, error) {
	response, err := c.call(ctx, controlRequest{Action: "events", Config: config, Cursor: cursor})
	if err == nil && response.Batch == nil {
		err = errors.New("Nginx 管理入口未返回探测记录")
	}
	if err != nil {
		return LogBatch{}, err
	}
	return *response.Batch, nil
}

func (c Client) Setup(ctx context.Context, config Config) SetupStatus {
	response, err := c.call(ctx, controlRequest{Action: "status", Config: config})
	if err != nil {
		return SetupStatus{Message: err.Error()}
	}
	if response.Setup == nil {
		return SetupStatus{Message: "Nginx 初始化状态无效，请以 root 重新执行 nginx setup"}
	}
	return *response.Setup
}

func lookupRunUser(name string) (int, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return 0, err
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil || uid <= 0 {
		return 0, errors.New("请配置实际运行 GoCMS 的普通系统用户，例如 www")
	}
	return uid, nil
}
