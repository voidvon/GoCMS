package theme

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	ManifestFile         = "theme.json"
	ActiveThemeFile      = "theme.json"
	MaxImportArchiveSize = 64 << 20
	MaxThemeFileSize     = 8 << 20
	MaxThemeTotalSize    = 128 << 20
	MaxThemeFileCount    = 10000
)

type Manifest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	Author      string `json:"author,omitempty"`
}

type Info struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	Author      string `json:"author,omitempty"`
	Active      bool   `json:"active"`
}

type Definition struct {
	Manifest      Manifest
	Root          string
	AssetsRoot    string
	TemplatesRoot string
}

func (d Definition) Info(active bool) Info {
	return Info{
		ID:          d.Manifest.ID,
		Name:        d.Manifest.Name,
		Version:     d.Manifest.Version,
		Description: d.Manifest.Description,
		Author:      d.Manifest.Author,
		Active:      active,
	}
}

func ValidID(value string) bool {
	if value == "" || len(value) > 64 || value == "." || value == ".." {
		return false
	}
	for index, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' {
			if index == 0 && (char == '.' || char == '-' || char == '_') {
				return false
			}
			continue
		}
		return false
	}
	return true
}

func LoadActive(dataRoot string) (string, error) {
	if strings.TrimSpace(dataRoot) == "" {
		return "", nil
	}
	b, err := os.ReadFile(filepath.Join(dataRoot, ActiveThemeFile))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read active theme: %w", err)
	}
	var selection struct {
		ThemeID string `json:"theme_id"`
	}
	if err := json.Unmarshal(b, &selection); err != nil {
		return "", fmt.Errorf("parse active theme: %w", err)
	}
	selection.ThemeID = strings.TrimSpace(selection.ThemeID)
	if selection.ThemeID != "" && !ValidID(selection.ThemeID) {
		return "", fmt.Errorf("invalid active theme id")
	}
	return selection.ThemeID, nil
}

func SaveActive(dataRoot, id string) error {
	if strings.TrimSpace(dataRoot) == "" {
		return fmt.Errorf("theme data directory is not configured")
	}
	if !ValidID(id) {
		return fmt.Errorf("invalid theme id")
	}
	if err := os.MkdirAll(dataRoot, 0755); err != nil {
		return fmt.Errorf("create theme data directory: %w", err)
	}
	b, err := json.MarshalIndent(struct {
		ThemeID string `json:"theme_id"`
	}{ThemeID: id}, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dataRoot, ActiveThemeFile), b, 0600)
}

func Resolve(themesRoot, dataRoot, override, defaultTemplates string) (Definition, error) {
	if strings.TrimSpace(override) != "" {
		return definitionFromRoot(filepath.Clean(override))
	}
	activeID, err := LoadActive(dataRoot)
	if err != nil {
		return Definition{}, err
	}
	items, err := List(themesRoot, activeID)
	if err != nil {
		return Definition{}, err
	}
	if len(items) == 0 {
		return Definition{TemplatesRoot: defaultTemplates}, nil
	}
	selectedID := items[0].ID
	for _, item := range items {
		if item.ID == activeID {
			selectedID = item.ID
			break
		}
	}
	if dataRoot != "" && selectedID != activeID {
		if err := SaveActive(dataRoot, selectedID); err != nil {
			return Definition{}, err
		}
	}
	return Find(themesRoot, selectedID)
}

