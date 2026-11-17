//go:build !linux && !darwin

package nginxguard

import (
	"context"
	"errors"
	"syscall"
)

type syscallStat struct{ Uid uint32 }

func runAsUser(uid, gid int) *syscall.SysProcAttr { return nil }

func lockControl(ctx context.Context, path string, owner int) (func(), error) {
	return nil, errors.New("Nginx 授权管理仅支持 Linux")
}
