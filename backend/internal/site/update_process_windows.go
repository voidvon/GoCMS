//go:build windows

package site

import "time"

func waitForProcessExit(_ int, _ time.Duration) error {
	// The old process exits shortly after starting this helper. Windows does not
	// expose the Unix signal-0 process probe, so leave a small replacement window.
	time.Sleep(2 * time.Second)
	return nil
}
