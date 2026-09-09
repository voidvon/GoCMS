package site

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"bilvie/internal/templateconfig"
)

const maxThemeFileSize = 8 << 20

type ThemeFile struct {
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modified_at"`
}

type ThemeFilesResponse struct {
	Name           string               `json:"name"`
	CSSFiles       []ThemeFile          `json:"css_files"`
	TemplateFiles  []ThemeFile          `json:"template_files"`
	TemplateGroups []ThemeTemplateGroup `json:"template_groups"`
}

type ThemeTemplateGroup struct {
	Key         string                    `json:"key"`
	Label       string                    `json:"label"`
	Files       []ThemeFile               `json:"files"`
	Assignments []ThemeTemplateAssignment `json:"assignments"`
}

type ThemeTemplateAssignment struct {
	Key           string `json:"key"`
	Label         string `json:"label"`
	Dimension     string `json:"dimension"`
	DimensionName string `json:"dimension_name"`
	TemplatePath  string `json:"template_path"`
	Available     bool   `json:"available"`
}

type ThemeFileContent struct {
	ThemeFile
	Kind    string `json:"kind"`
	Content string `json:"content"`
}

func (s *Server) adminTheme(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(response)
		return
	}
	if !s.requireAdmin(response, request) {
		return
	}

	kind := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("kind")))
	relativePath := strings.TrimSpace(request.URL.Query().Get("path"))
	if relativePath != "" {
		root, extension, ok := s.themeFileRoot(kind)
		if !ok {
			http.Error(response, "invalid theme file kind", http.StatusBadRequest)
			return
		}
		file, content, err := readThemeFile(root, extension, relativePath)
		if err != nil {
			status := http.StatusInternalServerError
			if err == errInvalidThemePath || err == errInvalidThemeFile {
				status = http.StatusBadRequest
			} else if err == fs.ErrNotExist {
				status = http.StatusNotFound
			}
			http.Error(response, err.Error(), status)
			return
		}
		writeJSON(response, http.StatusOK, ThemeFileContent{ThemeFile: file, Kind: kind, Content: content})
		return
	}

	cssFiles, err := listThemeFiles(s.themeRoot, ".css")
	if err != nil {
		http.Error(response, "读取主题 CSS 失败", http.StatusInternalServerError)
		return
	}
	templateFiles, err := listThemeFiles(s.templateRoot, ".html")
	if err != nil {
		http.Error(response, "读取 HTML 模板失败", http.StatusInternalServerError)
		return
	}
	templateGroups := s.themeTemplateGroups(templateFiles)
	name := filepath.Base(filepath.Clean(s.themeRoot))
	if name == "." || name == string(filepath.Separator) {
		name = "当前主题"
	}
	writeJSON(response, http.StatusOK, ThemeFilesResponse{Name: name, CSSFiles: cssFiles, TemplateFiles: templateFiles, TemplateGroups: templateGroups})
}

func (s *Server) themeTemplateGroups(files []ThemeFile) []ThemeTemplateGroup {
	assignments, _ := templateconfig.List(context.Background(), s.database)
	labels := map[string]string{
		templateconfig.DimensionHome:    "首页",
		templateconfig.DimensionList:    "列表模板",
		templateconfig.DimensionDetail:  "详情模板",
		templateconfig.DimensionMessage: "留言模板",
		templateconfig.DimensionOther:   "其他模板",
	}
	order := []string{
		templateconfig.DimensionHome,
		templateconfig.DimensionList,
		templateconfig.DimensionDetail,
		templateconfig.DimensionMessage,
		templateconfig.DimensionOther,
	}
	groups := make(map[string]*ThemeTemplateGroup, len(order))
	for _, dimension := range order {
		groups[dimension] = &ThemeTemplateGroup{Key: dimension, Label: labels[dimension], Files: []ThemeFile{}, Assignments: []ThemeTemplateAssignment{}}
	}
	for _, file := range files {
		dimension := templateDimension(file.Path)
		group, ok := groups[dimension]
		if !ok {
			continue
		}
		group.Files = append(group.Files, file)
	}
	for _, item := range assignments {
		group, ok := groups[item.Dimension]
		if !ok {
			continue
		}
		group.Assignments = append(group.Assignments, ThemeTemplateAssignment{
			Key: item.Key, Label: item.Label, Dimension: item.Dimension, DimensionName: item.DimensionName,
			TemplatePath: item.TemplatePath, Available: availableTemplate(files, item.TemplatePath),
		})
	}
	result := make([]ThemeTemplateGroup, 0, len(order))
	for _, dimension := range order {
		group := groups[dimension]
		if len(group.Files) == 0 {
			continue
		}
		result = append(result, *group)
	}
	return result
}

