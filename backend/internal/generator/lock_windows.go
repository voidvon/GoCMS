//go:build windows

package generator

import (
	"os"

	"golang.org/x/sys/windows"
)

func acquirePublishLock(lock *os.File) (func(), error) {
	var overlapped windows.Overlapped
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)
	if err := windows.LockFileEx(windows.Handle(lock.Fd()), flags, 0, 1, 0, &overlapped); err != nil {
		return nil, err
	}
	return func() {
		_ = windows.UnlockFileEx(windows.Handle(lock.Fd()), 0, 1, 0, &overlapped)
	}, nil
}
