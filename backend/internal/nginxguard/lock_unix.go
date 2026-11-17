//go:build linux || darwin

package nginxguard

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"
)

type syscallStat = syscall.Stat_t

func runAsUser(uid, gid int) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: []uint32{}}}
}

func lockControl(ctx context.Context, path string, owner int) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	if err := checkTrustedPath(path, false, owner); err != nil {
		f.Close()
		return nil, err
	}
	for {
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
		} else if !errors.Is(err, syscall.EWOULDBLOCK) {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, errors.New("等待 Nginx 管理锁超时，请稍后重试")
		case <-time.After(50 * time.Millisecond):
		}
	}
}
