package site

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
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
	"gocms/internal/templatelabel"
	themepkg "gocms/internal/theme"
)

const maxThemeFileSize = 8 << 20

const maxThemeFileRequestSize = maxThemeFileSize + 1<<20

var themeImageExtensions = []string{".avif", ".gif", ".ico", ".jpeg", ".jpg", ".png", ".webp", ".svg"}

type ThemeFile struct {
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modified_at"`
}

type ThemeFilesResponse struct {
	Name                 string               `json:"name"`
	ActiveTheme          string               `json:"active_theme"`
	Themes               []themepkg.Info      `json:"themes"`
	TemplateGroupCatalog []themepkg.Info      `json:"template_groups_catalog"`
	CSSFiles             []ThemeFile          `json:"css_files"`
	JSFiles              []ThemeFile          `json:"js_files"`
	ImageFiles           []ThemeFile          `json:"image_files"`
	CustomFiles          ThemeCustomFiles     `json:"custom_files"`
	TemplateFiles        []ThemeFile          `json:"template_files"`
	TemplateGroups       []ThemeTemplateGroup `json:"template_groups"`
}

type ThemeCustomFiles struct {
	CSS    []ThemeFile `json:"css"`
	JS     []ThemeFile `json:"js"`
	Images []ThemeFile `json:"images"`
}

