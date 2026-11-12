package site

import (
	"os"
	"testing"

	"gocms/internal/imageopt"
)

func TestMain(m *testing.M) {
	if imageopt.RunWorker() {
		os.Exit(0)
	}
	os.Exit(m.Run())
}
