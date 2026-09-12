package generator

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"io"
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
	"gocms/internal/templatelabel"
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
	HomeTemplate         string
}

type LanguageInfo struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	IsCurrent  bool   `json:"is_current"`
	IsDefault  bool   `json:"is_default"`
	PathPrefix string `json:"path_prefix,omitempty"`
}

type content struct {
	tables           map[string][]Row
	settings         map[string]string
	templates        map[string]*template.Template
	labelTemplates   map[string]*template.Template
	assignments      map[string]string
	pages            map[string][]byte
	homeTemplatePath string
	labelDepth       int
	lang             string
	langPrefix       string
	languages        []LanguageInfo
	visibleCache     []Row
	categoryCache    map[int][]Row
	categoryByID     map[int]Row
	childrenCache    map[int][]Row
	navigationCache  []NavigationItem
	catalogCache     []ListCategory
}

// ListItem, ListCategory, ListPagination, and NavigationItem are the data
// contract exposed to themes. They intentionally contain no markup.
type ListItem struct {
	ID          int
	CategoryID  int
	Index       int
	First       bool
	Last        bool
	Fields      map[string]string
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

type TemplateField struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

type TemplateTag struct {
	Name        string          `json:"name"`
	Category    string          `json:"category"`
	Signature   string          `json:"signature"`
	Description string          `json:"description"`
	Context     string          `json:"context"`
	Example     string          `json:"example"`
	Fields      []TemplateField `json:"fields,omitempty"`
}

// TemplateTags is the public template contract shown in the admin. Keep this
// list next to templateFuncs so the documentation and parser evolve together.
func TemplateTags() []TemplateTag {
	return []TemplateTag{
		{
			Name: "setting", Category: "全局设置", Signature: `{{setting "site_name"}}`,
			Description: "读取站点设置并进行 HTML 转义，适合输出站点名称、网址等文本。",
			Context:     "所有模板", Example: `<title>{{setting "site_name"}}</title>`,
		},
		{
			Name: "settingHTML", Category: "全局设置", Signature: `{{settingHTML "site_footer_links"}}`,
			Description: "读取站点设置中的原始 HTML。只应使用受信任的后台设置内容。",
			Context:     "所有模板", Example: `<footer>{{settingHTML "site_footer_links"}}</footer>`,
		},
		{
			Name: "include", Category: "公共模板", Signature: `{{include "site-header.html" .}}`,
			Description: "渲染主题 templates 目录中的公共 HTML 模板，并传入当前上下文。",
			Context:     "所有模板", Example: `{{include "site-header.html" .}}`,
		},
		{
			Name: "label", Category: "标签模板", Signature: `{{label "content-card" .}}`,
			Description: "渲染后台保存的标签模板。标签模板内部可以继续使用本清单中的模板标签。",
			Context:     "所有模板", Example: `{{range listItems .}}{{label "content-card" .}}{{end}}`,
		},
		{
			Name: "navigation", Category: "栏目导航", Signature: `{{range navigation .}}...{{end}}`,
			Description: "返回从根栏目开始的完整导航树。传入参数用于保持模板调用形式，当前上下文不会改变导航范围。",
			Context:     "所有模板", Example: `{{range navigation .}}<a href="{{.URL}}">{{.Name}}</a>{{end}}`,
			Fields: []TemplateField{
				{Name: ".URL", Type: "string", Description: "栏目链接"},
				{Name: ".Name", Type: "string", Description: "栏目名称"},
				{Name: ".Children", Type: "[]NavigationItem", Description: "子栏目导航，可继续 range"},
			},
		},
		{
			Name: "contentItems", Category: "内容列表", Signature: `{{range contentItems 0 10 true false "newest"}}...{{end}}`,
			Description: "从发布快照调用公开内容：栏目 ID（0 为全站）、条数（1–500）、包含子栏目、仅推荐、排序（sort/newest/oldest）。返回 Index、First、Last 和已转义的 Fields 自定义字段。",
			Context:     "所有模板", Example: `{{range contentItems 0 10 true false "newest"}}{{.Index}} <a href="{{.URL}}">{{.Title}}</a>{{else}}暂无内容{{end}}`,
			Fields: []TemplateField{
				{Name: ".ID / .CategoryID", Type: "int", Description: "内容 ID 和所属栏目 ID"},
				{Name: ".Index / .First / .Last", Type: "int / bool", Description: "从 1 开始的序号及首末项标记"},
				{Name: ".Fields", Type: "map[string]string", Description: "模型扩展字段，值已 HTML 转义"},
			},
		},
		{
			Name: "contentItemsWithImage", Category: "内容列表", Signature: `{{range contentItemsWithImage 0 10 true false true "newest"}}...{{end}}`,
			Description: "contentItems 的图片筛选版本，只返回有封面图片的公开内容。参数依次为栏目 ID、条数、包含子栏目、仅推荐、只显示有图片、排序。",
			Context:     "所有模板", Example: `{{range contentItemsWithImage 0 10 true false true "newest"}}<img src="{{.Image}}" alt="{{.Title}}">{{end}}`,
			Fields: []TemplateField{
				{Name: ".ID / .CategoryID / .Index", Type: "int", Description: "内容 ID、栏目 ID 和从 1 开始的序号"},
				{Name: ".URL / .Title / .Image", Type: "string", Description: "详情链接、标题和封面图片地址"},
				{Name: ".Fields", Type: "map[string]string", Description: "模型扩展字段，值已 HTML 转义"},
			},
		},
		{
			Name: "listItems", Category: "内容列表", Signature: `{{range listItems .}}...{{end}}`,
			Description: "返回当前栏目当前分页中的可见内容。列表模板和标签模板通常使用它输出内容卡片。",
			Context:     "列表模板", Example: `{{range listItems .}}<a href="{{.URL}}">{{.Title}}</a>{{end}}`,
			Fields: []TemplateField{
				{Name: ".ID / .CategoryID", Type: "int", Description: "内容 ID 和所属栏目 ID"},
				{Name: ".Index / .First / .Last", Type: "int / bool", Description: "从 1 开始的序号及首末项标记"},
				{Name: ".URL", Type: "string", Description: "内容详情链接"},
				{Name: ".Title", Type: "string", Description: "内容标题"},
				{Name: ".Summary", Type: "string", Description: "完整摘要"},
				{Name: ".Excerpt", Type: "string", Description: "适合列表展示的摘要"},
				{Name: ".PublishedAt", Type: "string", Description: "完整发布日期"},
				{Name: ".Date", Type: "string", Description: "发布日期前 10 位"},
				{Name: ".Image", Type: "string", Description: "封面图片地址"},
				{Name: ".Category", Type: "string", Description: "所属栏目名称"},
				{Name: ".Fields", Type: "map[string]string", Description: "模型扩展字段，值已 HTML 转义"},
				{Name: ".RowStart / .RowEnd", Type: "bool", Description: "按两列分组的首尾标记"},
			},
		},
		{
			Name: "listCategories", Category: "栏目导航", Signature: `{{range listCategories .}}...{{end}}`,
			Description: "返回当前栏目根栏目下的同级栏目，当前栏目带有 Current 标记。",
			Context:     "列表模板", Example: `{{range listCategories .}}<a class="{{if .Current}}active{{end}}" href="{{.URL}}">{{.Name}}</a>{{end}}`,
			Fields: []TemplateField{
				{Name: ".URL / .Name", Type: "string", Description: "栏目链接和名称"},
				{Name: ".Current / .Last", Type: "bool", Description: "当前项和最后一项标记"},
			},
		},
		{
			Name: "listChildren", Category: "栏目导航", Signature: `{{range listChildren .}}...{{end}}`,
			Description: "返回当前栏目直属的子栏目，适合封面模板展示栏目入口。",
			Context:     "栏目模板", Example: `{{range listChildren .}}<a href="{{.URL}}">{{.Name}}</a>{{end}}`,
			Fields: []TemplateField{
				{Name: ".URL / .Name", Type: "string", Description: "子栏目链接和名称"},
				{Name: ".Last", Type: "bool", Description: "最后一项标记"},
				{Name: ".RowStart / .RowEnd", Type: "bool", Description: "按五列分组的首尾标记"},
			},
		},
		{
			Name: "catalogCategories", Category: "栏目导航", Signature: `{{range catalogCategories .}}...{{end}}`,
			Description: "返回当前站点根栏目下的一级栏目集合，适合站点栏目目录。",
			Context:     "所有模板", Example: `{{range catalogCategories .}}<a href="{{.URL}}">{{.Name}}</a>{{end}}`,
			Fields: []TemplateField{
				{Name: ".URL / .Name", Type: "string", Description: "栏目链接和名称"},
				{Name: ".Last", Type: "bool", Description: "最后一项标记"},
			},
		},
		{
			Name: "featuredItems", Category: "内容列表", Signature: `{{range featuredItems 6}}...{{end}}`,
			Description: "返回全站推荐内容，参数为最大显示条数。",
			Context:     "所有模板", Example: `{{range featuredItems 6}}<a href="{{.URL}}">{{.Title}}</a>{{end}}`,
			Fields: []TemplateField{{Name: "参数", Type: "int", Description: "最大显示条数"}},
		},
		{
			Name: "featuredItemsIn", Category: "内容列表", Signature: `{{range featuredItemsIn "collection" 6}}...{{end}}`,
			Description: "返回指定内容集合中的推荐内容，参数依次为集合标识和最大显示条数。",
			Context:     "所有模板", Example: `{{range featuredItemsIn "products" 6}}{{.Title}}{{end}}`,
			Fields: []TemplateField{
				{Name: "collection", Type: "string", Description: "根栏目 URL 目录标识"},
				{Name: "limit", Type: "int", Description: "最大显示条数"},
			},
		},
		{
			Name: "relatedItems", Category: "相关内容", Signature: `{{range relatedItems . 6}}...{{end}}`,
			Description: "返回当前内容同栏目的其他可见内容，参数为当前详情上下文和最大显示条数。",
			Context:     "详情模板", Example: `{{range relatedItems . 6}}<a href="{{.URL}}">{{.Title}}</a>{{end}}`,
			Fields: []TemplateField{{Name: "limit", Type: "int", Description: "最大显示条数"}},
		},
		{
			Name: "listPagination", Category: "分页", Signature: `{{with listPagination .}}...{{end}}`,
			Description: "返回当前栏目分页信息。分页链接已经按栏目路由生成。",
			Context:     "列表模板", Example: `{{with listPagination .}}{{range .PageLinks}}<a href="{{.URL}}">{{.Number}}</a>{{end}}{{end}}`,
			Fields: []TemplateField{
				{Name: ".Total / .Page / .Pages / .PageSize", Type: "int", Description: "总数、当前页、总页数和每页数量"},
				{Name: ".FirstURL / .PreviousURL / .NextURL / .LastURL", Type: "string", Description: "首页、上一页、下一页和末页链接"},
				{Name: ".HasPrevious / .HasNext", Type: "bool", Description: "是否存在上一页或下一页"},
				{Name: ".PageLinks", Type: "[]ListPage", Description: "页码链接集合，每项有 .Number、.URL、.Current"},
			},
		},
	}
}

func (c *content) templateFuncs() template.FuncMap {
	return template.FuncMap{
		"contentItems":          c.contentItems,
		"contentItemsWithImage": c.contentItemsWithImage,
		"setting":               func(key string) string { return esc(c.settings[strings.TrimSpace(key)]) },
		"settingHTML":           func(key string) string { return c.settings[strings.TrimSpace(key)] },
		"include":               func(path string, row Row) (string, error) { return c.include(path, row) },
		"label":                 func(key string, data any) (string, error) { return c.renderLabel(key, data) },
		"listItems":             func(row Row) []ListItem { return c.listItems(row) },
		"listCategories":        func(row Row) []ListCategory { return c.listCategories(row) },
		"listChildren":          func(row Row) []ListCategory { return c.listChildren(row) },
		"catalogCategories":     func(_ Row) []ListCategory { return c.catalogCategories() },
		"navigation":            func(_ Row) []NavigationItem { return c.navigation() },
		"featuredItems":         func(limit int) []ListItem { return c.featuredItems(limit) },
		"featuredItemsIn": func(collection string, limit int) []ListItem {
			return c.featuredItemsIn(collection, limit)
		},
		"relatedItems": func(row Row, limit int) []ListItem {
			return c.relatedItems(row, limit)
		},
		"listPagination": func(row Row) ListPagination {
			return c.listPagination(row)
		},
		"currentLang": func() string { return c.lang },
		"languages":   func(_ ...any) []LanguageInfo { return c.languagesNav() },
		"morepic": func(val any) []PhotoItem {
			if val == nil {
				return nil
			}
			return parseMorepic(fmt.Sprint(val))
		},
		"multiValue": func(val any) []string {
			if val == nil {
				return nil
			}
			return parseMultiValue(fmt.Sprint(val))
		},
	}
}

func (c *content) homeURL() string {
	if c.langPrefix != "" {
		return "/" + c.langPrefix + "/"
	}
	return "/"
}

func (c *content) languagesNav() []LanguageInfo {
	res := make([]LanguageInfo, len(c.languages))
	for i, l := range c.languages {
		res[i] = LanguageInfo{
			Code:       l.Code,
			Name:       l.Name,
			URL:        l.URL,
			IsCurrent:  l.Code == c.lang,
			IsDefault:  l.IsDefault,
			PathPrefix: l.PathPrefix,
		}
	}
	return res
}

type PhotoItem struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Order int    `json:"order,omitempty"`
}

