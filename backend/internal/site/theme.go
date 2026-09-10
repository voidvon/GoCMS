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

	"gocms/internal/routing"
	"gocms/internal/templateconfig"
	themepkg "gocms/internal/theme"
)

const maxThemeFileSize = 8 << 20

type ThemeFile struct {
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modified_at"`
}

type ThemeFilesResponse struct {
	Name           string               `json:"name"`
	ActiveTheme    string               `json:"active_theme"`
	Themes         []themepkg.Info      `json:"themes"`
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

func (s *Server) ConfigureThemeCatalog(themesRoot, dataRoot string, definition themepkg.Definition) {
	s.themeMu.Lock()
	s.themeBase = themesRoot
	s.themeData = dataRoot
	s.activeTheme = definition
	s.themeRoot = definition.AssetsRoot
	s.templateRoot = definition.TemplatesRoot
	s.themeMu.Unlock()
}

func (s *Server) themeState() (string, string, themepkg.Definition) {
	s.themeMu.RLock()
	defer s.themeMu.RUnlock()
	return s.themeBase, s.themeData, s.activeTheme
}

func (s *Server) themePaths() (string, string) {
	s.themeMu.RLock()
	defer s.themeMu.RUnlock()
	return s.themeRoot, s.templateRoot
}

func (s *Server) setActiveTheme(definition themepkg.Definition) {
	s.themeMu.Lock()
	s.activeTheme = definition
	s.themeRoot = definition.AssetsRoot
	s.templateRoot = definition.TemplatesRoot
	s.themeMu.Unlock()
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

	themeRoot, templateRoot := s.themePaths()
	cssFiles, err := listThemeFiles(themeRoot, ".css")
	if err != nil {
		http.Error(response, "读取主题 CSS 失败", http.StatusInternalServerError)
		return
	}
	templateFiles, err := listThemeFiles(templateRoot, ".html")
	if err != nil {
		http.Error(response, "读取 HTML 模板失败", http.StatusInternalServerError)
		return
	}
	templateGroups := s.themeTemplateGroups(templateFiles)
	base, _, active := s.themeState()
	themes, err := themepkg.List(base, active.Manifest.ID)
	if err != nil {
		http.Error(response, "读取主题列表失败", http.StatusInternalServerError)
		return
	}
	name := active.Manifest.Name
	if name == "" {
		name = filepath.Base(filepath.Clean(themeRoot))
	}
	if name == "." || name == string(filepath.Separator) {
		name = "当前主题"
	}
	writeJSON(response, http.StatusOK, ThemeFilesResponse{Name: name, ActiveTheme: active.Manifest.ID, Themes: themes, CSSFiles: cssFiles, TemplateFiles: templateFiles, TemplateGroups: templateGroups})
}

func (s *Server) adminThemeActivate(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(response)
		return
	}
	if !s.requireAdmin(response, request) {
		return
	}
	var payload struct {
		ID string `json:"id"`
	}
	if err := decodeRequest(request, &payload); err != nil {
		http.Error(response, "invalid theme activation payload", http.StatusBadRequest)
		return
	}
	id := strings.TrimSpace(payload.ID)
	base, dataRoot, _ := s.themeState()
	if base == "" {
		http.Error(response, "主题目录未配置", http.StatusServiceUnavailable)
		return
	}
	definition, err := themepkg.Find(base, id)
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	var report any
	started := false
	if s.publication != nil {
		if err := s.validateThemeTemplates(definition); err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		s.publication.mu.Lock()
		if s.publication.report.State == "running" {
			s.publication.mu.Unlock()
			http.Error(response, "网站正在发布，请稍后切换主题", http.StatusConflict)
			return
		}
		if err := themepkg.SaveActive(dataRoot, definition.Manifest.ID); err != nil {
			s.publication.mu.Unlock()
			http.Error(response, err.Error(), http.StatusInternalServerError)
			return
		}
		s.setActiveTheme(definition)
		s.publication.publisher.Templates = definition.TemplatesRoot
		s.publication.publisher.Theme = definition.AssetsRoot
		publicationReport, publishStarted := s.startPublishLocked(s.publication, false)
		report = publicationReport
		started = publishStarted
		s.publication.mu.Unlock()
	} else {
		if err := themepkg.SaveActive(dataRoot, definition.Manifest.ID); err != nil {
			http.Error(response, err.Error(), http.StatusInternalServerError)
			return
		}
		s.setActiveTheme(definition)
	}

	result := map[string]any{
		"ok":              true,
		"theme":           definition.Info(true),
		"publish_started": started,
	}
	if report != nil {
		result["publication"] = report
	}
	status := http.StatusOK
	if started {
		status = http.StatusAccepted
	}
	writeJSON(response, status, result)
}

