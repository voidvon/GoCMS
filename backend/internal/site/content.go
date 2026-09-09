package site

import (
	"context"
	"database/sql"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"bilvie/internal/routing"
)

const defaultContentImage = "/images/content-placeholder.jpg"

// Content is the runtime content model. Historical source rows are imported
// into this shape once and are never read by the runtime afterward.
type Content struct {
	ID          int64  `json:"id"`
	RouteKey    string `json:"route_key"`
	Category    int64  `json:"category_id"`
	Title       string `json:"title"`
	Code        string `json:"code"`
	Summary     string `json:"summary"`
	Content     string `json:"content"`
	CoverImage  string `json:"cover_image"`
	PublishedAt string `json:"published_at"`
	Source      string `json:"source"`
	Keywords    string `json:"keywords"`
	Description string `json:"description"`
	OrderID     int64  `json:"order_id"`
	Featured    int64  `json:"featured"`
	Visible     int64  `json:"visible"`
}

type ContentPage struct {
	Query    string    `json:"query"`
	Page     int       `json:"page"`
	PageSize int       `json:"page_size"`
	Total    int64     `json:"total"`
	Items    []Content `json:"items"`
}

type contentPayload struct {
	Title       string `json:"title"`
	Code        string `json:"code"`
	Category    int64  `json:"category_id"`
	Summary     string `json:"summary"`
	Content     string `json:"content"`
	CoverImage  string `json:"cover_image"`
	PublishedAt string `json:"published_at"`
	Source      string `json:"source"`
	Keywords    string `json:"keywords"`
	Description string `json:"description"`
	OrderID     int64  `json:"order_id"`
	Featured    int64  `json:"featured"`
	Visible     int64  `json:"visible"`
}