func parseMorepic(raw string) []PhotoItem {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		var items []PhotoItem
		if err := json.Unmarshal([]byte(raw), &items); err == nil && len(items) > 0 {
			for i := range items {
				if items[i].Order == 0 {
					items[i].Order = i + 1
				}
			}
			return items
		}
		var stringUrls []string
		if err := json.Unmarshal([]byte(raw), &stringUrls); err == nil && len(stringUrls) > 0 {
			res := make([]PhotoItem, 0, len(stringUrls))
			for i, u := range stringUrls {
				res = append(res, PhotoItem{URL: strings.TrimSpace(u), Order: i + 1})
			}
			return res
		}
	}
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	items := make([]PhotoItem, 0, len(lines))
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "::::::")
		item := PhotoItem{Order: i + 1}
		if len(parts) >= 3 {
			item.URL = strings.TrimSpace(parts[0])
			item.Title = strings.TrimSpace(parts[2])
		} else if len(parts) == 2 {
			item.URL = strings.TrimSpace(parts[0])
			item.Title = strings.TrimSpace(parts[1])
		} else {
			item.URL = strings.TrimSpace(parts[0])
		}
		if item.URL != "" {
			items = append(items, item)
		}
	}
	return items
}

func parseMultiValue(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		var arr []string
		if err := json.Unmarshal([]byte(raw), &arr); err == nil {
			return arr
		}
	}
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	var res []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			res = append(res, line)
		}
	}
	return res
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