func (s *Server) validateThemeTemplates(definition themepkg.Definition) error {
	if definition.TemplatesRoot == "" {
		return fmt.Errorf("主题未提供模板目录")
	}
	paths := make(map[string]struct{})
	if s.database != nil {
		assignments, err := templateconfig.List(context.Background(), s.database)
		if err != nil {
			return fmt.Errorf("读取模板绑定失败: %w", err)
		}
		for _, assignment := range assignments {
			paths[assignment.TemplatePath] = struct{}{}
		}
		var categoryTable int
		if err := s.database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'gocms_category'`).Scan(&categoryTable); err != nil {
			return fmt.Errorf("检查分类模板失败: %w", err)
		}
		if categoryTable > 0 {
			rows, err := s.database.Query(`SELECT DISTINCT "page_type", "list_template", "cover_template", "detail_template" FROM "gocms_category"`)
			if err != nil {
				return fmt.Errorf("读取分类模板失败: %w", err)
			}
			defer rows.Close()
			for rows.Next() {
				var pageType, listTemplate, coverTemplate, detailTemplate string
				if err := rows.Scan(&pageType, &listTemplate, &coverTemplate, &detailTemplate); err != nil {
					return fmt.Errorf("读取分类模板失败: %w", err)
				}
				if strings.EqualFold(strings.TrimSpace(pageType), routing.PageTypeCover) {
					if strings.TrimSpace(coverTemplate) != "" {
						paths[coverTemplate] = struct{}{}
					}
				} else if strings.TrimSpace(listTemplate) != "" {
					paths[listTemplate] = struct{}{}
				}
				if strings.TrimSpace(detailTemplate) != "" {
					paths[detailTemplate] = struct{}{}
				}
			}
			if err := rows.Err(); err != nil {
				return fmt.Errorf("读取分类模板失败: %w", err)
			}
		}
	}
	for templatePath := range paths {
		if _, _, err := readThemeFile(definition.TemplatesRoot, ".html", templatePath); err != nil {
			return fmt.Errorf("主题缺少模板 %q: %w", templatePath, err)
		}
	}
	return nil
}

func (s *Server) adminThemeImport(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(response)
		return
	}
	if !s.requireAdmin(response, request) {
		return
	}
	base, _, _ := s.themeState()
	if base == "" {
		http.Error(response, "主题目录未配置", http.StatusServiceUnavailable)
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, int64(themepkg.MaxImportArchiveSize)+1)
	if err := request.ParseMultipartForm(themepkg.MaxImportArchiveSize); err != nil {
		http.Error(response, "主题压缩包无效或过大", http.StatusBadRequest)
		return
	}
	file, _, err := request.FormFile("theme")
	if err != nil {
		file, _, err = request.FormFile("file")
	}
	if err != nil {
		http.Error(response, "请选择主题 ZIP 文件", http.StatusBadRequest)
		return
	}
	defer file.Close()
	definition, err := themepkg.ImportArchive(base, file)
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{"ok": true, "theme": definition.Info(false)})
}

func (s *Server) adminThemeExport(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(response)
		return
	}
	if !s.requireAdmin(response, request) {
		return
	}
	base, _, active := s.themeState()
	id := strings.TrimSpace(request.URL.Query().Get("id"))
	definition := active
	if id != "" {
		var err error
		definition, err = themepkg.Find(base, id)
		if err != nil {
			http.Error(response, err.Error(), http.StatusNotFound)
			return
		}
	}
	if definition.Root == "" {
		http.Error(response, "主题未配置", http.StatusServiceUnavailable)
		return
	}
	response.Header().Set("Content-Type", "application/zip")
	response.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=theme-%s.zip", definition.Manifest.ID))
	if err := themepkg.WriteArchive(response, definition); err != nil {
		return
	}
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
	_, templateRoot := s.themePaths()
	if templateRoot == "" {
		http.Error(response, "publishing is not configured", http.StatusServiceUnavailable)
		return
	}
	if _, _, err := readThemeFile(templateRoot, ".html", templatePath); err != nil {
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
	case strings.Contains(name, "detail"):
		return templateconfig.DimensionDetail
	default:
		return templateconfig.DimensionOther
	}
}

func (s *Server) themeFileRoot(kind string) (root, extension string, ok bool) {
	themeRoot, templateRoot := s.themePaths()
	switch kind {
	case "css":
		return themeRoot, ".css", themeRoot != ""
	case "template":
		return templateRoot, ".html", templateRoot != ""
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
