package embedded

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureTemplatesPreservesExternalDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "templates")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	custom := filepath.Join(root, "index.html")
	if err := os.WriteFile(custom, []byte("administrator template"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureTemplates(root); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(custom)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "administrator template" {
		t.Fatalf("template content = %q", content)
	}
}
