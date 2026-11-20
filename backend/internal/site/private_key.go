package site

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gocms/internal/payment"
)

func localCredentialCipher(path string, create bool) (*payment.Cipher, error) {
	if path == "" {
		return nil, errors.New("platform credential key path is unavailable")
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && create {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		if err := savePrivateKey(path, base64.StdEncoding.EncodeToString(raw)); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		info, err = os.Lstat(path)
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 256 {
		return nil, errors.New("platform credential key is unavailable")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return payment.NewCipher(strings.TrimSpace(string(raw)))
}

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