func (s *Server) adminContent(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodPost {
		methodNotAllowed(response)
		return
	}
	if !s.requireAdmin(response, request) {
		return
	}
	if request.Method == http.MethodPost {
		s.saveContent(response, request, 0)
		return
	}

	query := strings.TrimSpace(request.URL.Query().Get("q"))
	categoryID := parseIntOrZero(request.URL.Query().Get("category_id"))
	page := positiveInt(request.URL.Query().Get("page"), 1)
	pageSize := positiveInt(request.URL.Query().Get("page_size"), 20)
	if pageSize > 100 {
		pageSize = 100
	}
	result, err := s.queryContent(request.Context(), query, categoryID, page, pageSize, false)
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (s *Server) adminContentItem(response http.ResponseWriter, request *http.Request, rawID string) {
	if request.Method != http.MethodGet && request.Method != http.MethodPut && request.Method != http.MethodPatch && request.Method != http.MethodDelete {
		methodNotAllowed(response)
		return
	}
	if !s.requireAdmin(response, request) {
		return
	}
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id < 1 {
		http.Error(response, "invalid content id", http.StatusBadRequest)
		return
	}
	if request.Method == http.MethodGet {
		item, err := s.readContent(request.Context(), id, false)
		if err == sql.ErrNoRows {
			http.Error(response, "content not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, item)
		return
	}
	if request.Method == http.MethodDelete {
		result, err := s.database.ExecContext(request.Context(), `DELETE FROM "bilvie_content" WHERE "id" = ?`, id)
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		affected, err := result.RowsAffected()
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		if affected == 0 {
			http.Error(response, "content not found", http.StatusNotFound)
			return
		}
		s.contentSaved(response, request)
		return
	}
	s.saveContent(response, request, id)
}

func (s *Server) saveContent(response http.ResponseWriter, request *http.Request, id int64) {
	var payload contentPayload
	if err := decodeRequest(request, &payload); err != nil {
		http.Error(response, "invalid content payload", http.StatusBadRequest)
		return
	}
	payload.Title = strings.TrimSpace(payload.Title)
	if payload.Title == "" {
		http.Error(response, "content title is required", http.StatusBadRequest)
		return
	}
	if payload.OrderID < 0 {
		payload.OrderID = 0
	}
	payload.Featured = normalizeFlag(payload.Featured)
	payload.Visible = normalizeFlag(payload.Visible)
	payload.CoverImage = canonicalImageURL(payload.CoverImage)
	payload.PublishedAt = normalizeContentDate(payload.PublishedAt)
	if err := s.validateContentCategory(request.Context(), payload.Category); err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}

	if id == 0 {
		transaction, err := s.database.BeginTx(request.Context(), nil)
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		defer transaction.Rollback()
		result, err := transaction.ExecContext(request.Context(), `
			INSERT INTO "bilvie_content"
			("category_id", "route_key", "title", "code", "summary", "body", "cover_image", "published_at", "source", "keywords", "description", "sort_order", "featured", "visible")
			VALUES (?, '', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			payload.Category, payload.Title, payload.Code, payload.Summary, payload.Content, payload.CoverImage,
			payload.PublishedAt, payload.Source, payload.Keywords, payload.Description, payload.OrderID, payload.Featured, payload.Visible)
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		newID, err := result.LastInsertId()
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		if _, err := transaction.ExecContext(request.Context(), `UPDATE "bilvie_content" SET "route_key" = ? WHERE "id" = ?`, strconv.FormatInt(newID, 10), newID); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		if err := transaction.Commit(); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
	} else {
		result, err := s.database.ExecContext(request.Context(), `
			UPDATE "bilvie_content" SET "category_id" = ?, "title" = ?, "code" = ?, "summary" = ?, "body" = ?,
			"cover_image" = ?, "published_at" = ?, "source" = ?, "keywords" = ?, "description" = ?,
			"sort_order" = ?, "featured" = ?, "visible" = ? WHERE "id" = ?`,
			payload.Category, payload.Title, payload.Code, payload.Summary, payload.Content, payload.CoverImage,
			payload.PublishedAt, payload.Source, payload.Keywords, payload.Description, payload.OrderID, payload.Featured, payload.Visible, id)
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		affected, err := result.RowsAffected()
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		if affected == 0 {
			http.Error(response, "content not found", http.StatusNotFound)
			return
		}
	}
	s.contentSaved(response, request)
}

func normalizeFlag(value int64) int64 {
	if value == 1 {
		return 1
	}
	return 0
}

func normalizeContentDate(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == len("2006-01-02T15:04") && strings.Contains(value, "T") {
		return strings.Replace(value, "T", " ", 1) + ":00"
	}
	return value
}

func (s *Server) queryContent(ctx context.Context, query string, categoryID int64, page, pageSize int, visibleOnly bool) (ContentPage, error) {
	where := "1 = 1"
	args := make([]any, 0, 4)
	if visibleOnly {
		where += ` AND "visible" = 1`
	}
	if categoryID > 0 {
		where += ` AND "category_id" = ?`
		args = append(args, categoryID)
	}
	if query != "" && query != "输入内容名称" {
		where += ` AND ("title" LIKE ? OR "code" LIKE ? OR "keywords" LIKE ? OR "summary" LIKE ?)`
		pattern := "%" + query + "%"
		args = append(args, pattern, pattern, pattern, pattern)
	} else if query == "输入内容名称" {
		query = ""
	}
	var total int64
	if err := s.database.QueryRowContext(ctx, `SELECT COUNT(*) FROM "bilvie_content" WHERE `+where, args...).Scan(&total); err != nil {
		return ContentPage{}, err
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.database.QueryContext(ctx, `
		SELECT "id", COALESCE("route_key", ''), COALESCE("category_id", 0), COALESCE("title", ''),
		       COALESCE("code", ''), COALESCE("summary", ''), COALESCE("body", ''), COALESCE("cover_image", ''),
		       COALESCE("published_at", ''), COALESCE("source", ''), COALESCE("keywords", ''), COALESCE("description", ''),
		       COALESCE("sort_order", 0), COALESCE("featured", 0), COALESCE("visible", 0)
		FROM "bilvie_content" WHERE `+where+` ORDER BY "sort_order" ASC, "id" DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return ContentPage{}, err
	}
	defer rows.Close()
	items := make([]Content, 0, pageSize)
	for rows.Next() {
		item, err := scanContent(rows)
		if err != nil {
			return ContentPage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ContentPage{}, err
	}
	return ContentPage{Query: query, Page: page, PageSize: pageSize, Total: total, Items: items}, nil
}

type contentScanner interface {
	Scan(...any) error
}

func scanContent(scanner contentScanner) (Content, error) {
	var item Content
	err := scanner.Scan(
		&item.ID, &item.RouteKey, &item.Category, &item.Title, &item.Code, &item.Summary,
		&item.Content, &item.CoverImage, &item.PublishedAt, &item.Source, &item.Keywords,
		&item.Description, &item.OrderID, &item.Featured, &item.Visible,
	)
	return item, err
}

func (s *Server) readContent(ctx context.Context, id int64, visibleOnly bool) (Content, error) {
	where := `"id" = ?`
	if visibleOnly {
		where += ` AND "visible" = 1`
	}
	row := s.database.QueryRowContext(ctx, `
		SELECT "id", COALESCE("route_key", ''), COALESCE("category_id", 0), COALESCE("title", ''),
		       COALESCE("code", ''), COALESCE("summary", ''), COALESCE("body", ''), COALESCE("cover_image", ''),
		       COALESCE("published_at", ''), COALESCE("source", ''), COALESCE("keywords", ''), COALESCE("description", ''),
		       COALESCE("sort_order", 0), COALESCE("featured", 0), COALESCE("visible", 0)
		FROM "bilvie_content" WHERE `+where, id)
	return scanContent(row)
}

func (s *Server) contentJSON(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(response)
		return
	}
	query := strings.TrimSpace(request.URL.Query().Get("q"))
	result, err := s.queryContent(request.Context(), query, 0, positiveInt(request.URL.Query().Get("page"), 1), positiveInt(request.URL.Query().Get("page_size"), 20), true)
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (s *Server) contentJSONItem(response http.ResponseWriter, request *http.Request, rawID string) {
	if request.Method != http.MethodGet {
		methodNotAllowed(response)
		return
	}
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id < 1 {
		http.Error(response, "invalid content id", http.StatusBadRequest)
		return
	}
	item, err := s.readContent(request.Context(), id, true)
	if err == sql.ErrNoRows {
		http.NotFound(response, request)
		return
	}
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(response, http.StatusOK, item)
}

func (s *Server) contentDetailURL(ctx context.Context, categoryID int64, routeKey string) (string, error) {
	directory := routing.DefaultDetailPath
	pattern := routing.DefaultDetailPattern
	if categoryID > 0 {
		var storedDirectory, storedPattern string
		err := s.database.QueryRowContext(ctx, `
			SELECT COALESCE("detail_path", ''), COALESCE("detail_file_pattern", '')
			FROM "bilvie_category" WHERE "id" = ?`, categoryID).Scan(&storedDirectory, &storedPattern)
		if err != nil && err != sql.ErrNoRows {
			return "", err
		}
		if storedDirectory != "" {
			directory = storedDirectory
		}
		if storedPattern != "" {
			pattern = storedPattern
		}
	}
	directory, err := routing.NormalizeDirectory(directory)
	if err != nil {
		return "", err
	}
	filename, err := routing.RenderDetailFilenameValue(pattern, routeKey)
	if err != nil {
		return "", err
	}
	return path.Join("/", directory, filename), nil
}

func (s *Server) searchHTML(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodPost {
		methodNotAllowed(response)
		return
	}
	if err := request.ParseForm(); err != nil {
		http.Error(response, "invalid form", http.StatusBadRequest)
		return
	}
	query := strings.TrimSpace(request.FormValue("q"))
	result, err := s.queryContent(request.Context(), query, 0, positiveInt(request.FormValue("page"), 1), 12, true)
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(response, `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>站内搜索</title><link rel="stylesheet" href="/css/c.css"><style>body{font-family:Arial,"Microsoft YaHei",sans-serif;margin:0;color:#333}.wrap{width:min(980px,calc(100% - 32px));margin:32px auto}.search{display:flex;gap:8px;margin:20px 0}.search input{flex:1;padding:10px;border:1px solid #ccc}.search button{padding:10px 20px}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(300px,1fr));gap:16px}.item{display:flex;gap:12px;border-bottom:1px dashed #ccc;padding:12px 0}.item img{width:120px;height:90px;object-fit:contain}.item h2{font-size:16px;margin:0 0 8px}.item p{font-size:13px;line-height:1.6;margin:0}.pages{margin:24px 0}.pages a{margin-right:12px}</style></head><body><main class="wrap"><a href="/">返回首页</a><h1>站内搜索</h1><form class="search" method="post" action="/search.asp?action=search"><input name="q" value="`)
	fmt.Fprint(response, html.EscapeString(query))
	fmt.Fprint(response, `" placeholder="输入内容名称"><button type="submit">搜索</button></form>`)
	fmt.Fprintf(response, `<p>“%s”共找到 %d 条内容</p>`, html.EscapeString(query), result.Total)
	fmt.Fprint(response, `<section class="grid">`)
	for _, item := range result.Items {
		image := item.CoverImage
		if image == "" {
			image = defaultContentImage
		}
		contentURL, err := s.contentDetailURL(request.Context(), item.Category, item.RouteKey)
		if err != nil {
			http.Error(response, "content route error", http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(response, `<article class="item"><img src="%s" alt="%s"><div><h2><a href="%s">%s</a></h2><p>%s</p></div></article>`, html.EscapeString(image), html.EscapeString(item.Title), html.EscapeString(contentURL), html.EscapeString(item.Title), html.EscapeString(snippet(item.Summary, 150)))
	}
	fmt.Fprint(response, `</section><nav class="pages">`)
	lastPage := (result.Total + int64(result.PageSize) - 1) / int64(result.PageSize)
	if result.Page > 1 {
		fmt.Fprintf(response, `<a href="/search.asp?q=%s&page=%d">上一页</a>`, url.QueryEscape(query), result.Page-1)
	}
	if int64(result.Page) < lastPage {
		fmt.Fprintf(response, `<a href="/search.asp?q=%s&page=%d">下一页</a>`, url.QueryEscape(query), result.Page+1)
	}
	fmt.Fprint(response, `</nav></main></body></html>`)
}
