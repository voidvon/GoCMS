package templatelabel

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"text/template"
)

const (
	categoryTable = "gocms_template_label_category"
	labelTable    = "gocms_template_label"

	ContextAny      = "any"
	ContextHome     = "home"
	ContextCategory = "category"
	ContextList     = "list"
	ContextDetail   = "detail"
)

var keyPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

type Category struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	SortOrder  int64  `json:"sort_order"`
	LabelCount int64  `json:"label_count"`
}

type Label struct {
	ID           int64  `json:"id"`
	Key          string `json:"key"`
	Name         string `json:"name"`
	CategoryID   int64  `json:"category_id"`
	CategoryName string `json:"category_name"`
	Context      string `json:"context"`
	Description  string `json:"description"`
	Content      string `json:"content"`
	Temptext     string `json:"temptext"`
	Listvar      string `json:"listvar"`
	Rownum       int    `json:"rownum"`
	Subnews      int    `json:"subnews"`
	Showdate     string `json:"showdate"`
	SortOrder    int64  `json:"sort_order"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

type Input struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	CategoryID  int64  `json:"category_id"`
	Context     string `json:"context"`
	Description string `json:"description"`
	Content     string `json:"content"`
	Temptext    string `json:"temptext"`
	Listvar     string `json:"listvar"`
	Rownum      int    `json:"rownum"`
	Subnews     int    `json:"subnews"`
	Showdate    string `json:"showdate"`
	SortOrder   int64  `json:"sort_order"`
}

type Page struct {
	Page     int     `json:"page"`
	PageSize int     `json:"page_size"`
	Total    int64   `json:"total"`
	Items    []Label `json:"items"`
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func Ensure(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS "`+categoryTable+`" (
			"id" INTEGER PRIMARY KEY AUTOINCREMENT,
			"name" TEXT NOT NULL UNIQUE,
			"sort_order" INTEGER NOT NULL DEFAULT 0,
			"created_at" TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			"updated_at" TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		return fmt.Errorf("create template label category table: %w", err)
	}
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS "`+labelTable+`" (
			"id" INTEGER PRIMARY KEY AUTOINCREMENT,
			"key" TEXT NOT NULL UNIQUE,
			"name" TEXT NOT NULL,
			"category_id" INTEGER NOT NULL DEFAULT 0,
			"context" TEXT NOT NULL DEFAULT 'any',
			"description" TEXT NOT NULL DEFAULT '',
			"content" TEXT NOT NULL DEFAULT '',
			"temptext" TEXT NOT NULL DEFAULT '',
			"listvar" TEXT NOT NULL DEFAULT '',
			"rownum" INTEGER NOT NULL DEFAULT 1,
			"subnews" INTEGER NOT NULL DEFAULT 0,
			"showdate" TEXT NOT NULL DEFAULT 'Y-m-d H:i:s',
			"sort_order" INTEGER NOT NULL DEFAULT 0,
			"created_at" TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			"updated_at" TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		return fmt.Errorf("create template label table: %w", err)
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_gocms_template_label_category ON "gocms_template_label" ("category_id", "sort_order", "id")`,
		`CREATE INDEX IF NOT EXISTS idx_gocms_template_label_context ON "gocms_template_label" ("context", "id")`,
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create template label index: %w", err)
		}
	}
	return nil
}

// ConvertTemplateVars converts template field tags to Go template placeholders.
func ConvertTemplateVars(content string) string {
	replacements := []struct {
		old string
		new string
	}{
		// 标准现代化字段标记
		{"[!--title--]", "{{.Title}}"},
		{"[!--oldtitle--]", "{{.Title}}"},
		{"[!--url--]", "{{.URL}}"},
		{"[!--titleurl--]", "{{.URL}}"},
		{"[!--image--]", "{{.Image}}"},
		{"[!--titlepic--]", "{{.Image}}"},
		{"[!--summary--]", "{{.Summary}}"},
		{"[!--smalltext--]", "{{.Summary}}"},
		{"[!--date--]", "{{.Date}}"},
		{"[!--newstime--]", "{{.Date}}"},
		{"[!--id--]", "{{.ID}}"},
		{"[!--index--]", "{{.Index}}"},
		{"[!--no.num--]", "{{.Index}}"},
		{"[!--category.name--]", "{{.CategoryName}}"},
		{"[!--class.name--]", "{{.CategoryName}}"},
		{"[!--this.classname--]", "{{.CategoryName}}"},
		{"[!--category.url--]", "{{.CategoryURL}}"},
		{"[!--this.classlink--]", "{{.CategoryURL}}"},
		{"[!--classurl--]", "{{.CategoryURL}}"},
		{"[!--the.classname--]", "{{.category_name}}"},
		{"[!--the.classid--]", "{{.category_id}}"},
		{"[!--the.classurl--]", "{{.category_url}}"},
		{"[!--site.name--]", `{{setting "site_name"}}`},
		{"[!--site.url--]", `{{setting "site_url"}}`},
	}
	result := content
	for _, r := range replacements {
		result = strings.ReplaceAll(result, r.old, r.new)
	}
	return result
}

