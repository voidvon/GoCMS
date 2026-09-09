package site

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

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

func TestExtractUpdateArchive(t *testing.T) {
	archivePath := createTestUpdateArchive(t, map[string]string{
		filepath.ToSlash(filepath.Join("bin", currentBinaryName())): "binary",
		"frontend/dist/index.html":                                  "admin",
		"backend/templates/index.html":                              "template",
	})
	destination := t.TempDir()
	if err := extractUpdateArchive(archivePath, destination); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(destination, "bin", currentBinaryName()))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "binary" {
		t.Fatalf("binary content = %q", content)
	}
}

func TestExtractUpdateArchiveRejectsTraversal(t *testing.T) {
	archivePath := createTestUpdateArchive(t, map[string]string{"../outside": "blocked"})
	if err := extractUpdateArchive(archivePath, t.TempDir()); err == nil {
		t.Fatal("expected path traversal to be rejected")
	}
}

func createTestUpdateArchive(t *testing.T, files map[string]string) string {
	t.Helper()
	archivePath := filepath.Join(t.TempDir(), "update.tar.gz")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	compressed := gzip.NewWriter(file)
	archive := tar.NewWriter(compressed)
	for name, content := range files {
		data := []byte(content)
		if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := archive.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return archivePath
}