func availableTemplate(files []ThemeFile, path string) bool {
	for _, file := range files {
		if file.Path == path {
			return true
		}
	}
	return false
}

func (s *Server) adminThemeAssignment(response http.ResponseWriter, request *http.Request, rawKey string) {
	if request.Method != http.MethodPut {
		methodNotAllowed(response)
		return
	}
	if !s.requireAdmin(response, request) {
		return
	}
	key, err := url.PathUnescape(rawKey)
	if err != nil || strings.TrimSpace(key) == "" {
		http.Error(response, "invalid template assignment", http.StatusBadRequest)
		return
	}
	if _, ok := templateconfig.Default(key); !ok {
		http.Error(response, "该模板由分类配置，不属于全局模板", http.StatusBadRequest)
		return
	}
	var payload struct {
		TemplatePath string `json:"template_path"`
	}
	if err := decodeRequest(request, &payload); err != nil {
		http.Error(response, "invalid template assignment payload", http.StatusBadRequest)
		return
	}
	templatePath, err := templateconfig.NormalizePath(payload.TemplatePath)
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	if s.templateRoot == "" {
		http.Error(response, "publishing is not configured", http.StatusServiceUnavailable)
		return
	}
	if _, _, err := readThemeFile(s.templateRoot, ".html", templatePath); err != nil {
		if err == fs.ErrNotExist {
			http.Error(response, "模板文件不存在", http.StatusBadRequest)
			return
		}
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	if err := templateconfig.Update(request.Context(), s.database, key, templatePath); err != nil {
		http.Error(response, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"ok": true, "key": key, "template_path": templatePath})
}

func templateDimension(filePath string) string {
	name := strings.ToLower(filepath.Base(filePath))
	switch {
	case strings.HasPrefix(name, "index"):
		return templateconfig.DimensionHome
	case name == "msg.html":
		return templateconfig.DimensionMessage
	case strings.Contains(name, "list") || strings.Contains(name, "sort"):
		return templateconfig.DimensionList
	case strings.Contains(name, "detail") || name == "corporation.html":
		return templateconfig.DimensionDetail
	default:
		return templateconfig.DimensionOther
	}
}

func (s *Server) themeFileRoot(kind string) (root, extension string, ok bool) {
	switch kind {
	case "css":
		return s.themeRoot, ".css", s.themeRoot != ""
	case "template":
		return s.templateRoot, ".html", s.templateRoot != ""
	default:
		return "", "", false
	}
}

var errInvalidThemePath = fmt.Errorf("invalid theme file path")
var errInvalidThemeFile = fmt.Errorf("invalid theme file")

func listThemeFiles(root, extension string) ([]ThemeFile, error) {
	if root == "" {
		return []ThemeFile{}, nil
	}
	files := make([]ThemeFile, 0)
	err := filepath.WalkDir(root, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), extension) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(root, filePath)
		if err != nil {
			return err
		}
		files = append(files, ThemeFile{
			Path:       filepath.ToSlash(relative),
			Size:       info.Size(),
			ModifiedAt: info.ModTime().UTC().Format(time.RFC3339),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func readThemeFile(root, extension, relative string) (ThemeFile, string, error) {
	relative = strings.TrimSpace(strings.ReplaceAll(relative, `\`, "/"))
	if relative == "" || strings.HasPrefix(relative, "/") {
		return ThemeFile{}, "", errInvalidThemePath
	}
	clean := pathpkg.Clean(relative)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return ThemeFile{}, "", errInvalidThemePath
	}
	if !strings.EqualFold(filepath.Ext(clean), extension) {
		return ThemeFile{}, "", errInvalidThemeFile
	}
	file, err := os.OpenInRoot(root, filepath.FromSlash(clean))
	if err != nil {
		if os.IsNotExist(err) {
			return ThemeFile{}, "", fs.ErrNotExist
		}
		return ThemeFile{}, "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ThemeFile{}, "", err
	}
	if !info.Mode().IsRegular() {
		return ThemeFile{}, "", fs.ErrNotExist
	}
	if info.Size() > maxThemeFileSize {
		return ThemeFile{}, "", fmt.Errorf("theme file is too large")
	}
	content, err := io.ReadAll(file)
	if err != nil {
		return ThemeFile{}, "", err
	}
	if !utf8.Valid(content) {
		return ThemeFile{}, "", fmt.Errorf("theme file is not UTF-8")
	}
	return ThemeFile{Path: clean, Size: info.Size(), ModifiedAt: info.ModTime().UTC().Format(time.RFC3339)}, string(content), nil
}