func readOptionalTable(ctx context.Context, tx *sql.Tx, name string) ([]Row, error) {
	rows, err := readTable(ctx, tx, name)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return []Row{}, nil
		}
		return nil, err
	}
	return rows, nil
}

func prepareTranslatedCategories(baseRows, transRows []Row, targetLang, fallbackLang string) []Row {
	transMap := make(map[string]Row)
	for _, tr := range transRows {
		key := tr["category_id"] + "_" + tr["lang"]
		transMap[key] = tr
	}

	result := make([]Row, len(baseRows))
	for i, base := range baseRows {
		row := make(Row, len(base))
		for k, v := range base {
			row[k] = v
		}

		catID := base["id"]
		targetTrans := transMap[catID+"_"+targetLang]
		fallbackTrans := transMap[catID+"_"+fallbackLang]

		if v := strings.TrimSpace(targetTrans["name"]); v != "" {
			row["name"] = v
		} else if v := strings.TrimSpace(fallbackTrans["name"]); v != "" {
			row["name"] = v
		}

		if v := strings.TrimSpace(targetTrans["seo_title"]); v != "" {
			row["seo_title"] = v
		} else if v := strings.TrimSpace(fallbackTrans["seo_title"]); v != "" {
			row["seo_title"] = v
		}

		if v := strings.TrimSpace(targetTrans["keywords"]); v != "" {
			row["keywords"] = v
		} else if v := strings.TrimSpace(fallbackTrans["keywords"]); v != "" {
			row["keywords"] = v
		}

		if v := strings.TrimSpace(targetTrans["description"]); v != "" {
			row["description"] = v
		} else if v := strings.TrimSpace(fallbackTrans["description"]); v != "" {
			row["description"] = v
		}

		if v := strings.TrimSpace(targetTrans["cover_content"]); v != "" {
			row["cover_content"] = v
		} else if v := strings.TrimSpace(fallbackTrans["cover_content"]); v != "" {
			row["cover_content"] = v
		}

		result[i] = row
	}
	return result
}