func List(themesRoot, activeID string) ([]Info, error) {
	if strings.TrimSpace(themesRoot) == "" {
		return []Info{}, nil
	}
	entries, err := os.ReadDir(themesRoot)
	if os.IsNotExist(err) {
		return []Info{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read themes directory: %w", err)
	}
	items := make([]Info, 0, len(entries))
	seen := make(map[string]bool)
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		definition, err := definitionFromRoot(filepath.Join(themesRoot, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read theme %s: %w", entry.Name(), err)
		}
		if seen[definition.Manifest.ID] {
			return nil, fmt.Errorf("duplicate theme id: %s", definition.Manifest.ID)
		}
		seen[definition.Manifest.ID] = true
		items = append(items, definition.Info(definition.Manifest.ID == activeID))
	}
	sort.SliceStable(items, func(i, j int) bool {
		left, right := strings.ToLower(items[i].Name), strings.ToLower(items[j].Name)
		if left == right {
			return items[i].ID < items[j].ID
		}
		return left < right
	})
	return items, nil
}

func Find(themesRoot, id string) (Definition, error) {
	if !ValidID(id) {
		return Definition{}, fmt.Errorf("invalid theme id")
	}
	entries, err := os.ReadDir(themesRoot)
	if os.IsNotExist(err) {
		return Definition{}, fmt.Errorf("theme not found: %s", id)
	}
	if err != nil {
		return Definition{}, fmt.Errorf("read themes directory: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		root := filepath.Join(themesRoot, entry.Name())
		definition, err := definitionFromRoot(root)
		if err != nil {
			return Definition{}, fmt.Errorf("read theme %s: %w", entry.Name(), err)
		}
		if definition.Manifest.ID == id {
			return definition, nil
		}
	}
	return Definition{}, fmt.Errorf("theme not found: %s", id)
}

func ImportArchive(themesRoot string, source io.Reader) (Definition, error) {
	if strings.TrimSpace(themesRoot) == "" {
		return Definition{}, fmt.Errorf("theme directory is not configured")
	}
	archive, err := io.ReadAll(io.LimitReader(source, MaxImportArchiveSize+1))
	if err != nil {
		return Definition{}, fmt.Errorf("read theme archive: %w", err)
	}
	if len(archive) > MaxImportArchiveSize {
		return Definition{}, fmt.Errorf("theme archive is too large")
	}
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return Definition{}, fmt.Errorf("invalid theme archive: %w", err)
	}
	if len(reader.File) > MaxThemeFileCount {
		return Definition{}, fmt.Errorf("theme archive contains too many files")
	}
	if err := os.MkdirAll(themesRoot, 0755); err != nil {
		return Definition{}, fmt.Errorf("create themes directory: %w", err)
	}
	temporary, err := os.MkdirTemp(themesRoot, ".theme-import-")
	if err != nil {
		return Definition{}, fmt.Errorf("create theme staging directory: %w", err)
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(temporary)
		}
	}()

	seen := make(map[string]bool)
	var manifestData []byte
	var totalSize uint64
	var copiedTotal int64
	for _, item := range reader.File {
		clean, err := archivePath(item.Name)
		if err != nil {
			return Definition{}, err
		}
		if item.Mode()&os.ModeSymlink != 0 {
			return Definition{}, fmt.Errorf("theme archive cannot contain symbolic links: %s", clean)
		}
		if strings.HasSuffix(item.Name, "/") {
			continue
		}
		if seen[clean] {
			return Definition{}, fmt.Errorf("duplicate theme file: %s", clean)
		}
		seen[clean] = true
		if item.UncompressedSize64 > MaxThemeFileSize {
			return Definition{}, fmt.Errorf("theme file is too large: %s", clean)
		}
		totalSize += item.UncompressedSize64
		if totalSize > MaxThemeTotalSize {
			return Definition{}, fmt.Errorf("theme archive contents are too large")
		}
		if clean == ManifestFile {
			manifestData, err = readZipFile(item, MaxThemeFileSize)
			if err != nil {
				return Definition{}, err
			}
		}
		destination := filepath.Join(temporary, filepath.FromSlash(clean))
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			return Definition{}, err
		}
		file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			return Definition{}, err
		}
		archiveFile, openErr := item.Open()
		var copied int64
		if openErr == nil {
			copied, openErr = io.Copy(file, io.LimitReader(archiveFile, MaxThemeFileSize+1))
			_ = archiveFile.Close()
		}
		closeErr := file.Close()
		if openErr != nil {
			return Definition{}, openErr
		}
		if copied > MaxThemeFileSize {
			return Definition{}, fmt.Errorf("theme file is too large: %s", clean)
		}
		copiedTotal += copied
		if copiedTotal > MaxThemeTotalSize {
			return Definition{}, fmt.Errorf("theme archive contents are too large")
		}
		if closeErr != nil {
			return Definition{}, closeErr
		}
	}
	if len(manifestData) == 0 {
		return Definition{}, fmt.Errorf("theme archive is missing %s", ManifestFile)
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return Definition{}, fmt.Errorf("invalid theme manifest: %w", err)
	}
	manifest, err = normalizeManifest(manifest)
	if err != nil {
		return Definition{}, err
	}
	if !hasHTMLFile(filepath.Join(temporary, "templates")) {
		return Definition{}, fmt.Errorf("theme archive is missing templates")
	}
	target := filepath.Join(themesRoot, manifest.ID)
	if _, err := os.Lstat(target); err == nil {
		return Definition{}, fmt.Errorf("theme already exists: %s", manifest.ID)
	} else if !os.IsNotExist(err) {
		return Definition{}, err
	}
	if err := os.Rename(temporary, target); err != nil {
		return Definition{}, fmt.Errorf("install theme: %w", err)
	}
	keep = true
	return definitionFromRoot(target)
}

