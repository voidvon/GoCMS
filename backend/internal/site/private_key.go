package site

import (
	"os"
	"path/filepath"
)

// savePrivateKey publishes a fully written private key without overwriting existing files.
func savePrivateKey(path, key string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".private-key-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.WriteString(key + "\n"); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	// Link publishes a complete private file atomically and never overwrites.
	return os.Link(f.Name(), path)
}
