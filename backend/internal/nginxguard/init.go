package nginxguard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func sudoRule(uid int, binary string) []byte {
	return []byte(fmt.Sprintf("# Managed by GoCMS nginx setup. Only this exact command is allowed.\n#%d ALL=(root) NOPASSWD: %s %s\nDefaults!%s !requiretty\n", uid, binary, controlArgument, binary))
}

func ensureSudoInclude(data []byte, dir string) []byte {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && (fields[0] == "#includedir" || fields[0] == "@includedir") && strings.Trim(fields[1], "\"") == dir {
			return data
		}
	}
	return append(append([]byte{}, data...), []byte("\n#includedir "+dir+"\n")...)
}

func makeTrustedDirectory(path string, mode os.FileMode, owner int) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return err
	}
	if err := checkTrustedPath(path, true, owner); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

func validateSudoFile(binary, path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-c", "-f", path)
	cmd.Env, cmd.Dir = commandEnvironment(), "/"
	var diagnostic diagnosticOutput
	cmd.Stdout, cmd.Stderr = &diagnostic, &diagnostic
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sudo 授权配置检查失败：%s（%w）", strings.TrimSpace(diagnostic.String()), err)
	}
	return nil
}

func validateSudoContents(binary, dir string, data []byte) error {
	f, err := os.CreateTemp(dir, ".sudoers-check-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0440); err == nil {
		_, err = f.Write(data)
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return validateSudoFile(binary, f.Name())
}

// initializeWithLayout has no persistent service. Every installed executable,
// policy and ancestor stays root-owned; failures restore only files we changed.
func initializeWithLayout(ctx context.Context, l controlLayout, c Config, snapshot Snapshot, source, visudo string, uid int, verify func(installation) error) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if uid <= 0 {
		return errors.New("Nginx 授权必须绑定普通系统用户")
	}
	if err := Validate(snapshot); err != nil {
		return err
	}
	for _, path := range []string{c.NginxBinary, c.NginxConfig, c.ServerConfig, l.sudoersMain} {
		if err := checkTrustedPath(path, false, l.owner); err != nil {
			return err
		}
	}
	for dir, mode := range map[string]os.FileMode{l.state: 0700, filepath.Dir(l.log): 0700, filepath.Dir(l.binary): 0755, filepath.Dir(l.sudoers): 0750} {
		if err := makeTrustedDirectory(dir, mode, l.owner); err != nil {
			return err
		}
	}
	unlock, err := lockControl(ctx, filepath.Join(l.state, "control.lock"), l.owner)
	if err != nil {
		return err
	}
	defer unlock()
	paths := []string{l.binary, filepath.Join(l.state, "control.json"), l.sudoers, l.sudoersMain}
	previous := make([]savedFile, len(paths))
	for i, path := range paths {
		previous[i], err = rememberFile(path)
		if err != nil {
			return err
		}
		if previous[i].exists {
			if err := checkTrustedPath(path, false, l.owner); err != nil {
				return err
			}
		}
	}
	installed := installation{Protocol: controlProtocol, InitializedAt: time.Now().UTC().Format(time.RFC3339), UID: uid, Config: c}
	raw, err := json.MarshalIndent(installed, "", "  ")
	if err != nil {
		return err
	}
	binary, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	updatedSudo := ensureSudoInclude(previous[3].data, filepath.Dir(l.sudoers))
	if err := validateSudoContents(visudo, l.state, sudoRule(uid, l.binary)); err != nil {
		return err
	}
	if err := validateSudoContents(visudo, l.state, updatedSudo); err != nil {
		return err
	}
	if !bytes.Equal(updatedSudo, previous[3].data) {
		backup := filepath.Join(l.state, "sudoers.before")
		if _, err := os.Lstat(backup); os.IsNotExist(err) {
			if err := atomicFile(backup, previous[3].data, 0600); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}
	contents := [][]byte{binary, raw, sudoRule(uid, l.binary), updatedSudo}
	modes := []os.FileMode{0755, 0600, 0440, previous[3].mode}
	var changed []savedFile
	rollback := func(cause error) error {
		for i := len(changed) - 1; i >= 0; i-- {
			cause = errors.Join(cause, changed[i].restore())
		}
		return cause
	}
	for i, file := range previous {
		if file.exists && bytes.Equal(file.data, contents[i]) && file.mode == modes[i] {
			continue
		}
		if err := atomicFile(file.path, contents[i], modes[i]); err != nil {
			return rollback(err)
		}
		changed = append(changed, file)
	}
	if err := validateSudoFile(visudo, l.sudoers); err != nil {
		return rollback(err)
	}
	if err := validateSudoFile(visudo, l.sudoersMain); err != nil {
		return rollback(err)
	}
	if err := verify(installed); err != nil {
		return rollback(fmt.Errorf("无法以 %s 验证免密受限授权：%w", c.RunUser, err))
	}
	direct := NewDirect(c)
	direct.engine.backupDir, direct.engine.logPath = l.state, l.log
	if _, err := direct.Apply(snapshot); err != nil {
		return rollback(err)
	}
	return nil
}

// Initialize is a root-only CLI operation using the settings saved in the
// admin. Web requests cannot invoke this installer or replace its executable.
func Initialize(ctx context.Context, c Config, snapshot Snapshot) error {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return errors.New("请在 Linux 服务器以 root 执行 ./gocms nginx setup；日常 GoCMS 继续使用普通用户运行")
	}
	uid, err := lookupRunUser(c.RunUser)
	if err != nil {
		return err
	}
	u, err := user.Lookup(c.RunUser)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return err
	}
	visudo, err := exec.LookPath("visudo")
	if err != nil {
		return errors.New("服务器缺少 visudo，请先安装 sudo 后重新执行 nginx setup")
	}
	for _, binary := range []string{visudo, "/usr/bin/sudo"} {
		if err := checkTrustedPath(binary, false, 0); err != nil {
			return err
		}
	}
	source, err := os.Executable()
	if err != nil {
		return err
	}
	return initializeWithLayout(ctx, systemLayout(), c, snapshot, source, visudo, uid, func(installed installation) error {
		data, _ := json.Marshal(controlRequest{Protocol: controlProtocol, Action: "status", Config: c})
		cmd := exec.CommandContext(ctx, "/usr/bin/sudo", "-n", "-u", "root", "--", controlBinary, controlArgument)
		cmd.Dir, cmd.Env = "/", commandEnvironment()
		cmd.Stdin = bytes.NewReader(data)
		cmd.SysProcAttr = runAsUser(uid, gid)
		var output bytes.Buffer
		var diagnostic diagnosticOutput
		cmd.Stdout, cmd.Stderr = &output, &diagnostic
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s（%w）", strings.TrimSpace(diagnostic.String()), err)
		}
		var response controlResponse
		if err := json.Unmarshal(output.Bytes(), &response); err != nil || response.Error != "" || response.Setup == nil || !response.Setup.Ready {
			return fmt.Errorf("Nginx 授权入口自检失败：%s", response.Error)
		}
		return nil
	})
}