func prepareTranslatedContent(baseRows, transRows []Row, targetLang, fallbackLang string, transFields map[string]bool) []Row {
	transMap := make(map[string]Row)
	for _, tr := range transRows {
		key := tr["content_id"] + "_" + tr["lang"]
		transMap[key] = tr
	}

	result := make([]Row, len(baseRows))
	for i, base := range baseRows {
		row := make(Row, len(base))
		for k, v := range base {
			row[k] = v
		}

		contentID := base["id"]
		targetTrans := transMap[contentID+"_"+targetLang]
		fallbackTrans := transMap[contentID+"_"+fallbackLang]

		if v := strings.TrimSpace(targetTrans["title"]); v != "" {
			row["title"] = v
		} else if v := strings.TrimSpace(fallbackTrans["title"]); v != "" {
			row["title"] = v
		}

		if v := strings.TrimSpace(targetTrans["summary"]); v != "" {
			row["summary"] = v
		} else if v := strings.TrimSpace(fallbackTrans["summary"]); v != "" {
			row["summary"] = v
		}

		bodyVal := strings.TrimSpace(targetTrans["body"])
		if bodyVal == "" {
			bodyVal = strings.TrimSpace(targetTrans["content"])
		}
		if bodyVal != "" {
			row["body"] = bodyVal
		} else {
			fbBodyVal := strings.TrimSpace(fallbackTrans["body"])
			if fbBodyVal == "" {
				fbBodyVal = strings.TrimSpace(fallbackTrans["content"])
			}
			if fbBodyVal != "" {
				row["body"] = fbBodyVal
			}
		}

		if v := strings.TrimSpace(targetTrans["keywords"]); v != "" {
			row["keywords"] = v
		} else if v := strings.TrimSpace(fallbackTrans["keywords"]); v != "" {
			row["keywords"] = v
		}

		if v := strings.TrimSpace(targetTrans["description"]); v != "" {
			row["description"] = v
		} else if v := strings.TrimSpace(fallbackTrans["description"]); v != "" {
			row["description"] = v
		}

		var extraMap map[string]any
		if baseExtra := strings.TrimSpace(base["extra_data"]); baseExtra != "" && baseExtra != "{}" {
			_ = json.Unmarshal([]byte(baseExtra), &extraMap)
		}
		if extraMap == nil {
			extraMap = make(map[string]any)
		}
		if fbExtraStr := strings.TrimSpace(fallbackTrans["extra_data"]); fbExtraStr != "" && fbExtraStr != "{}" {
			var fbExtra map[string]any
			if err := json.Unmarshal([]byte(fbExtraStr), &fbExtra); err == nil {
				for k, v := range fbExtra {
					if transFields[k] && strings.TrimSpace(fmt.Sprint(v)) != "" {
						extraMap[k] = v
					}
				}
			}
		}
		if tgExtraStr := strings.TrimSpace(targetTrans["extra_data"]); tgExtraStr != "" && tgExtraStr != "{}" {
			var tgExtra map[string]any
			if err := json.Unmarshal([]byte(tgExtraStr), &tgExtra); err == nil {
				for k, v := range tgExtra {
					if transFields[k] && strings.TrimSpace(fmt.Sprint(v)) != "" {
						extraMap[k] = v
					}
				}
			}
		}
		extraBytes, _ := json.Marshal(extraMap)
		row["extra_data"] = string(extraBytes)

		result[i] = row
	}
	return result
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
		tables:           map[string][]Row{},
		settings:         map[string]string{},
		templates:        map[string]*template.Template{},
		labelTemplates:   map[string]*template.Template{},
		assignments:      map[string]string{},
		pages:            map[string][]byte{},
		homeTemplatePath: p.HomeTemplate,
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
	for _, name := range []string{"gocms_category_translation", "gocms_content_translation", "gocms_language", "gocms_model_field"} {
		rows, _ := readOptionalTable(ctx, tx, name)
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
	labels, err := templatelabel.Load(ctx, tx)
	if err != nil {
		_ = tx.Rollback()
		return report, err
	}
	if err = tx.Commit(); err != nil {
		return report, err
	}

	var enabledLanguages []LanguageInfo
	defaultLang := ""
	fallbackLang := ""

	for _, lr := range c.tables["gocms_language"] {
		if lr.n("is_enabled") == 1 {
			isDef := lr.n("is_default") == 1
			isFb := lr.n("is_fallback") == 1
			prefix := strings.Trim(lr["path_prefix"], "/")
			if isDef {
				defaultLang = lr["code"]
				prefix = ""
			} else if prefix == "" {
				prefix = strings.ToLower(lr["code"])
			}
			if isFb {
				fallbackLang = lr["code"]
			}
			url := "/"
			if prefix != "" {
				url = "/" + prefix + "/"
			}
			enabledLanguages = append(enabledLanguages, LanguageInfo{
				Code:       lr["code"],
				Name:       lr["name"],
				URL:        url,
				IsDefault:  isDef,
				PathPrefix: prefix,
			})
		}
	}
	if len(enabledLanguages) == 0 {
		enabledLanguages = append(enabledLanguages, LanguageInfo{
			Code:       "zh-CN",
			Name:       "中文",
			URL:        "/",
			IsDefault:  true,
			PathPrefix: "",
		})
		defaultLang = "zh-CN"
		fallbackLang = "zh-CN"
	}
	if defaultLang == "" {
		defaultLang = enabledLanguages[0].Code
		enabledLanguages[0].IsDefault = true
		enabledLanguages[0].PathPrefix = ""
		enabledLanguages[0].URL = "/"
	}
	if fallbackLang == "" {
		fallbackLang = defaultLang
	}
	c.languages = enabledLanguages

	transFields := map[string]bool{
		"title":       true,
		"summary":     true,
		"body":        true,
		"content":     true,
		"keywords":    true,
		"description": true,
	}
	for _, f := range c.tables["gocms_model_field"] {
		if f.n("is_translatable") == 1 {
			transFields[strings.ToLower(strings.TrimSpace(f["field_name"]))] = true
		}
	}

	for _, label := range labels {
		content, expandErr := expandLegacyLoopSyntax(label.Content)
		if expandErr != nil {
			return report, fmt.Errorf("解析标签模板 %s: %w", label.Key, expandErr)
		}
		parsed, parseErr := template.New("label:" + label.Key).Funcs(c.templateFuncs()).Parse(content)
		if parseErr != nil {
			return report, fmt.Errorf("解析标签模板 %s: %w", label.Key, parseErr)
		}
		c.labelTemplates[label.Key] = parsed
	}

	baseCategories := c.tables["gocms_category"]
	baseContents := c.tables["gocms_content"]
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
		content, expandErr := expandLegacyLoopSyntax(string(data))
		if expandErr != nil {
			return fmt.Errorf("解析模板 %s: %w", templatePath, expandErr)
		}
		parsed, err := template.New(templatePath).Funcs(c.templateFuncs()).Parse(content)
		if err != nil {
			return fmt.Errorf("解析模板 %s: %w", templatePath, err)
		}
		c.templates[templatePath] = parsed
		return nil
	})
	if err != nil {
		return report, err
	}
	for _, lang := range enabledLanguages {
		c.lang = lang.Code
		c.langPrefix = lang.PathPrefix
		c.tables["gocms_category"] = prepareTranslatedCategories(baseCategories, c.tables["gocms_category_translation"], lang.Code, fallbackLang)
		c.tables["gocms_content"] = prepareTranslatedContent(baseContents, c.tables["gocms_content_translation"], lang.Code, fallbackLang, transFields)
		c.visibleCache = nil
		c.categoryCache = nil
		c.categoryByID = nil
		c.childrenCache = nil
		c.navigationCache = nil
		c.catalogCache = nil
		sortRows(c.tables["gocms_category"], "order_id", true)
		sortRows(c.tables["gocms_content"], "sort_order", false)
		if err = c.buildForLang(); err != nil {
			return report, err
		}
	}

	urls := make([]string, 0, len(c.pages))
	for pagePath := range c.pages {
		urls = append(urls, pagePath)
	}
	sort.Strings(urls)
	c.pages["llms.txt"], err = renderLLMS(ctx, c.settings["site_url"], urls, func(path string) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(c.pages[path])), nil })
	if err != nil {
		return report, err
	}
	c.pages["sitemap.html"] = renderSitemapHTML(urls)
	c.pages["Sitemap.xml"] = renderSitemapXML(strings.TrimRight(c.settings["site_url"], "/"), urls)

	for _, row := range baseContents {
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
		// stage is a private temporary tree; avoid per-file temp files and renames.
		// This substantially reduces filesystem overhead for large publications.
		path := filepath.Join(stage, filepath.FromSlash(relative))
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return report, err
		}
		if err = os.WriteFile(path, data, 0644); err != nil {
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

func (c *content) homeTemplate() string {
	if strings.TrimSpace(c.homeTemplatePath) != "" {
		return strings.TrimSpace(c.homeTemplatePath)
	}
	if templatePath, ok := c.assignments[templateconfig.RoleHomeIndex]; ok && strings.TrimSpace(templatePath) != "" {
		return strings.TrimSpace(templatePath)
	}
	return "index.html"
}

func (c *content) page(rolePath, role string, row Row) error {
	templatePath, ok := c.assignments[role]
	if !ok {
		if role == templateconfig.RoleHomeIndex {
			return c.pageTemplate(rolePath, c.homeTemplate(), row)
		}
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

func (c *content) renderLabel(key string, data any) (string, error) {
	key = strings.ToLower(strings.TrimSpace(key))
	parsed, ok := c.labelTemplates[key]
	if !ok {
		return "", fmt.Errorf("标签模板不存在: %s", key)
	}
	if c.labelDepth >= 32 {
		return "", fmt.Errorf("标签模板嵌套层级超过限制")
	}
	c.labelDepth++
	defer func() { c.labelDepth-- }()
	var output strings.Builder
	if err := parsed.Execute(&output, data); err != nil {
		return "", fmt.Errorf("渲染标签模板 %s: %w", key, err)
	}
	return output.String(), nil
}

func (c *content) cat(id int) Row {
	if c.categoryByID == nil {
		c.categoryByID = make(map[int]Row, len(c.tables["gocms_category"]))
		for _, category := range c.tables["gocms_category"] {
			c.categoryByID[category.n("id")] = category
		}
	}
	if category, ok := c.categoryByID[id]; ok {
		return category
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
			if c.langPrefix != "" {
				return "/" + c.langPrefix + "/"
			}
			return "/"
		}
		if c.langPrefix != "" {
			return "/" + c.langPrefix + "/" + strings.Trim(directory, "/") + "/"
		}
		return "/" + strings.Trim(directory, "/") + "/"
	}
	target := strings.Trim(directory+"/"+filename, "/")
	if c.langPrefix != "" {
		return "/" + c.langPrefix + "/" + target
	}
	return "/" + target
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
	target := strings.Trim(c.categoryListDir(row)+"/"+filename, "/")
	if c.langPrefix != "" {
		return c.langPrefix + "/" + target
	}
	return target
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
	target := strings.Trim(c.categoryDetailDir(category)+"/"+filename, "/")
	if c.langPrefix != "" {
		return "/" + c.langPrefix + "/" + target
	}
	return "/" + target
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
		if c.langPrefix != "" {
			return "/" + c.langPrefix + "/" + routing.DefaultListPath() + "/"
		}
		return "/" + routing.DefaultListPath() + "/"
	}
	directory := c.categoryListDir(root)
	if directory == "" {
		if c.langPrefix != "" {
			return "/" + c.langPrefix + "/"
		}
		return "/"
	}
	if c.langPrefix != "" {
		return "/" + c.langPrefix + "/" + strings.Trim(directory, "/") + "/"
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
	if c.visibleCache != nil {
		return c.visibleCache
	}
	items := make([]Row, 0, len(c.tables["gocms_content"]))
	for _, item := range c.tables["gocms_content"] {
		if item.n("visible") == 1 {
			items = append(items, item)
		}
	}
	sortRows(items, "sort_order", false)
	c.visibleCache = items
	return items
}

func (c *content) contentsForCategory(category Row) []Row {
	if c.categoryCache == nil {
		c.categoryCache = make(map[int][]Row)
	}
	id := category.n("id")
	if v, ok := c.categoryCache[id]; ok {
		return v
	}
	items := make([]Row, 0)
	for _, item := range c.visibleContents() {
		if c.under(item.n("category_id"), category.n("id")) {
			items = append(items, item)
		}
	}
	c.categoryCache[id] = items
	return items
}

func (c *content) listContext(view Row) (category Row, pageItems []Row, page, pages, pageSize, total int) {
	category = c.cat(view.n("category_id"))
	allItems := c.contentsForCategory(category)
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
	fields := map[string]string{}
	var extra map[string]any
	if json.Unmarshal([]byte(row["extra_data"]), &extra) == nil {
		for key, value := range extra {
			fields[key] = esc(fmt.Sprint(value))
		}
	}
	publishedAt := strings.TrimSpace(row["published_at"])
	summary := strings.TrimSpace(row["summary"])
	image := strings.TrimSpace(row["cover_image"])
	return ListItem{
		ID: row.n("id"), CategoryID: row.n("category_id"), Index: index + 1,
		First: index == 0, Last: index+1 == total, Fields: fields,
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
	if c.catalogCache != nil {
		return c.catalogCache
	}
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
	c.catalogCache = items
	return items
}

func (c *content) children(parentID int) []Row {
	if c.childrenCache == nil {
		c.childrenCache = make(map[int][]Row)
		for _, category := range c.tables["gocms_category"] {
			id := category.n("parent_id")
			c.childrenCache[id] = append(c.childrenCache[id], category)
		}
		for _, children := range c.childrenCache {
			sortRows(children, "order_id", true)
		}
	}
	return c.childrenCache[parentID]
}

func (c *content) navigation() []NavigationItem {
	if c.navigationCache != nil {
		return c.navigationCache
	}
	items := make([]NavigationItem, 0)
	for _, category := range c.children(0) {
		items = append(items, c.navigationItem(category))
	}
	c.navigationCache = items
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

func (c *content) relatedItems(view Row, limit int) []ListItem {
	if limit < 1 {
		return []ListItem{}
	}
	category := c.cat(view.n("category_id"))
	if category["id"] == "" {
		return []ListItem{}
	}
	items := make([]Row, 0, limit)
	for _, item := range c.tables["gocms_content"] {
		if item.n("visible") != 1 || item.n("id") == view.n("id") || item.n("category_id") != category.n("id") {
			continue
		}
		items = append(items, item)
	}
	sortRows(items, "sort_order", false)
	if len(items) > limit {
		items = items[:limit]
	}
	result := make([]ListItem, 0, len(items))
	for index, item := range items {
		result = append(result, c.listItem(item, category, index, len(items)))
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
	root := c.listRoot(category)
	image := strings.TrimSpace(row["cover_image"])
	res := Row{
		"id":                 esc(row["id"]),
		"route_key":          esc(row["route_key"]),
		"title":              esc(row["title"]),
		"body":               row["body"],
		"content":            row["body"],
		"code":               esc(row["code"]),
		"summary":            esc(row["summary"]),
		"cover_image":        esc(image),
		"image":              esc(image),
		"published_at":       esc(row["published_at"]),
		"date":               esc(row["published_at"]),
		"source":             esc(row["source"]),
		"keywords":           esc(row["keywords"]),
		"description":        esc(row["description"]),
		"category_id":        esc(category["id"]),
		"category_name":      esc(category["name"]),
		"category_url":       c.categoryListURL(category, 1),
		"category_root_id":   esc(root["id"]),
		"category_root_name": esc(root["name"]),
		"category_root_url":  c.categoryListURL(root, 1),
		"content_url":        c.contentURL(row),
		"home_url":           c.homeURL(),
		"lang":               c.lang,
		"category_children":  "",
		"model_id":           esc(row["model_id"]),
	}
	if extra := strings.TrimSpace(row["extra_data"]); extra != "" && extra != "{}" {
		var extraMap map[string]any
		if err := json.Unmarshal([]byte(extra), &extraMap); err == nil {
			for k, v := range extraMap {
				if _, exists := res[k]; !exists {
					res[k] = fmt.Sprint(v)
				}
			}
		}
	}
	for k, v := range row {
		if _, exists := res[k]; !exists {
			res[k] = v
		}
	}
	return res
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
		"home_url":          c.homeURL(),
		"lang":              c.lang,
		"keywords":          esc(category["keywords"]),
		"description":       esc(category["description"]),
		"body":              category["cover_content"],
		"content":           category["cover_content"],
		"content_count":     strconv.Itoa(len(items)),
		"content_page_size": strconv.Itoa(pageSize),
	}
}

func (c *content) buildForLang() error {
	homePagePath := "index.html"
	if c.langPrefix != "" {
		homePagePath = c.langPrefix + "/index.html"
	}
	if err := c.pageTemplate(homePagePath, c.homeTemplate(), Row{
		"title":         esc(c.settings["site_name"]),
		"site_name":     esc(c.settings["site_name"]),
		"site_url":      esc(c.settings["site_url"]),
		"root_category": "",
		"home_url":      c.homeURL(),
		"lang":          c.lang,
	}); err != nil {
		return err
	}

	visible := c.visibleContents()
	byCategory := make(map[int][]Row)
	positions := make(map[int]int)
	for _, item := range visible {
		positions[item.n("id")] = len(byCategory[item.n("category_id")])
		byCategory[item.n("category_id")] = append(byCategory[item.n("category_id")], item)
	}
	for _, item := range visible {
		category := c.cat(item.n("category_id"))
		view := c.contentView(item)
		view["previous_url"], view["previous_title"] = "", ""
		view["next_url"], view["next_title"] = "", ""
		sameCategory := byCategory[item.n("category_id")]
		index := positions[item.n("id")]
		if index > 0 {
			view["previous_url"] = esc(c.contentURL(sameCategory[index-1]))
			view["previous_title"] = esc(sameCategory[index-1]["title"])
		}
		if index+1 < len(sameCategory) {
			view["next_url"] = esc(c.contentURL(sameCategory[index+1]))
			view["next_title"] = esc(sameCategory[index+1]["title"])
		}
		if err := c.pageTemplate(strings.TrimPrefix(c.contentURL(item), "/"), c.categoryDetailTemplate(category), view); err != nil {
			return err
		}
	}

	for _, category := range c.tables["gocms_category"] {
		items := c.contentsForCategory(category)
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
				if alias != "" && !strings.HasSuffix(alias, "/") {
					c.pages[alias] = c.pages[pagePath]
				}
				directory := c.categoryListDir(category)
				if directory != "" {
					indexPath := directory + "/index.html"
					if c.langPrefix != "" {
						indexPath = c.langPrefix + "/" + indexPath
					}
					// Several categories may intentionally share a list directory. The
					// first category in the configured order owns that directory index.
					if c.pages[indexPath] == nil {
						c.pages[indexPath] = c.pages[pagePath]
					}
				}
			}
		}
	}

	msgPath := "msg.html"
	searchPath := "search.html"
	if c.langPrefix != "" {
		msgPath = c.langPrefix + "/msg.html"
		searchPath = c.langPrefix + "/search.html"
	}
	if err := c.page(msgPath, templateconfig.RoleMessage, Row{"home_url": c.homeURL(), "lang": c.lang}); err != nil {
		return err
	}
	if err := c.page(searchPath, templateconfig.RoleSearch, Row{"home_url": c.homeURL(), "lang": c.lang}); err != nil {
		return err
	}
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
