package theme

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestImportExportAndActiveSelection(t *testing.T) {
	themesRoot := filepath.Join(t.TempDir(), "themes")
	archive := makeArchive(t, map[string]string{
		ManifestFile:           `{"id":"plain","name":"Plain","version":"2.0.0"}`,
		"templates/index.html": "<main>plain</main>",
		"assets/css/site.css":  "body { color: red; }",
	})

	definition, err := ImportArchive(themesRoot, bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	if definition.Manifest.ID != "plain" || definition.Manifest.Name != "Plain" {
		t.Fatalf("unexpected definition: %+v", definition.Manifest)
	}
	if filepath.Base(definition.AssetsRoot) != "assets" || filepath.Base(definition.TemplatesRoot) != "templates" {
		t.Fatalf("unexpected theme roots: %+v", definition)
	}
	if content, err := os.ReadFile(filepath.Join(definition.TemplatesRoot, "index.html")); err != nil || string(content) != "<main>plain</main>" {
		t.Fatalf("imported template = %q, err = %v", content, err)
	}

	items, err := List(themesRoot, definition.Manifest.ID)
	if err != nil || len(items) != 1 || !items[0].Active {
		t.Fatalf("theme list = %+v, err = %v", items, err)
	}
	dataRoot := filepath.Join(t.TempDir(), "data")
	if err := SaveActive(dataRoot, definition.Manifest.ID); err != nil {
		t.Fatal(err)
	}
	if active, err := LoadActive(dataRoot); err != nil || active != "plain" {
		t.Fatalf("active theme = %q, err = %v", active, err)
	}
	resolved, err := Resolve(themesRoot, dataRoot, "", "")
	if err != nil || resolved.Manifest.ID != "plain" {
		t.Fatalf("resolved theme = %+v, err = %v", resolved.Manifest, err)
	}

	var exported bytes.Buffer
	if err := WriteArchive(&exported, definition); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(exported.Bytes()), int64(exported.Len()))
	if err != nil {
		t.Fatal(err)
	}
	paths := make(map[string]bool)
	for _, file := range reader.File {
		paths[file.Name] = true
	}
	for _, path := range []string{ManifestFile, "templates/index.html", "assets/css/site.css"} {
		if !paths[path] {
			t.Fatalf("exported archive is missing %s", path)
		}
	}
	var manifest Manifest
	manifestFile := findZipFile(t, reader, ManifestFile)
	manifestData, err := io.ReadAll(manifestFile)
	if closeErr := manifestFile.(io.Closer).Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil || manifest.ID != "plain" {
		t.Fatalf("exported manifest = %+v, err = %v", manifest, err)
	}
}

func TestTemplateGroupMetadataOverridesFallback(t *testing.T) {
	themesRoot := filepath.Join(t.TempDir(), "templates")
	archive := makeArchive(t, map[string]string{
		ManifestFile:                   `{"id":"grouped","name":"Grouped","template_groups":{"home":["landing.html"],"cover":["index.html"],"content":["pages/article.html"]}}`,
		"templates/index.html":         "<main>home</main>",
		"templates/landing.html":       "<main>landing</main>",
		"templates/pages/article.html": "<main>article</main>",
	})

	definition, err := ImportArchive(themesRoot, bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	if got := definition.TemplateGroupFor("landing.html"); got != TemplateGroupHome {
		t.Fatalf("explicit home group = %q", got)
	}
	if got := definition.TemplateGroupFor("index.html"); got != TemplateGroupCover {
		t.Fatalf("explicit cover group = %q", got)
	}
	if got := definition.TemplateGroupFor("pages/article.html"); got != TemplateGroupContent {
		t.Fatalf("explicit content group = %q", got)
	}
	if got := definition.TemplateGroupFor("category_list.html"); got != TemplateGroupList {
		t.Fatalf("fallback list group = %q", got)
	}
	if got := definition.TemplateGroupFor("search.html"); got != TemplateGroupPublic {
		t.Fatalf("fallback public group = %q", got)
	}
}

func TestPublicAndLegacyOtherTemplateGroup(t *testing.T) {
	themesRoot := filepath.Join(t.TempDir(), "templates")
	archive := makeArchive(t, map[string]string{
		ManifestFile:             `{"id":"public-theme","name":"PublicTheme","template_groups":{"public":["search.html"],"other":["msg.html"]}}`,
		"templates/search.html":  "<main>search</main>",
		"templates/msg.html":     "<main>msg</main>",
	})

	definition, err := ImportArchive(themesRoot, bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	if got := definition.TemplateGroupFor("search.html"); got != TemplateGroupPublic {
		t.Fatalf("explicit public group = %q", got)
	}
	if got := definition.TemplateGroupFor("msg.html"); got != TemplateGroupPublic {
		t.Fatalf("legacy other mapped to public = %q", got)
	}
	if label := TemplateGroupLabel(TemplateGroupPublic); label != "公共模板" {
		t.Fatalf("label = %q", label)
	}
	if label := TemplateGroupLabel("other"); label != "公共模板" {
		t.Fatalf("label for other = %q", label)
	}
}

func TestTemplateGroupMetadataRejectsUnsafePath(t *testing.T) {
	themesRoot := filepath.Join(t.TempDir(), "templates")
	archive := makeArchive(t, map[string]string{
		ManifestFile:           `{"id":"unsafe-groups","template_groups":{"content":["../secret.html"]}}`,
		"templates/index.html": "<main>home</main>",
	})
	if _, err := ImportArchive(themesRoot, bytes.NewReader(archive)); err == nil {
		t.Fatal("unsafe template group path was accepted")
	}
}

func TestImportRejectsUnsafeArchivePath(t *testing.T) {
	themesRoot := filepath.Join(t.TempDir(), "themes")
	archive := makeArchive(t, map[string]string{
		ManifestFile:     `{"id":"unsafe","name":"Unsafe"}`,
		"../outside.txt": "must not be written",
	})
	if _, err := ImportArchive(themesRoot, bytes.NewReader(archive)); err == nil {
		t.Fatal("unsafe archive path was accepted")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(themesRoot), "outside.txt")); !os.IsNotExist(err) {
		t.Fatalf("unsafe file exists, err = %v", err)
	}
}

func TestListRejectsLegacyThemeLayout(t *testing.T) {
	themesRoot := filepath.Join(t.TempDir(), "themes")
	legacyRoot := filepath.Join(themesRoot, "legacy")
	if err := os.MkdirAll(filepath.Join(legacyRoot, "css"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyRoot, ManifestFile), []byte(`{"id":"legacy","name":"Legacy"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyRoot, "index.html"), []byte("<main>legacy</main>"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyRoot, "css", "site.css"), []byte("body{}"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := List(themesRoot, ""); err == nil {
		t.Fatal("legacy theme layout was accepted")
	}
}

func TestImportRejectsLegacyThemeArchive(t *testing.T) {
	themesRoot := filepath.Join(t.TempDir(), "themes")
	archive := makeArchive(t, map[string]string{
		ManifestFile:   `{"id":"legacy","name":"Legacy"}`,
		"index.html":   "<main>legacy</main>",
		"css/site.css": "body{}",
	})
	if _, err := ImportArchive(themesRoot, bytes.NewReader(archive)); err == nil {
		t.Fatal("legacy theme archive was accepted")
	}
}

func makeArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range files {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(file, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func findZipFile(t *testing.T, reader *zip.Reader, name string) io.Reader {
	t.Helper()
	for _, file := range reader.File {
		if file.Name != name {
			continue
		}
		opened, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		return opened
	}
	t.Fatalf("zip file not found: %s", name)
	return nil
}
