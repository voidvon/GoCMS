//go:build !windows

package site

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

func waitForProcessExit(pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		process, err := os.FindProcess(pid)
		if err != nil {
			return nil
		}
		if err := process.Signal(syscall.Signal(0)); err != nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("等待父进程退出超时")
}
