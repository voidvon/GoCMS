package generator

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"
	"unicode/utf8"

	"gocms/internal/db"
	"gocms/internal/routing"
	"gocms/internal/templateconfig"
)

type Row map[string]string

func (r Row) n(key string) int {
	value, _ := strconv.Atoi(r[key])
	return value
}

type Report struct {
	State    string    `json:"state"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
	Files    int       `json:"files"`
	Contents int       `json:"contents"`
	Error    string    `json:"error,omitempty"`
}

type Publisher struct {
	DB                   *sql.DB
	Web, Templates, Data string
	Assets               string
	Theme                string
}

type content struct {
	tables      map[string][]Row
	settings    map[string]string
	templates   map[string]*template.Template
	assignments map[string]string
	pages       map[string][]byte
}

// ListItem, ListCategory, ListPagination, and NavigationItem are the data
// contract exposed to themes. They intentionally contain no markup.
type ListItem struct {
	URL         string
	Title       string
	Summary     string
	Excerpt     string
	PublishedAt string
	Date        string
	Image       string
	Category    string
	RowStart    bool
	RowEnd      bool
}

type ListCategory struct {
	URL      string
	Name     string
	Current  bool
	Last     bool
	RowStart bool
	RowEnd   bool
}

type NavigationItem struct {
	URL      string
	Name     string
	Children []NavigationItem
}

type ListPage struct {
	Number  int
	URL     string
	Current bool
}

type ListPagination struct {
	Total       int
	Page        int
	Pages       int
	PageSize    int
	FirstURL    string
	PreviousURL string
	NextURL     string
	LastURL     string
	HasPrevious bool
	HasNext     bool
	PageLinks   []ListPage
}

func (c *content) templateFuncs() template.FuncMap {
	return template.FuncMap{
		"setting":           func(key string) string { return esc(c.settings[strings.TrimSpace(key)]) },
		"settingHTML":       func(key string) string { return c.settings[strings.TrimSpace(key)] },
		"include":           func(path string, row Row) (string, error) { return c.include(path, row) },
		"listItems":         func(row Row) []ListItem { return c.listItems(row) },
		"listCategories":    func(row Row) []ListCategory { return c.listCategories(row) },
		"listChildren":      func(row Row) []ListCategory { return c.listChildren(row) },
		"catalogCategories": func(_ Row) []ListCategory { return c.catalogCategories() },
		"navigation":        func(_ Row) []NavigationItem { return c.navigation() },
		"featuredItems":     func(limit int) []ListItem { return c.featuredItems(limit) },
		"featuredItemsIn": func(collection string, limit int) []ListItem {
			return c.featuredItemsIn(collection, limit)
		},
		"listPagination": func(row Row) ListPagination {
			return c.listPagination(row)
		},
	}
}

func esc(value string) string { return html.EscapeString(value) }

func readTable(ctx context.Context, tx *sql.Tx, name string) ([]Row, error) {
	rows, err := tx.QueryContext(ctx, `SELECT * FROM "`+name+`"`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	items := make([]Row, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, err
		}
		row := Row{}
		for index, column := range columns {
			if values[index] == nil {
				row[strings.ToLower(column)] = ""
				continue
			}
			if value, ok := values[index].([]byte); ok {
				row[strings.ToLower(column)] = string(value)
				continue
			}
			row[strings.ToLower(column)] = fmt.Sprint(values[index])
		}
		items = append(items, row)
	}
	return items, rows.Err()
}

func (p Publisher) Generate(ctx context.Context) (report Report, err error) {
	report = Report{State: "running", Started: time.Now()}
	if err = os.MkdirAll(p.Data, 0755); err != nil {
		return report, err
	}
	lock, err := os.OpenFile(filepath.Join(p.Data, "publish.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return report, err
	}
	defer lock.Close()
	unlock, err := acquirePublishLock(lock)
	if err != nil {
		return report, ErrPublishBusy
	}
	defer unlock()
	defer func() {
		report.Finished = time.Now()
		report.State = "success"
		if err != nil {
			report.State = "failed"
			report.Error = err.Error()
		}
		data, _ := json.MarshalIndent(report, "", "  ")
		_ = atomicWrite(filepath.Join(p.Data, "publish.json"), data)
	}()

	c := &content{
		tables:      map[string][]Row{},
		settings:    map[string]string{},
		templates:   map[string]*template.Template{},
		assignments: map[string]string{},
		pages:       map[string][]byte{},
	}
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return report, err
	}
	for _, name := range []string{"gocms_category", "gocms_content"} {
		rows, readErr := readTable(ctx, tx, name)
		if readErr != nil {
			_ = tx.Rollback()
			return report, readErr
		}
		c.tables[name] = rows
	}
	c.settings, err = db.LoadSiteSettings(ctx, tx)
	if err != nil {
		_ = tx.Rollback()
		return report, err
	}
	c.assignments, err = templateconfig.Load(ctx, tx)
	if err != nil {
		_ = tx.Rollback()
		return report, err
	}
	if err = tx.Commit(); err != nil {
		return report, err
	}

	sortRows(c.tables["gocms_category"], "order_id", true)
	sortRows(c.tables["gocms_content"], "sort_order", false)
	err = filepath.WalkDir(p.Templates, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".html") {
			return nil
		}
		relative, err := filepath.Rel(p.Templates, filePath)
		if err != nil {
			return err
		}
		templatePath := filepath.ToSlash(relative)
		data, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		if !utf8.Valid(data) {
			return fmt.Errorf("模板非 UTF-8: %s", templatePath)
		}
		parsed, err := template.New(templatePath).Funcs(c.templateFuncs()).Parse(string(data))
		if err != nil {
			return fmt.Errorf("解析模板 %s: %w", templatePath, err)
		}
		c.templates[templatePath] = parsed
		return nil
	})
	if err != nil {
		return report, err
	}
	if err = c.build(); err != nil {
		return report, err
	}
	for _, row := range c.tables["gocms_content"] {
		if row.n("visible") == 1 {
			report.Contents++
		}
	}
	report.Files = len(c.pages)

	web, err := filepath.Abs(p.Web)
	if err != nil {
		return report, err
	}
	if web == filepath.Dir(web) {
		return report, fmt.Errorf("invalid web root")
	}
	stage, err := os.MkdirTemp(filepath.Dir(web), ".web-stage-")
	if err != nil {
		return report, err
	}
	defer os.RemoveAll(stage)
	if err = c.normalizeLinks(p.Assets, p.Theme); err != nil {
		return report, err
	}
	for relative, data := range c.pages {
		if err = ctx.Err(); err != nil {
			return report, err
		}
		if err = atomicWrite(filepath.Join(stage, filepath.FromSlash(relative)), data); err != nil {
			return report, err
		}
	}

	backup := web + ".previous"
	if _, err = os.Stat(backup); err == nil {
		return report, fmt.Errorf("上次发布备份仍存在: %s，请先检查恢复", backup)
	}
	hadWeb := false
	if _, err = os.Stat(web); err == nil {
		if err = os.Rename(web, backup); err != nil {
			return report, err
		}
		hadWeb = true
	}
	if err = os.Rename(stage, web); err != nil {
		if hadWeb {
			_ = os.Rename(backup, web)
		}
		return report, err
	}
	if hadWeb {
		_ = os.RemoveAll(backup)
	}
	return report, nil
}

func sortRows(rows []Row, orderField string, category bool) {
	sort.SliceStable(rows, func(left, right int) bool {
		a, b := rows[left], rows[right]
		if a.n(orderField) == b.n(orderField) {
			if category {
				return a.n("id") < b.n("id")
			}
			return a.n("id") < b.n("id")
		}
		return a.n(orderField) < b.n(orderField)
	})
}

func atomicWrite(filePath string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(filePath), ".publish-")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err = temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err = temporary.Chmod(0644); err != nil {
		_ = temporary.Close()
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), filePath)
}

func (c *content) page(rolePath, role string, row Row) error {
	templatePath, ok := c.assignments[role]
	if !ok {
		return fmt.Errorf("缺少模板配置: %s", role)
	}
	return c.pageTemplate(rolePath, templatePath, row)
}

func (c *content) pageTemplate(pagePath, templatePath string, row Row) error {
	normalized, err := templateconfig.NormalizePath(templatePath)
	if err != nil {
		return fmt.Errorf("模板路径无效: %s: %w", templatePath, err)
	}
	parsed, ok := c.templates[normalized]
	if !ok {
		return fmt.Errorf("缺少模板: %s", normalized)
	}
	var output strings.Builder
	if err := parsed.Execute(&output, row); err != nil {
		return fmt.Errorf("生成 %s: %w", pagePath, err)
	}
	if !utf8.ValidString(output.String()) {
		return fmt.Errorf("生成页面非 UTF-8: %s", pagePath)
	}
	c.pages[pagePath] = []byte(output.String())
	return nil
}

func (c *content) include(templatePath string, row Row) (string, error) {
	normalized, err := templateconfig.NormalizePath(templatePath)
	if err != nil {
		return "", err
	}
	parsed, ok := c.templates[normalized]
	if !ok {
		return "", fmt.Errorf("缺少模板: %s", normalized)
	}
	var output strings.Builder
	if err := parsed.Execute(&output, row); err != nil {
		return "", err
	}
	return output.String(), nil
}

func (c *content) cat(id int) Row {
	for _, category := range c.tables["gocms_category"] {
		if category.n("id") == id {
			return category
		}
	}
	return Row{}
}

func (c *content) under(id, root int) bool {
	seen := map[int]bool{}
	for id > 0 && !seen[id] {
		if id == root {
			return true
		}
		seen[id] = true
		id = c.cat(id).n("parent_id")
	}
	return false
}

func (c *content) categoryListDir(row Row) string {
	directory := strings.TrimSpace(row["list_path"])
	if directory == "" {
		if c.categoryPageType(row) == routing.PageTypeCover {
			return ""
		}
		directory = routing.DefaultListPath()
	}
	if normalized, err := routing.NormalizeOptionalDirectory(directory); err == nil {
		return normalized
	}
	return routing.DefaultListPath()
}

func (c *content) categoryListPattern(row Row) string {
	pattern := strings.TrimSpace(row["list_file_pattern"])
	if pattern == "" {
		if c.categoryPageType(row) == routing.PageTypeCover {
			return routing.DefaultCoverPattern
		}
		return routing.DefaultListPattern
	}
	if c.categoryPageType(row) == routing.PageTypeCover {
		if normalized, err := routing.NormalizeCoverFilePattern(pattern); err == nil {
			return normalized
		}
		return routing.DefaultCoverPattern
	}
	if normalized, err := routing.NormalizeFilePattern(pattern, false); err == nil {
		return normalized
	}
	return routing.DefaultListPattern
}

func (c *content) categoryPageType(row Row) string {
	if value, err := routing.NormalizePageType(row["page_type"]); err == nil {
		return value
	}
	return routing.PageTypeList
}

func (c *content) categoryDetailDir(row Row) string {
	directory := strings.TrimSpace(row["detail_path"])
	if directory == "" {
		directory = routing.DefaultDetailPath
	}
	if normalized, err := routing.NormalizeDirectory(directory); err == nil {
		return normalized
	}
	return routing.DefaultDetailPath
}

func (c *content) categoryDetailPattern(row Row) string {
	pattern := strings.TrimSpace(row["detail_file_pattern"])
	if pattern == "" {
		pattern = routing.DefaultDetailPattern
	}
	if normalized, err := routing.NormalizeFilePattern(pattern, false); err == nil {
		return normalized
	}
	return routing.DefaultDetailPattern
}

func (c *content) categoryListURL(row Row, page int) string {
	if row["id"] == "" {
		return ""
	}
	routeID := row.n("route_id")
	if routeID == 0 {
		routeID = row.n("id")
	}
	var filename string
	var err error
	if c.categoryPageType(row) == routing.PageTypeCover {
		filename, err = routing.RenderCoverFilename(c.categoryListPattern(row), int64(routeID))
	} else {
		filename, err = routing.RenderListFilename(c.categoryListPattern(row), int64(routeID), page)
	}
	if err != nil {
		filename = strconv.Itoa(routeID) + ".html"
	}
	directory := c.categoryListDir(row)
	if c.categoryPageType(row) == routing.PageTypeCover && isIndexFilename(filename) {
		if directory == "" {
			return "/"
		}
		return "/" + strings.Trim(directory, "/") + "/"
	}
	return "/" + strings.Trim(directory+"/"+filename, "/")
}

func (c *content) categoryListPagePath(row Row, page int) string {
	if row["id"] == "" {
		return ""
	}
	routeID := row.n("route_id")
	if routeID == 0 {
		routeID = row.n("id")
	}
	var filename string
	var err error
	if c.categoryPageType(row) == routing.PageTypeCover {
		filename, err = routing.RenderCoverFilename(c.categoryListPattern(row), int64(routeID))
	} else {
		filename, err = routing.RenderListPageFilename(c.categoryListPattern(row), int64(routeID), page)
	}
	if err != nil {
		filename = fmt.Sprintf("%d-%d.html", routeID, page)
	}
	return strings.Trim(c.categoryListDir(row)+"/"+filename, "/")
}

func isIndexFilename(value string) bool {
	lower := strings.ToLower(value)
	return lower == "index.html" || lower == "index.htm"
}

func (c *content) contentURL(row Row) string {
	category := c.cat(row.n("category_id"))
	filename, err := routing.RenderDetailFilenameValue(c.categoryDetailPattern(category), row["route_key"])
	if err != nil {
		filename = row["route_key"] + ".html"
	}
	return "/" + strings.Trim(c.categoryDetailDir(category)+"/"+filename, "/")
}

func (c *content) listRoot(category Row) Row {
	current := category
	seen := map[int]bool{}
	for current["id"] != "" && current.n("parent_id") != 0 && !seen[current.n("id")] {
		seen[current.n("id")] = true
		parent := c.cat(current.n("parent_id"))
		if parent["id"] == "" {
			break
		}
		current = parent
	}
	return current
}

func (c *content) rootCategory() Row {
	var root Row
	for _, category := range c.tables["gocms_category"] {
		if category.n("parent_id") != 0 {
			continue
		}
		if root["id"] == "" || category.n("order_id") < root.n("order_id") ||
			(category.n("order_id") == root.n("order_id") && category.n("id") < root.n("id")) {
			root = category
		}
	}
	return root
}

func (c *content) rootCategoryURL() string {
	root := c.rootCategory()
	if root["id"] == "" {
		return "/" + routing.DefaultListPath() + "/"
	}
	directory := c.categoryListDir(root)
	if directory == "" {
		return "/"
	}
	return "/" + strings.Trim(directory, "/") + "/"
}

func (c *content) categoryCollectionKey(category Row) string {
	root := c.listRoot(category)
	value := strings.Trim(strings.TrimSpace(root["list_path"]), "/")
	if value == "" {
		value = strings.Trim(strings.TrimSpace(root["detail_path"]), "/")
	}
	if slash := strings.IndexByte(value, '/'); slash >= 0 {
		value = value[:slash]
	}
	return strings.ToLower(value)
}

func (c *content) categoryPageSize(category Row) int {
	pageSize := category.n("list_page_size")
	if pageSize < 1 {
		pageSize = 14
	}
	return pageSize
}

func (c *content) visibleContents() []Row {
	items := make([]Row, 0, len(c.tables["gocms_content"]))
	for _, item := range c.tables["gocms_content"] {
		if item.n("visible") == 1 {
			items = append(items, item)
		}
	}
	sortRows(items, "sort_order", false)
	return items
}

func (c *content) contentsForCategory(category Row, visible []Row) []Row {
	items := make([]Row, 0)
	for _, item := range visible {
		if c.under(item.n("category_id"), category.n("id")) {
			items = append(items, item)
		}
	}
	return items
}

func (c *content) listContext(view Row) (category Row, pageItems []Row, page, pages, pageSize, total int) {
	category = c.cat(view.n("category_id"))
	allItems := c.contentsForCategory(category, c.visibleContents())
	total = len(allItems)
	pageSize = view.n("list_page_size")
	if pageSize < 1 {
		pageSize = c.categoryPageSize(category)
	}
	pages = max(1, (total+pageSize-1)/pageSize)
	page = max(1, view.n("list_page"))
	page = min(page, pages)
	start := min((page-1)*pageSize, total)
	end := min(start+pageSize, total)
	return category, allItems[start:end], page, pages, pageSize, total
}

func (c *content) listItems(view Row) []ListItem {
	category, rows, _, _, _, _ := c.listContext(view)
	items := make([]ListItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, c.listItem(row, category, len(items), len(rows)))
	}
	return items
}

func (c *content) listItem(row, category Row, index, total int) ListItem {
	publishedAt := strings.TrimSpace(row["published_at"])
	summary := strings.TrimSpace(row["summary"])
	image := strings.TrimSpace(row["cover_image"])
	return ListItem{
		URL:         esc(c.contentURL(row)),
		Title:       esc(row["title"]),
		Summary:     esc(summary),
		Excerpt:     esc(excerpt(summary, 230)),
		PublishedAt: esc(publishedAt),
		Date:        esc(listDate(publishedAt)),
		Image:       esc(image),
		Category:    esc(category["name"]),
		RowStart:    index%2 == 0,
		RowEnd:      index%2 == 1 || index+1 == total,
	}
}

func (c *content) listCategories(view Row) []ListCategory {
	category := c.cat(view.n("category_id"))
	root := c.listRoot(category)
	children := c.children(root.n("id"))
	items := make([]ListCategory, 0, len(children))
	for index, child := range children {
		items = append(items, ListCategory{
			URL:     esc(c.categoryListURL(child, 1)),
			Name:    esc(child["name"]),
			Current: child.n("id") == category.n("id"),
			Last:    index+1 == len(children),
		})
	}
	return items
}

func (c *content) listChildren(view Row) []ListCategory {
	category := c.cat(view.n("category_id"))
	children := c.children(category.n("id"))
	items := make([]ListCategory, 0, len(children))
	for index, child := range children {
		items = append(items, ListCategory{
			URL:      esc(c.categoryListURL(child, 1)),
			Name:     esc(child["name"]),
			Last:     index+1 == len(children),
			RowStart: index%5 == 0,
			RowEnd:   index%5 == 4 || index+1 == len(children),
		})
	}
	return items
}

func (c *content) catalogCategories() []ListCategory {
	collection := c.categoryCollectionKey(c.rootCategory())
	items := make([]ListCategory, 0)
	for _, category := range c.tables["gocms_category"] {
		if category.n("parent_id") != 0 || c.categoryCollectionKey(category) != collection {
			continue
		}
		items = append(items, ListCategory{
			URL:  esc(c.categoryListURL(category, 1)),
			Name: esc(category["name"]),
		})
	}
	for index := range items {
		items[index].Last = index+1 == len(items)
	}
	return items
}

func (c *content) children(parentID int) []Row {
	children := make([]Row, 0)
	for _, category := range c.tables["gocms_category"] {
		if category.n("parent_id") == parentID {
			children = append(children, category)
		}
	}
	sortRows(children, "order_id", true)
	return children
}

func (c *content) navigation() []NavigationItem {
	items := make([]NavigationItem, 0)
	for _, category := range c.children(0) {
		items = append(items, c.navigationItem(category))
	}
	return items
}

func (c *content) navigationItem(category Row) NavigationItem {
	children := c.children(category.n("id"))
	items := make([]NavigationItem, 0, len(children))
	for _, child := range children {
		items = append(items, c.navigationItem(child))
	}
	return NavigationItem{URL: esc(c.categoryListURL(category, 1)), Name: esc(category["name"]), Children: items}
}

func (c *content) featuredItems(limit int) []ListItem {
	return c.featuredItemsIn("", limit)
}

func (c *content) featuredItemsIn(collection string, limit int) []ListItem {
	if limit < 1 {
		return []ListItem{}
	}
	collection = strings.ToLower(strings.Trim(strings.TrimSpace(collection), "/"))
	items := make([]Row, 0)
	for _, item := range c.tables["gocms_content"] {
		if item.n("visible") != 1 || item.n("featured") != 1 {
			continue
		}
		if collection != "" && c.categoryCollectionKey(c.cat(item.n("category_id"))) != collection {
			continue
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(left, right int) bool {
		if items[left].n("sort_order") == items[right].n("sort_order") {
			return items[left].n("id") > items[right].n("id")
		}
		return items[left].n("sort_order") < items[right].n("sort_order")
	})
	items = items[:min(limit, len(items))]
	result := make([]ListItem, 0, len(items))
	for index, item := range items {
		result = append(result, c.listItem(item, c.cat(item.n("category_id")), index, len(items)))
	}
	return result
}

func (c *content) listPagination(view Row) ListPagination {
	category, _, page, pages, pageSize, total := c.listContext(view)
	pagination := ListPagination{
		Total:       total,
		Page:        page,
		Pages:       pages,
		PageSize:    pageSize,
		HasPrevious: page > 1,
		HasNext:     page < pages,
		PageLinks:   make([]ListPage, 0, pages),
	}
	if category["id"] == "" {
		return pagination
	}
	pagination.FirstURL = esc(c.categoryListURL(category, 1))
	pagination.PreviousURL = esc(c.categoryListURL(category, max(1, page-1)))
	pagination.NextURL = esc(c.categoryListURL(category, min(pages, page+1)))
	pagination.LastURL = esc(c.categoryListURL(category, pages))
	for number := 1; number <= pages; number++ {
		pagination.PageLinks = append(pagination.PageLinks, ListPage{
			Number:  number,
			URL:     esc(c.categoryListURL(category, number)),
			Current: number == page,
		})
	}
	return pagination
}

func listDate(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 10 {
		return value[:10]
	}
	return value
}

func excerpt(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" || limit < 1 {
		return value
	}
	width := 0
	runes := []rune(value)
	for index, char := range runes {
		if char > 255 {
			width += 2
		} else {
			width++
		}
		if width >= limit {
			return string(runes[:index+1])
		}
	}
	return value
}

func (c *content) categoryListTemplate(category Row) string {
	if value := strings.TrimSpace(category["list_template"]); value != "" {
		return value
	}
	return templateconfig.DefaultListTemplate
}

func (c *content) categoryCoverTemplate(category Row) string {
	if value := strings.TrimSpace(category["cover_template"]); value != "" {
		return value
	}
	return templateconfig.DefaultCoverTemplate
}

func (c *content) categoryDetailTemplate(category Row) string {
	if value := strings.TrimSpace(category["detail_template"]); value != "" {
		return value
	}
	return templateconfig.DefaultDetailTemplate
}

func (c *content) contentView(row Row) Row {
	category := c.cat(row.n("category_id"))
	image := strings.TrimSpace(row["cover_image"])
	return Row{
		"id":                esc(row["id"]),
		"route_key":         esc(row["route_key"]),
		"title":             esc(row["title"]),
		"body":              row["body"],
		"content":           row["body"],
		"code":              esc(row["code"]),
		"summary":           esc(row["summary"]),
		"cover_image":       esc(image),
		"image":             esc(image),
		"published_at":      esc(row["published_at"]),
		"date":              esc(row["published_at"]),
		"source":            esc(row["source"]),
		"keywords":          esc(row["keywords"]),
		"description":       esc(row["description"]),
		"category_id":       esc(category["id"]),
		"category_name":     esc(category["name"]),
		"category_url":      c.categoryListURL(category, 1),
		"content_url":       c.contentURL(row),
		"category_children": "",
	}
}

func (c *content) categoryView(category Row, items []Row, page, pageSize int) Row {
	parent := c.cat(category.n("parent_id"))
	root := c.listRoot(category)
	rootName := root["name"]
	if rootName == "" {
		rootName = category["name"]
	}
	return Row{
		"title":             esc(category["name"]),
		"category_name":     esc(category["name"]),
		"category_id":       esc(category["id"]),
		"category_url":      c.categoryListURL(category, 1),
		"parent_name":       esc(parent["name"]),
		"parent_url":        c.categoryListURL(parent, 1),
		"page_type":         c.categoryPageType(category),
		"cover_template":    esc(category["cover_template"]),
		"list_page":         strconv.Itoa(page),
		"list_page_size":    strconv.Itoa(pageSize),
		"list_root_id":      strconv.Itoa(root.n("id")),
		"list_root_name":    esc(rootName),
		"list_root_url":     c.categoryListURL(root, 1),
		"root_category_url": c.rootCategoryURL(),
		"keywords":          esc(category["keywords"]),
		"description":       esc(category["description"]),
		"body":              category["cover_content"],
		"content":           category["cover_content"],
		"content_count":     strconv.Itoa(len(items)),
		"content_page_size": strconv.Itoa(pageSize),
	}
}

func (c *content) build() error {
	if err := c.page("index.html", templateconfig.RoleHomeIndex, Row{
		"title":         esc(c.settings["site_name"]),
		"site_name":     esc(c.settings["site_name"]),
		"site_url":      esc(c.settings["site_url"]),
		"root_category": "",
	}); err != nil {
		return err
	}

	visible := c.visibleContents()
	for _, item := range visible {
		category := c.cat(item.n("category_id"))
		view := c.contentView(item)
		view["previous_url"], view["previous_title"] = "", ""
		view["next_url"], view["next_title"] = "", ""
		sameCategory := make([]Row, 0)
		for _, candidate := range visible {
			if candidate.n("category_id") == item.n("category_id") {
				sameCategory = append(sameCategory, candidate)
			}
		}
		for index, candidate := range sameCategory {
			if candidate.n("id") != item.n("id") {
				continue
			}
			if index > 0 {
				view["previous_url"] = esc(c.contentURL(sameCategory[index-1]))
				view["previous_title"] = esc(sameCategory[index-1]["title"])
			}
			if index+1 < len(sameCategory) {
				view["next_url"] = esc(c.contentURL(sameCategory[index+1]))
				view["next_title"] = esc(sameCategory[index+1]["title"])
			}
		}
		if err := c.pageTemplate(strings.TrimPrefix(c.contentURL(item), "/"), c.categoryDetailTemplate(category), view); err != nil {
			return err
		}
	}

	for _, category := range c.tables["gocms_category"] {
		items := c.contentsForCategory(category, visible)
		pageSize := c.categoryPageSize(category)
		if c.categoryPageType(category) == routing.PageTypeCover {
			view := c.categoryView(category, items, 1, pageSize)
			pagePath := c.categoryListPagePath(category, 1)
			if err := c.pageTemplate(pagePath, c.categoryCoverTemplate(category), view); err != nil {
				return err
			}
			continue
		}
		pages := max(1, (len(items)+pageSize-1)/pageSize)
		for page := 1; page <= pages; page++ {
			view := c.categoryView(category, items, page, pageSize)
			pagePath := c.categoryListPagePath(category, page)
			if err := c.pageTemplate(pagePath, c.categoryListTemplate(category), view); err != nil {
				return err
			}
			if page == 1 {
				alias := strings.TrimPrefix(c.categoryListURL(category, 1), "/")
				if alias != "" {
					c.pages[alias] = c.pages[pagePath]
				}
				directory := c.categoryListDir(category)
				if directory != "" {
					indexPath := directory + "/index.html"
					// Several categories may intentionally share a list directory. The
					// first category in the configured order owns that directory index.
					if c.pages[indexPath] == nil {
						c.pages[indexPath] = c.pages[pagePath]
					}
				}
			}
		}
	}

	if err := c.page("msg.html", templateconfig.RoleMessage, Row{}); err != nil {
		return err
	}
	if err := c.page("search.html", templateconfig.RoleSearch, Row{}); err != nil {
		return err
	}
	urls := make([]string, 0, len(c.pages))
	for pagePath := range c.pages {
		urls = append(urls, pagePath)
	}
	sort.Strings(urls)
	c.pages["sitemap.html"] = renderSitemapHTML(urls)
	c.pages["Sitemap.xml"] = renderSitemapXML(strings.TrimRight(c.settings["site_url"], "/"), urls)
	return nil
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
