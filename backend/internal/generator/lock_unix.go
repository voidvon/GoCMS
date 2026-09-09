//go:build !windows

package generator

import (
	"os"
	"syscall"
)

func acquirePublishLock(lock *os.File) (func(), error) {
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	}, nil
}
