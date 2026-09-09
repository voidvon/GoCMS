package embedded

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// The release build places the admin SPA and the default publishing templates
// in this directory before compiling the binary. bootstrap.txt keeps the Go
// package buildable before the generated assets have been prepared.
//
//go:embed assets
var files embed.FS

func assetFS(path string) (fs.FS, error) {
	return fs.Sub(files, filepath.ToSlash(filepath.Join("assets", path)))
}

// FrontendFS returns the embedded admin SPA filesystem.
func FrontendFS() (fs.FS, error) {
	return assetFS(filepath.Join("frontend", "dist"))
}

func templateFS() (fs.FS, error) {
	return assetFS("templates")
}

// EnsureTemplates initializes the external template directory only when it is
// absent. Existing files, including administrator edits, are never replaced.
func EnsureTemplates(root string) error {
	if root == "" {
		return errors.New("template directory is empty")
	}
	if _, err := os.Stat(root); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := copyFS(templateFS, root); err != nil {
		return fmt.Errorf("initialize external templates: %w", err)
	}
	return nil
}

func copyFS(source func() (fs.FS, error), destination string) error {
	root, err := source()
	if err != nil {
		return err
	}
	return fs.WalkDir(root, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		target := destination
		if path != "." {
			target = filepath.Join(destination, filepath.FromSlash(path))
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("embedded resource is a symlink: %s", path)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("embedded resource is not a regular file: %s", path)
		}
		data, err := fs.ReadFile(root, path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
}