type ThemeTemplateGroup struct {
	Key         string                    `json:"key"`
	Label       string                    `json:"label"`
	Count       int64                     `json:"count"`
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
		root, extensions, ok := s.themeFileSpec(kind)
		if !ok {
			http.Error(response, "invalid template file kind", http.StatusBadRequest)
			return
		}
		file, content, err := readThemeFileAny(root, extensions, relativePath, kind != "image")
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
	jsFiles, err := listThemeFiles(themeRoot, ".js")
	if err != nil {
		http.Error(response, "读取模板 JS 失败", http.StatusInternalServerError)
		return
	}
	imageFiles, err := listThemeFilesAny(themeRoot, themeImageExtensions)
	if err != nil {
		http.Error(response, "读取模板图片失败", http.StatusInternalServerError)
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
	writeJSON(response, http.StatusOK, ThemeFilesResponse{
		Name:                 name,
		ActiveTheme:          active.Manifest.ID,
		Themes:               themes,
		TemplateGroupCatalog: themes,
		CSSFiles:             cssFiles,
		JSFiles:              jsFiles,
		ImageFiles:           imageFiles,
		CustomFiles:          ThemeCustomFiles{CSS: cssFiles, JS: jsFiles, Images: imageFiles},
		TemplateFiles:        templateFiles,
		TemplateGroups:       templateGroups,
	})
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
		s.publication.publisher.HomeTemplate = definition.HomeTemplate()
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
	paths[definition.HomeTemplate()] = struct{}{}
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

func (s *Server) adminThemeFiles(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost && request.Method != http.MethodPut && request.Method != http.MethodDelete {
		response.Header().Set("Allow", "POST, PUT, DELETE")
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireAdmin(response, request) {
		return
	}

	switch request.Method {
	case http.MethodPost:
		s.uploadThemeFile(response, request)
	case http.MethodPut:
		s.updateThemeFile(response, request)
	case http.MethodDelete:
		s.deleteThemeFile(response, request)
	}
}

func (s *Server) uploadThemeFile(response http.ResponseWriter, request *http.Request) {
	if !strings.HasPrefix(strings.ToLower(request.Header.Get("Content-Type")), "multipart/form-data;") {
		http.Error(response, "multipart upload is required", http.StatusBadRequest)
		return
	}
	root, _, ok := s.themeFileSpec("css")
	if !ok || root == "" {
		http.Error(response, "模板组资源目录未配置", http.StatusServiceUnavailable)
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxThemeFileRequestSize)
	file, header, err := request.FormFile("file")
	if err != nil {
		http.Error(response, "请选择模板文件", http.StatusBadRequest)
		return
	}
	defer file.Close()

	kind, err := normalizeCustomFileKind(request.FormValue("kind"))
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, maxThemeFileSize+1))
	if err != nil {
		http.Error(response, "读取模板文件失败", http.StatusBadRequest)
		return
	}
	if int64(len(data)) > maxThemeFileSize {
		http.Error(response, "模板文件不能超过 8 MB", http.StatusRequestEntityTooLarge)
		return
	}

	relativePath := strings.TrimSpace(request.FormValue("path"))
	if relativePath == "" {
		filename := pathpkg.Base(strings.ReplaceAll(header.Filename, `\`, "/"))
		if filename == "." || filename == "" {
			filename = "upload"
		}
		if kind == "image" {
			extension := strings.ToLower(filepath.Ext(filename))
			detected := detectedThemeImageExtension(data)
			if detected == "" {
				http.Error(response, "文件不是受支持的图片格式", http.StatusBadRequest)
				return
			}
			if extension == "" || extension == ".svg" || (extension == ".jpeg" && detected != ".jpg") || (extension != ".jpeg" && extension != detected) {
				filename, err = uniqueThemeFilename(detected)
				if err != nil {
					http.Error(response, "生成模板图片名称失败", http.StatusInternalServerError)
					return
				}
			}
		} else if filepath.Ext(filename) == "" {
			http.Error(response, "模板文件必须带有正确扩展名", http.StatusBadRequest)
			return
		}
		relativePath = filename
	}

	item, err := s.saveCustomThemeFile(kind, relativePath, data)
	if err != nil {
		writeThemeFileError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{"ok": true, "file": item})
}

func (s *Server) updateThemeFile(response http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(response, request.Body, maxThemeFileRequestSize)
	var payload struct {
		Kind    string `json:"kind"`
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := decodeRequest(request, &payload); err != nil {
		http.Error(response, "invalid template file payload", http.StatusBadRequest)
		return
	}
	kind, err := normalizeManagedFileKind(payload.Kind)
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	if kind == "image" {
		http.Error(response, "图片文件不能直接保存文本内容", http.StatusBadRequest)
		return
	}
	if int64(len([]byte(payload.Content))) > maxThemeFileSize {
		http.Error(response, "模板文件不能超过 8 MB", http.StatusRequestEntityTooLarge)
		return
	}
	item, err := s.saveCustomThemeFile(kind, payload.Path, []byte(payload.Content))
	if err != nil {
		writeThemeFileError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"ok": true, "file": item})
}

func (s *Server) deleteThemeFile(response http.ResponseWriter, request *http.Request) {
	kind, err := normalizeCustomFileKind(request.URL.Query().Get("kind"))
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	root, _, ok := s.themeFileSpec(kind)
	if !ok || root == "" {
		http.Error(response, "模板组资源目录未配置", http.StatusServiceUnavailable)
		return
	}
	relativePath, err := normalizeCustomFilePath(kind, request.URL.Query().Get("path"))
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	filePath, err := safeThemeFilePath(root, relativePath)
	if err != nil {
		writeThemeFileError(response, err)
		return
	}
	info, err := os.Lstat(filePath)
	if os.IsNotExist(err) {
		http.Error(response, "模板文件不存在", http.StatusNotFound)
		return
	}
	if err != nil {
		writeThemeFileError(response, err)
		return
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		writeThemeFileError(response, errInvalidThemeFile)
		return
	}
	if err := os.Remove(filePath); err != nil {
		writeThemeFileError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

func normalizeCustomFileKind(value string) (string, error) {
	kind, err := normalizeManagedFileKind(value)
	if err != nil {
		return "", err
	}
	if kind == "template" {
		return "", fmt.Errorf("模板文件不能通过自定义文件接口上传")
	}
	return kind, nil
}

func normalizeManagedFileKind(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "css":
		return "css", nil
	case "js", "javascript":
		return "js", nil
	case "image", "images", "img":
		return "image", nil
	case "template", "html":
		return "template", nil
	default:
		return "", fmt.Errorf("自定义文件类型无效")
	}
}

func normalizeCustomFilePath(kind, value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, `\`, "/"))
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, ":") {
		return "", errInvalidThemePath
	}
	clean := pathpkg.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errInvalidThemePath
	}
	if strings.HasPrefix(clean, "assets/") {
		return "", errInvalidThemePath
	}
	extension := strings.ToLower(filepath.Ext(clean))
	switch kind {
	case "css":
		if extension != ".css" {
			return "", invalidThemeFileInput("CSS 文件必须使用 .css 扩展名")
		}
		if strings.HasPrefix(clean, "images/") || strings.HasPrefix(clean, "js/") {
			return "", errInvalidThemePath
		}
		if !strings.HasPrefix(clean, "css/") && !strings.HasPrefix(clean, "skin/") {
			clean = pathpkg.Join("css", clean)
		}
	case "js":
		if extension != ".js" {
			return "", invalidThemeFileInput("JS 文件必须使用 .js 扩展名")
		}
		if strings.HasPrefix(clean, "images/") || strings.HasPrefix(clean, "css/") || strings.HasPrefix(clean, "skin/") {
			return "", errInvalidThemePath
		}
		if !strings.HasPrefix(clean, "js/") {
			clean = pathpkg.Join("js", clean)
		}
	case "image":
		if !containsExtension(themeImageExtensions, extension) {
			return "", invalidThemeFileInput("图片文件格式不受支持")
		}
		if strings.HasPrefix(clean, "css/") || strings.HasPrefix(clean, "js/") || strings.HasPrefix(clean, "skin/") {
			return "", errInvalidThemePath
		}
		if !strings.HasPrefix(clean, "images/") {
			clean = pathpkg.Join("images", clean)
		}
	case "template":
		if extension != ".html" {
			return "", invalidThemeFileInput("模板文件必须使用 .html 扩展名")
		}
	}
	return clean, nil
}

func (s *Server) saveCustomThemeFile(kind, relativePath string, data []byte) (ThemeFile, error) {
	root, _, ok := s.themeFileSpec(kind)
	if !ok || root == "" {
		return ThemeFile{}, fmt.Errorf("模板组资源目录未配置")
	}
	relativePath, err := normalizeCustomFilePath(kind, relativePath)
	if err != nil {
		return ThemeFile{}, err
	}
	if kind == "css" || kind == "js" || kind == "template" {
		if !utf8.Valid(data) {
			return ThemeFile{}, invalidThemeFileInput("模板文件必须是 UTF-8 文本")
		}
	} else if err := validateThemeImage(data, filepath.Ext(relativePath)); err != nil {
		return ThemeFile{}, err
	}
	filePath, err := safeThemeFilePath(root, relativePath)
	if err != nil {
		return ThemeFile{}, err
	}
	if err := writeThemeFile(filePath, data); err != nil {
		return ThemeFile{}, err
	}
	return statThemeFile(root, relativePath)
}

func safeThemeFilePath(root, relativePath string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("模板组资源目录未配置")
	}
	root, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", errInvalidThemePath
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		return "", err
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return "", errInvalidThemePath
	}
	clean := filepath.Clean(filepath.FromSlash(relativePath))
	filePath := filepath.Join(root, clean)
	relative, err := filepath.Rel(root, filePath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errInvalidThemePath
	}
	current := root
	parts := strings.Split(filepath.ToSlash(clean), "/")
	for index, part := range parts {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			break
		}
		if statErr != nil {
			return "", statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errInvalidThemePath
		}
		if index < len(parts)-1 && !info.IsDir() {
			return "", errInvalidThemePath
		}
		if index == len(parts)-1 && info.IsDir() {
			return "", errInvalidThemeFile
		}
	}
	return filePath, nil
}

func writeThemeFile(filePath string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(filePath), ".gocms-template-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0644); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, filePath)
}

func statThemeFile(root, relativePath string) (ThemeFile, error) {
	filePath, err := safeThemeFilePath(root, relativePath)
	if err != nil {
		return ThemeFile{}, err
	}
	info, err := os.Lstat(filePath)
	if err != nil {
		return ThemeFile{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return ThemeFile{}, errInvalidThemeFile
	}
	return ThemeFile{Path: filepath.ToSlash(relativePath), Size: info.Size(), ModifiedAt: info.ModTime().UTC().Format(time.RFC3339)}, nil
}

func uniqueThemeFilename(extension string) (string, error) {
	randomName := make([]byte, 12)
	if _, err := rand.Read(randomName); err != nil {
		return "", err
	}
	return "upload-" + hex.EncodeToString(randomName) + extension, nil
}

func containsExtension(extensions []string, extension string) bool {
	for _, item := range extensions {
		if strings.EqualFold(item, extension) {
			return true
		}
	}
	return false
}

func detectedThemeImageExtension(data []byte) string {
	_, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err == nil {
		switch strings.ToLower(format) {
		case "jpeg":
			return ".jpg"
		case "png":
			return ".png"
		case "gif":
			return ".gif"
		}
	}
	if len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return ".webp"
	}
	if len(data) >= 4 && data[0] == 0 && data[1] == 0 && data[2] == 1 && data[3] == 0 {
		return ".ico"
	}
	if len(data) >= 12 && bytes.Equal(data[4:8], []byte("ftyp")) && bytes.Contains(data[8:12], []byte("avif")) {
		return ".avif"
	}
	return ""
}

func validateThemeImage(data []byte, extension string) error {
	detected := detectedThemeImageExtension(data)
	if strings.EqualFold(extension, ".jpeg") {
		extension = ".jpg"
	}
	if detected == "" || !strings.EqualFold(detected, extension) {
		return invalidThemeFileInput("图片内容与扩展名不匹配")
	}
	return nil
}

func writeThemeFileError(response http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, errInvalidThemePath) || errors.Is(err, errInvalidThemeFile) || errors.Is(err, errInvalidThemeInput) || os.IsNotExist(err) {
		status = http.StatusBadRequest
		if os.IsNotExist(err) {
			status = http.StatusNotFound
		}
	}
	http.Error(response, err.Error(), status)
}

func (s *Server) themeTemplateGroups(files []ThemeFile) []ThemeTemplateGroup {
	_, _, active := s.themeState()
	assignments, _ := templateconfig.List(context.Background(), s.database)
	labelCount := int64(0)
	if s.database != nil {
		labelCount, _ = templatelabel.Count(context.Background(), s.database)
	}
	order := themepkg.TemplateGroupKeys()
	groups := make(map[string]*ThemeTemplateGroup, len(order))
	for _, groupKey := range order {
		groups[groupKey] = &ThemeTemplateGroup{Key: groupKey, Label: themepkg.TemplateGroupLabel(groupKey), Files: []ThemeFile{}, Assignments: []ThemeTemplateAssignment{}}
	}
	for _, file := range files {
		groupKey := active.TemplateGroupFor(file.Path)
		if groupKey == themepkg.TemplateGroupOther {
			groupKey = themepkg.TemplateGroupPublic
		}
		group, ok := groups[groupKey]
		if !ok {
			continue
		}
		group.Files = append(group.Files, file)
	}
	for _, item := range assignments {
		groupKey := templateGroupForDimension(item.Dimension)
		if groupKey == themepkg.TemplateGroupHome {
			continue
		}
		group, ok := groups[groupKey]
		if !ok {
			continue
		}
		group.Assignments = append(group.Assignments, ThemeTemplateAssignment{
			Key: item.Key, Label: item.Label, Dimension: item.Dimension, DimensionName: item.DimensionName,
			TemplatePath: item.TemplatePath, Available: availableTemplate(files, item.TemplatePath),
		})
	}
	result := make([]ThemeTemplateGroup, 0, len(order))
	for _, groupKey := range order {
		group := groups[groupKey]
		if groupKey == themepkg.TemplateGroupLabelTemplates {
			group.Count = labelCount
		} else {
			group.Count = int64(len(group.Files))
		}
		if len(group.Files) == 0 && len(group.Assignments) == 0 && group.Count == 0 {
			continue
		}
		result = append(result, *group)
	}
	return result
}

func templateGroupForDimension(dimension string) string {
	switch strings.ToLower(strings.TrimSpace(dimension)) {
	case templateconfig.DimensionHome:
		return themepkg.TemplateGroupHome
	case templateconfig.DimensionList:
		return themepkg.TemplateGroupList
	case templateconfig.DimensionDetail:
		return themepkg.TemplateGroupContent
	default:
		return themepkg.TemplateGroupPublic
	}
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

func (s *Server) themeFileSpec(kind string) (root string, extensions []string, ok bool) {
	themeRoot, templateRoot := s.themePaths()
	switch kind {
	case "css":
		return themeRoot, []string{".css"}, themeRoot != ""
	case "js":
		return themeRoot, []string{".js"}, themeRoot != ""
	case "image":
		return themeRoot, themeImageExtensions, themeRoot != ""
	case "template":
		return templateRoot, []string{".html"}, templateRoot != ""
	default:
		return "", nil, false
	}
}

func (s *Server) themeFileRoot(kind string) (root, extension string, ok bool) {
	root, extensions, ok := s.themeFileSpec(kind)
	if len(extensions) == 1 {
		return root, extensions[0], ok
	}
	return root, "", ok
}

var errInvalidThemePath = fmt.Errorf("invalid theme file path")
var errInvalidThemeFile = fmt.Errorf("invalid theme file")
var errInvalidThemeInput = fmt.Errorf("invalid theme file input")

func invalidThemeFileInput(message string) error {
	return fmt.Errorf("%w: %s", errInvalidThemeInput, message)
}

func listThemeFiles(root, extension string) ([]ThemeFile, error) {
	return listThemeFilesAny(root, []string{extension})
}

func listThemeFilesAny(root string, extensions []string) ([]ThemeFile, error) {
	if root == "" {
		return []ThemeFile{}, nil
	}
	allowed := make(map[string]struct{}, len(extensions))
	for _, extension := range extensions {
		allowed[strings.ToLower(extension)] = struct{}{}
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
		if entry.IsDir() {
			return nil
		}
		if _, ok := allowed[strings.ToLower(filepath.Ext(entry.Name()))]; !ok {
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
	if os.IsNotExist(err) {
		return []ThemeFile{}, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func readThemeFile(root, extension, relative string) (ThemeFile, string, error) {
	return readThemeFileAny(root, []string{extension}, relative, true)
}

func readThemeFileAny(root string, extensions []string, relative string, text bool) (ThemeFile, string, error) {
	relative = strings.TrimSpace(strings.ReplaceAll(relative, `\`, "/"))
	if relative == "" || strings.HasPrefix(relative, "/") {
		return ThemeFile{}, "", errInvalidThemePath
	}
	clean := pathpkg.Clean(relative)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return ThemeFile{}, "", errInvalidThemePath
	}
	allowed := make(map[string]struct{}, len(extensions))
	for _, extension := range extensions {
		allowed[strings.ToLower(extension)] = struct{}{}
	}
	if _, ok := allowed[strings.ToLower(filepath.Ext(clean))]; !ok {
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
	if text && !utf8.Valid(content) {
		return ThemeFile{}, "", fmt.Errorf("theme file is not UTF-8")
	}
	if !text {
		return ThemeFile{Path: clean, Size: info.Size(), ModifiedAt: info.ModTime().UTC().Format(time.RFC3339)}, "", nil
	}
	return ThemeFile{Path: clean, Size: info.Size(), ModifiedAt: info.ModTime().UTC().Format(time.RFC3339)}, string(content), nil
}