func findListMarker(temptext string) string {
	markers := []string{
		"[!--list.temp--]",
		"[!--list.loop--]",
		"[!--listtemp--]",
	}
	for _, m := range markers {
		if strings.Contains(temptext, m) {
			return m
		}
	}
	return ""
}

// CompileContent combines temptext (页面模板内容) and listvar (列表内容模板) into an executable Go template.
func CompileContent(temptext, listvar string, rownum int) string {
	temptext = strings.TrimSpace(temptext)
	listvar = strings.TrimSpace(listvar)
	if temptext == "" || listvar == "" {
		return ""
	}
	listvar = ConvertTemplateVars(listvar)

	if marker := findListMarker(temptext); marker != "" {
		parts := strings.Split(temptext, marker)
		if len(parts) >= 3 {
			header := ConvertTemplateVars(parts[0])
			loopBody := parts[1]
			footer := ConvertTemplateVars(parts[2])

			compiledLoopBody := loopBody
			if strings.Contains(compiledLoopBody, "<!--list.var") {
				for i := 1; i <= 20; i++ {
					tag := fmt.Sprintf("<!--list.var%d-->", i)
					compiledLoopBody = strings.ReplaceAll(compiledLoopBody, tag, listvar)
				}
			} else {
				compiledLoopBody = listvar
			}

			var builder strings.Builder
			builder.WriteString(header)
			builder.WriteString("\n{{if listItems .}}\n{{range listItems .}}\n")
			builder.WriteString(compiledLoopBody)
			builder.WriteString("\n{{end}}\n{{else if .}}\n{{range .}}\n")
			builder.WriteString(compiledLoopBody)
			builder.WriteString("\n{{end}}\n{{end}}\n")
			builder.WriteString(footer)
			return strings.TrimSpace(builder.String())
		}
	}

	if strings.Contains(temptext, "<!--list.var") {
		result := temptext
		for i := 1; i <= 20; i++ {
			tag := fmt.Sprintf("<!--list.var%d-->", i)
			result = strings.ReplaceAll(result, tag, listvar)
		}
		return strings.TrimSpace(ConvertTemplateVars(result))
	}

	return strings.TrimSpace(ConvertTemplateVars(temptext))
}

func NormalizeInput(input Input) (Input, error) {
	input.Key = strings.ToLower(strings.TrimSpace(input.Key))
	input.Name = strings.TrimSpace(input.Name)
	input.Context = strings.TrimSpace(input.Context)
	input.Description = strings.TrimSpace(input.Description)
	input.Temptext = strings.TrimSpace(input.Temptext)
	input.Listvar = strings.TrimSpace(input.Listvar)
	input.Showdate = strings.TrimSpace(input.Showdate)
	if input.Showdate == "" {
		input.Showdate = "Y-m-d H:i:s"
	}
	if input.Rownum <= 0 {
		input.Rownum = 1
	}
	if input.Subnews < 0 {
		input.Subnews = 0
	}

	if input.Key == "" || !keyPattern.MatchString(input.Key) {
		return Input{}, fmt.Errorf("标签调用名只能以字母开头，并使用字母、数字、下划线或短横线")
	}
	if input.Name == "" {
		return Input{}, fmt.Errorf("标签模板名称不能为空")
	}
	if input.Temptext == "" {
		return Input{}, fmt.Errorf("页面模板内容不能为空")
	}
	if input.Listvar == "" {
		return Input{}, fmt.Errorf("列表内容模板(list.var)不能为空")
	}

	input.Content = CompileContent(input.Temptext, input.Listvar, input.Rownum)
	if strings.TrimSpace(input.Content) == "" {
		return Input{}, fmt.Errorf("标签模板编译内容不能为空")
	}
	if input.Context == "" {
		input.Context = ContextAny
	}
	if !validContext(input.Context) {
		return Input{}, fmt.Errorf("标签模板使用场景无效")
	}
	if input.CategoryID < 0 {
		input.CategoryID = 0
	}
	if input.SortOrder < 0 {
		input.SortOrder = 0
	}
	return input, nil
}