func WriteArchive(destination io.Writer, definition Definition) error {
	manifest, err := normalizeManifest(definition.Manifest)
	if err != nil {
		return err
	}
	archive := zip.NewWriter(destination)
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = archive.Close()
		return err
	}
	if err := writeArchiveBytes(archive, ManifestFile, manifestData); err != nil {
		_ = archive.Close()
		return err
	}
	if err := addTree(archive, definition.TemplatesRoot, "templates", nil); err != nil {
		_ = archive.Close()
		return err
	}
	nestedAssets := filepath.Clean(definition.AssetsRoot) == filepath.Join(filepath.Clean(definition.Root), "assets")
	skip := func(relative string) bool {
		if nestedAssets {
			return false
		}
		return relative == ManifestFile || relative == "templates" || strings.HasPrefix(relative, "templates/")
	}
	if err := addTree(archive, definition.AssetsRoot, "assets", skip); err != nil {
		_ = archive.Close()
		return err
	}
	return archive.Close()
}

func definitionFromRoot(root string) (Definition, error) {
	info, err := os.Stat(root)
	if err != nil {
		return Definition{}, err
	}
	if !info.IsDir() {
		return Definition{}, fmt.Errorf("theme root is not a directory")
	}
	manifest, err := readManifest(root)
	if err != nil {
		return Definition{}, err
	}
	manifest, err = normalizeManifest(manifest)
	if err != nil {
		return Definition{}, err
	}
	assetsRoot := filepath.Join(root, "assets")
	templatesRoot := filepath.Join(root, "templates")
	if !isDirectory(templatesRoot) {
		return Definition{}, fmt.Errorf("theme is missing templates directory")
	}
	if !hasHTMLFile(templatesRoot) {
		return Definition{}, fmt.Errorf("theme templates directory contains no HTML files")
	}
	return Definition{Manifest: manifest, Root: root, AssetsRoot: assetsRoot, TemplatesRoot: templatesRoot}, nil
}

func readManifest(root string) (Manifest, error) {
	b, err := os.ReadFile(filepath.Join(root, ManifestFile))
	if err != nil {
		if os.IsNotExist(err) {
			return Manifest{}, fmt.Errorf("theme is missing %s", ManifestFile)
		}
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(b, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("invalid theme manifest: %w", err)
	}
	return manifest, nil
}

func normalizeManifest(manifest Manifest) (Manifest, error) {
	manifest.ID = strings.TrimSpace(manifest.ID)
	if !ValidID(manifest.ID) {
		return Manifest{}, fmt.Errorf("invalid theme id")
	}
	manifest.Name = strings.TrimSpace(manifest.Name)
	if manifest.Name == "" {
		manifest.Name = manifest.ID
	}
	return manifest, nil
}

func archivePath(value string) (string, error) {
	if value == "" || strings.Contains(value, `\`) || strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("invalid theme archive path: %s", value)
	}
	clean := path.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("invalid theme archive path: %s", value)
	}
	if clean != ManifestFile && clean != "templates" && clean != "assets" &&
		!strings.HasPrefix(clean, "templates/") && !strings.HasPrefix(clean, "assets/") {
		return "", fmt.Errorf("theme archive path must be under templates or assets: %s", value)
	}
	return clean, nil
}

func readZipFile(item *zip.File, limit int64) ([]byte, error) {
	file, err := item.Open()
	if err != nil {
		return nil, err
	}
	defer file.Close()
	b, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("theme file is too large: %s", item.Name)
	}
	return b, nil
}

func hasHTMLFile(root string) bool {
	found := false
	_ = filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err != nil || found {
			return err
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".html") {
			found = true
		}
		return nil
	})
	return found
}

func addTree(archive *zip.Writer, root, prefix string, skip func(string) bool) error {
	if strings.TrimSpace(root) == "" {
		return nil
	}
	err := filepath.WalkDir(root, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return fmt.Errorf("theme contains symbolic link: %s", filePath)
		}
		relative, err := filepath.Rel(root, filePath)
		if err != nil {
			return err
		}
		if relative == "." || entry.IsDir() {
			return nil
		}
		relative = filepath.ToSlash(relative)
		if skip != nil && skip(relative) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = path.Join(prefix, relative)
		header.Method = zip.Deflate
		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		file, err := os.Open(filePath)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func writeArchiveBytes(archive *zip.Writer, name string, data []byte) error {
	writer, err := archive.Create(name)
	if err != nil {
		return err
	}
	_, err = writer.Write(data)
	return err
}

func atomicWrite(filePath string, data []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(filePath), ".theme-")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, filePath)
}

func isDirectory(filePath string) bool {
	info, err := os.Stat(filePath)
	return err == nil && info.IsDir()
}
