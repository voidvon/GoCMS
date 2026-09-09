package site

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestExtractUpdateBinary(t *testing.T) {
	for _, name := range []string{"gocms", "../gocms", "other"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			archivePath := filepath.Join(root, "update.zip")
			file, err := os.Create(archivePath)
			if err != nil {
				t.Fatal(err)
			}
			writer := zip.NewWriter(file)
			entry, err := writer.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := entry.Write([]byte("new binary")); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(root, "gocms")
			err = extractUpdateBinary(archivePath, target)
			if name != "gocms" {
				if err == nil {
					t.Fatal("expected invalid entry to be rejected")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(target)
			if err != nil || string(content) != "new binary" {
				t.Fatalf("content = %q, error = %v", content, err)
			}
		})
	}
}

func TestUpdateVersionComparison(t *testing.T) {
	left, err := parseUpdateVersion("v0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	right, err := parseUpdateVersion("0.1.99")
	if err != nil {
		t.Fatal(err)
	}
	if compareUpdateVersions(left, right) <= 0 {
		t.Fatal("0.2.0 should be newer than 0.1.99")
	}
	if got := formatUpdateVersion(left); got != "0.2.0" {
		t.Fatalf("formatted version = %q", got)
	}
}

func TestUpdateAssetNameIsZip(t *testing.T) {
	name := updateAssetName("v0.1.0")
	want := "gocms-v0.1.0-" + runtime.GOOS + "-" + runtime.GOARCH + ".zip"
	if name != want {
		t.Fatalf("asset name = %q, want %q", name, want)
	}
}

func TestApplyStagedUpdateReplacesOnlyBinary(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "gocms")
	staging := filepath.Join(root, "downloaded-binary")
	themeFile := filepath.Join(root, "assets", "theme", "blue", "css", "site.css")
	if err := os.MkdirAll(filepath.Dir(themeFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old binary"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staging, []byte("new binary"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(themeFile, []byte("administrator theme"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := applyStagedUpdate(staging, target); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new binary" {
		t.Fatalf("binary content = %q", content)
	}
	theme, err := os.ReadFile(themeFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(theme) != "administrator theme" {
		t.Fatalf("theme content = %q", theme)
	}
}

func TestApplyStagedUpdateRejectsDirectory(t *testing.T) {
	root := t.TempDir()
	staging := filepath.Join(root, "downloaded-directory")
	if err := os.Mkdir(staging, 0700); err != nil {
		t.Fatal(err)
	}
	if err := applyStagedUpdate(staging, filepath.Join(root, "gocms")); err == nil {
		t.Fatal("expected directory update to be rejected")
	}
}