func validContext(value string) bool {
	switch value {
	case ContextAny, ContextHome, ContextCategory, ContextList, ContextDetail:
		return true
	default:
		return false
	}
}

func Categories(ctx context.Context, database *sql.DB) ([]Category, error) {
	rows, err := database.QueryContext(ctx, `
		SELECT c."id", c."name", c."sort_order", COUNT(l."id")
		FROM "gocms_template_label_category" c
		LEFT JOIN "gocms_template_label" l ON l."category_id" = c."id"
		GROUP BY c."id", c."name", c."sort_order"
		ORDER BY c."sort_order" ASC, c."id" ASC`)
	if err != nil {
		return nil, fmt.Errorf("read template label categories: %w", err)
	}
	defer rows.Close()
	items := make([]Category, 0)
	for rows.Next() {
		var item Category
		if err := rows.Scan(&item.ID, &item.Name, &item.SortOrder, &item.LabelCount); err != nil {
			return nil, fmt.Errorf("scan template label category: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read template label categories: %w", err)
	}
	return items, nil
}

func Count(ctx context.Context, database *sql.DB) (int64, error) {
	var total int64
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM "gocms_template_label"`).Scan(&total); err != nil {
		return 0, fmt.Errorf("count template labels: %w", err)
	}
	return total, nil
}

func List(ctx context.Context, database *sql.DB, query string, categoryID int64, page, pageSize int) (Page, error) {
	page = positive(page, 1)
	pageSize = positive(pageSize, 20)
	if pageSize > 100 {
		pageSize = 100
	}
	query = strings.TrimSpace(query)
	where := `1 = 1`
	args := make([]any, 0, 4)
	if query != "" {
		where += ` AND (l."key" LIKE ? OR l."name" LIKE ? OR l."description" LIKE ?)`
		pattern := "%" + query + "%"
		args = append(args, pattern, pattern, pattern)
	}
	if categoryID > 0 {
		where += ` AND l."category_id" = ?`
		args = append(args, categoryID)
	}
	var total int64
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM "gocms_template_label" l WHERE `+where, args...).Scan(&total); err != nil {
		return Page{}, fmt.Errorf("count template labels: %w", err)
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := database.QueryContext(ctx, `
		SELECT l."id", l."key", l."name", l."category_id", COALESCE(c."name", ''), l."context",
		       l."description", l."content", l."temptext", l."listvar", l."rownum", l."subnews", l."showdate",
		       l."sort_order", l."created_at", l."updated_at"
		FROM "gocms_template_label" l
		LEFT JOIN "gocms_template_label_category" c ON c."id" = l."category_id"
		WHERE `+where+`
		ORDER BY l."sort_order" ASC, l."id" DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return Page{}, fmt.Errorf("read template labels: %w", err)
	}
	defer rows.Close()
	items, err := scanLabels(rows, pageSize)
	if err != nil {
		return Page{}, err
	}
	return Page{Page: page, PageSize: pageSize, Total: total, Items: items}, nil
}

func Load(ctx context.Context, query queryer) ([]Label, error) {
	rows, err := query.QueryContext(ctx, `
		SELECT l."id", l."key", l."name", l."category_id", COALESCE(c."name", ''), l."context",
		       l."description", l."content", l."temptext", l."listvar", l."rownum", l."subnews", l."showdate",
		       l."sort_order", l."created_at", l."updated_at"
		FROM "gocms_template_label" l
		LEFT JOIN "gocms_template_label_category" c ON c."id" = l."category_id"
		ORDER BY l."sort_order" ASC, l."id" ASC`)
	if err != nil {
		return nil, fmt.Errorf("read template labels: %w", err)
	}
	defer rows.Close()
	return scanLabels(rows, 0)
}

type scanner interface {
	Scan(...any) error
}

func scanLabels(rows *sql.Rows, capacity int) ([]Label, error) {
	if capacity < 0 {
		capacity = 0
	}
	items := make([]Label, 0, capacity)
	for rows.Next() {
		var item Label
		if err := scanLabel(rows, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read template labels: %w", err)
	}
	return items, nil
}

func scanLabel(source scanner, item *Label) error {
	if err := source.Scan(
		&item.ID, &item.Key, &item.Name, &item.CategoryID, &item.CategoryName, &item.Context,
		&item.Description, &item.Content, &item.Temptext, &item.Listvar, &item.Rownum, &item.Subnews, &item.Showdate,
		&item.SortOrder, &item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return fmt.Errorf("scan template label: %w", err)
	}
	return nil
}

func Get(ctx context.Context, database *sql.DB, id int64) (Label, error) {
	var item Label
	err := database.QueryRowContext(ctx, `
		SELECT l."id", l."key", l."name", l."category_id", COALESCE(c."name", ''), l."context",
		       l."description", l."content", l."temptext", l."listvar", l."rownum", l."subnews", l."showdate",
		       l."sort_order", l."created_at", l."updated_at"
		FROM "gocms_template_label" l
		LEFT JOIN "gocms_template_label_category" c ON c."id" = l."category_id"
		WHERE l."id" = ?`, id).Scan(
		&item.ID, &item.Key, &item.Name, &item.CategoryID, &item.CategoryName, &item.Context,
		&item.Description, &item.Content, &item.Temptext, &item.Listvar, &item.Rownum, &item.Subnews, &item.Showdate,
		&item.SortOrder, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return Label{}, err
	}
	return item, nil
}

func Create(ctx context.Context, database *sql.DB, input Input) (Label, error) {
	input, err := NormalizeInput(input)
	if err != nil {
		return Label{}, err
	}
	if err := ValidateContent(input.Key, input.Content); err != nil {
		return Label{}, err
	}
	if err := validateCategory(ctx, database, input.CategoryID); err != nil {
		return Label{}, err
	}
	result, err := database.ExecContext(ctx, `
		INSERT INTO "gocms_template_label" ("key", "name", "category_id", "context", "description", "content", "temptext", "listvar", "rownum", "subnews", "showdate", "sort_order")
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, input.Key, input.Name, input.CategoryID, input.Context, input.Description, input.Content, input.Temptext, input.Listvar, input.Rownum, input.Subnews, input.Showdate, input.SortOrder)
	if err != nil {
		return Label{}, normalizeDatabaseError(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Label{}, fmt.Errorf("read template label id: %w", err)
	}
	return Get(ctx, database, id)
}

func Update(ctx context.Context, database *sql.DB, id int64, input Input) (Label, error) {
	if id < 1 {
		return Label{}, fmt.Errorf("标签模板编号无效")
	}
	input, err := NormalizeInput(input)
	if err != nil {
		return Label{}, err
	}
	if err := ValidateContent(input.Key, input.Content); err != nil {
		return Label{}, err
	}
	if err := validateCategory(ctx, database, input.CategoryID); err != nil {
		return Label{}, err
	}
	result, err := database.ExecContext(ctx, `
		UPDATE "gocms_template_label"
		SET "key" = ?, "name" = ?, "category_id" = ?, "context" = ?, "description" = ?, "content" = ?, "temptext" = ?, "listvar" = ?, "rownum" = ?, "subnews" = ?, "showdate" = ?, "sort_order" = ?, "updated_at" = CURRENT_TIMESTAMP
		WHERE "id" = ?`, input.Key, input.Name, input.CategoryID, input.Context, input.Description, input.Content, input.Temptext, input.Listvar, input.Rownum, input.Subnews, input.Showdate, input.SortOrder, id)
	if err != nil {
		return Label{}, normalizeDatabaseError(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Label{}, fmt.Errorf("check template label update: %w", err)
	}
	if affected == 0 {
		if _, err := Get(ctx, database, id); err == sql.ErrNoRows {
			return Label{}, fmt.Errorf("标签模板不存在")
		}
	}
	return Get(ctx, database, id)
}

func Delete(ctx context.Context, database *sql.DB, id int64) error {
	if id < 1 {
		return fmt.Errorf("标签模板编号无效")
	}
	result, err := database.ExecContext(ctx, `DELETE FROM "gocms_template_label" WHERE "id" = ?`, id)
	if err != nil {
		return fmt.Errorf("delete template label: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check template label deletion: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("标签模板不存在")
	}
	return nil
}

func validateCategory(ctx context.Context, database *sql.DB, id int64) error {
	if id == 0 {
		return nil
	}
	var exists int64
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM "gocms_template_label_category" WHERE "id" = ?`, id).Scan(&exists); err != nil {
		return fmt.Errorf("读取标签模板分类失败")
	}
	if exists == 0 {
		return fmt.Errorf("标签模板分类不存在")
	}
	return nil
}

func CreateCategory(ctx context.Context, database *sql.DB, name string, sortOrder int64) (Category, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Category{}, fmt.Errorf("标签模板分类名称不能为空")
	}
	if sortOrder < 0 {
		sortOrder = 0
	}
	result, err := database.ExecContext(ctx, `INSERT INTO "gocms_template_label_category" ("name", "sort_order") VALUES (?, ?)`, name, sortOrder)
	if err != nil {
		return Category{}, normalizeDatabaseError(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Category{}, fmt.Errorf("read template label category id: %w", err)
	}
	return GetCategory(ctx, database, id)
}

func GetCategory(ctx context.Context, database *sql.DB, id int64) (Category, error) {
	var item Category
	err := database.QueryRowContext(ctx, `
		SELECT c."id", c."name", c."sort_order", COUNT(l."id")
		FROM "gocms_template_label_category" c
		LEFT JOIN "gocms_template_label" l ON l."category_id" = c."id"
		WHERE c."id" = ?
		GROUP BY c."id", c."name", c."sort_order"`, id).
		Scan(&item.ID, &item.Name, &item.SortOrder, &item.LabelCount)
	if err != nil {
		return Category{}, err
	}
	return item, nil
}

func UpdateCategory(ctx context.Context, database *sql.DB, id, sortOrder int64, name string) (Category, error) {
	name = strings.TrimSpace(name)
	if id < 1 {
		return Category{}, fmt.Errorf("标签模板分类编号无效")
	}
	if name == "" {
		return Category{}, fmt.Errorf("标签模板分类名称不能为空")
	}
	if sortOrder < 0 {
		sortOrder = 0
	}
	result, err := database.ExecContext(ctx, `
		UPDATE "gocms_template_label_category" SET "name" = ?, "sort_order" = ?, "updated_at" = CURRENT_TIMESTAMP WHERE "id" = ?`, name, sortOrder, id)
	if err != nil {
		return Category{}, normalizeDatabaseError(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Category{}, fmt.Errorf("check template label category update: %w", err)
	}
	if affected == 0 {
		if _, err := GetCategory(ctx, database, id); err == sql.ErrNoRows {
			return Category{}, fmt.Errorf("标签模板分类不存在")
		}
	}
	return GetCategory(ctx, database, id)
}

func DeleteCategory(ctx context.Context, database *sql.DB, id int64) error {
	if id < 1 {
		return fmt.Errorf("标签模板分类编号无效")
	}
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin template label category deletion: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, `UPDATE "gocms_template_label" SET "category_id" = 0 WHERE "category_id" = ?`, id); err != nil {
		return fmt.Errorf("uncategorize template labels: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `DELETE FROM "gocms_template_label_category" WHERE "id" = ?`, id)
	if err != nil {
		return fmt.Errorf("delete template label category: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check template label category deletion: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("标签模板分类不存在")
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit template label category deletion: %w", err)
	}
	return nil
}

func ValidateContent(name, content string) error {
	_, err := template.New(name).Funcs(template.FuncMap{
		"setting":           func(...any) string { return "" },
		"settingHTML":       func(...any) string { return "" },
		"include":           func(...any) (string, error) { return "", nil },
		"label":             func(...any) (string, error) { return "", nil },
		"listItems":         func(...any) []any { return nil },
		"listCategories":    func(...any) []any { return nil },
		"listChildren":      func(...any) []any { return nil },
		"catalogCategories": func(...any) []any { return nil },
		"navigation":        func(...any) []any { return nil },
		"featuredItems":     func(...any) []any { return nil },
		"featuredItemsIn":   func(...any) []any { return nil },
		"relatedItems":      func(...any) []any { return nil },
		"listPagination":    func(...any) any { return nil },
	}).Parse(content)
	if err != nil {
		return fmt.Errorf("标签模板语法错误: %w", err)
	}
	return nil
}

func normalizeDatabaseError(err error) error {
	if strings.Contains(strings.ToLower(err.Error()), "unique") {
		return fmt.Errorf("标签调用名或分类名称已存在")
	}
	return fmt.Errorf("database error: %w", err)
}

func positive(value, fallback int) int {
	if value < 1 {
		return fallback
	}
	return value
}
