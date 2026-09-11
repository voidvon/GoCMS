package site

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"path"
	"strconv"
	"strings"

	"gocms/internal/db"
	"gocms/internal/routing"
)

// Content is the runtime content model. Historical source rows are imported
// into this shape once and are never read by the runtime afterward.
type Content struct {
	ID            int64                             `json:"id"`
	RouteKey      string                            `json:"route_key"`
	Category      int64                             `json:"category_id"`
	Title         string                            `json:"title"`
	Code          string                            `json:"code"`
	Summary       string                            `json:"summary"`
	Content       string                            `json:"content"`
	CoverImage    string                            `json:"cover_image"`
	PublishedAt   string                            `json:"published_at"`
	Source        string                            `json:"source"`
	Keywords      string                            `json:"keywords"`
	Description   string                            `json:"description"`
	OrderID       int64                             `json:"order_id"`
	Featured      int64                             `json:"featured"`
	Visible       int64                             `json:"visible"`
	ModelID       int64                             `json:"model_id"`
	ExtraData     map[string]any                    `json:"extra_data,omitempty"`
	URL           string                            `json:"url,omitempty"`
	Lang          string                            `json:"lang,omitempty"`
	PublishStatus string                            `json:"publish_status,omitempty"`
	IsFallback    bool                              `json:"is_fallback,omitempty"`
	FallbackLang  string                            `json:"fallback_lang,omitempty"`
	Translations  map[string]ContentTranslationItem `json:"translations,omitempty"`
}

type ContentPage struct {
	Query    string    `json:"query"`
	Page     int       `json:"page"`
	PageSize int       `json:"page_size"`
	Total    int64     `json:"total"`
	Items    []Content `json:"items"`
}

type contentPayload struct {
	Lang          string                            `json:"lang,omitempty"`
	Translations  map[string]ContentTranslationItem `json:"translations,omitempty"`
	Title         string                            `json:"title"`
	Code          string                            `json:"code"`
	Category      int64                             `json:"category_id"`
	Summary       string                            `json:"summary"`
	Content       string                            `json:"content"`
	CoverImage    string                            `json:"cover_image"`
	PublishedAt   string                            `json:"published_at"`
	Source        string                            `json:"source"`
	Keywords      string                            `json:"keywords"`
	Description   string                            `json:"description"`
	OrderID       int64                             `json:"order_id"`
	Featured      int64                             `json:"featured"`
	Visible       int64                             `json:"visible"`
	ModelID       int64                             `json:"model_id"`
	ExtraData     map[string]any                    `json:"extra_data"`
	PublishStatus string                            `json:"publish_status,omitempty"`
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
	lang := strings.TrimSpace(request.URL.Query().Get("lang"))
	page := positiveInt(request.URL.Query().Get("page"), 1)
	pageSize := positiveInt(request.URL.Query().Get("page_size"), 20)
	if pageSize > 100 {
		pageSize = 100
	}
	result, err := s.queryContent(request.Context(), query, categoryID, lang, page, pageSize, false)
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	if err := s.populateContentURLs(request.Context(), &result); err != nil {
		http.Error(response, "content route error", http.StatusInternalServerError)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (s *Server) populateContentURLs(ctx context.Context, page *ContentPage) error {
	for index := range page.Items {
		url, err := s.contentDetailURL(ctx, page.Items[index].Category, page.Items[index].RouteKey)
		if err != nil {
			return err
		}
		page.Items[index].URL = url
	}
	return nil
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
		lang := strings.TrimSpace(request.URL.Query().Get("lang"))
		item, err := s.readContent(request.Context(), id, lang, false)
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
		result, err := s.database.ExecContext(request.Context(), `DELETE FROM "gocms_content" WHERE "id" = ?`, id)
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
		_, _ = s.database.ExecContext(request.Context(), `DELETE FROM "`+db.ContentTranslationTable+`" WHERE "content_id" = ?`, id)
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
	if payload.Title == "" && payload.Translations != nil {
		defaultLang, _ := s.getDefaultAndFallbackLang(request.Context())
		if def, ok := payload.Translations[defaultLang]; ok && strings.TrimSpace(def.Title) != "" {
			payload.Title = strings.TrimSpace(def.Title)
			payload.Summary = def.Summary
			payload.Content = def.Content
			payload.Keywords = def.Keywords
			payload.Description = def.Description
		}
	}
	if payload.Title == "" {
		http.Error(response, "content title is required", http.StatusBadRequest)
		return
	}
	if payload.OrderID < 0 {
		payload.OrderID = 0
	}
	payload.Featured = normalizeFlag(payload.Featured)
	payload.Visible = normalizeFlag(payload.Visible)
	payload.PublishedAt = normalizeContentDate(payload.PublishedAt)
	if err := s.validateContentCategory(request.Context(), payload.Category); err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}

	modelID := payload.ModelID
	if modelID == 0 {
		_ = s.database.QueryRowContext(request.Context(), `SELECT COALESCE("model_id", 1) FROM "gocms_category" WHERE "id" = ?`, payload.Category).Scan(&modelID)
		if modelID == 0 {
			modelID = 1
		}
	}
	if payload.ExtraData == nil {
		payload.ExtraData = make(map[string]any)
	}
	extraBytes, _ := json.Marshal(payload.ExtraData)
	extraDataStr := string(extraBytes)

	requestedLang := strings.TrimSpace(payload.Lang)
	if requestedLang == "" {
		requestedLang = strings.TrimSpace(request.URL.Query().Get("lang"))
	}
	defaultLang, _ := s.getDefaultAndFallbackLang(request.Context())
	if requestedLang == "" {
		requestedLang = defaultLang
	}

	if id == 0 {
		transaction, err := s.database.BeginTx(request.Context(), nil)
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		defer transaction.Rollback()
		result, err := transaction.ExecContext(request.Context(), `
			INSERT INTO "gocms_content"
			("category_id", "route_key", "title", "code", "summary", "body", "cover_image", "published_at", "source", "keywords", "description", "sort_order", "featured", "visible", "model_id", "extra_data")
			VALUES (?, '', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			payload.Category, payload.Title, payload.Code, payload.Summary, payload.Content, payload.CoverImage,
			payload.PublishedAt, payload.Source, payload.Keywords, payload.Description, payload.OrderID, payload.Featured, payload.Visible,
			modelID, extraDataStr)
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		newID, err := result.LastInsertId()
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		if _, err := transaction.ExecContext(request.Context(), `UPDATE "gocms_content" SET "route_key" = ? WHERE "id" = ?`, strconv.FormatInt(newID, 10), newID); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		if payload.Translations != nil && len(payload.Translations) > 0 {
			_ = s.saveContentTranslations(request.Context(), transaction, newID, payload.Translations)
		} else {
			transFields := s.getTranslatableFieldNames(request.Context(), transaction, modelID)
			transExtra := make(map[string]any)
			for k, v := range payload.ExtraData {
				if transFields[k] {
					transExtra[k] = v
				}
			}
			_ = s.saveContentTranslations(request.Context(), transaction, newID, map[string]ContentTranslationItem{
				requestedLang: {
					Title:         payload.Title,
					Summary:       payload.Summary,
					Content:       payload.Content,
					Keywords:      payload.Keywords,
					Description:   payload.Description,
					ExtraData:     transExtra,
					PublishStatus: payload.PublishStatus,
				},
			})
		}
		if err := syncContentMediaRefs(request.Context(), transaction, newID, payload.Content, payload.CoverImage); err != nil {
			http.Error(response, "media reference error", http.StatusInternalServerError)
			return
		}
		if err := transaction.Commit(); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
	} else {
		transaction, err := s.database.BeginTx(request.Context(), nil)
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		defer transaction.Rollback()

		if payload.Translations != nil && len(payload.Translations) > 0 {
			_ = s.saveContentTranslations(request.Context(), transaction, id, payload.Translations)
			if def, ok := payload.Translations[defaultLang]; ok && strings.TrimSpace(def.Title) != "" {
				payload.Title = def.Title
				payload.Summary = def.Summary
				payload.Content = def.Content
				payload.Keywords = def.Keywords
				payload.Description = def.Description
			}
		} else {
			transFields := s.getTranslatableFieldNames(request.Context(), transaction, modelID)
			transExtra := make(map[string]any)
			for k, v := range payload.ExtraData {
				if transFields[k] {
					transExtra[k] = v
				}
			}
			_ = s.saveContentTranslations(request.Context(), transaction, id, map[string]ContentTranslationItem{
				requestedLang: {
					Title:         payload.Title,
					Summary:       payload.Summary,
					Content:       payload.Content,
					Keywords:      payload.Keywords,
					Description:   payload.Description,
					ExtraData:     transExtra,
					PublishStatus: payload.PublishStatus,
				},
			})
		}

		if requestedLang == defaultLang || payload.Translations != nil {
			result, err := transaction.ExecContext(request.Context(), `
				UPDATE "gocms_content" SET "category_id" = ?, "title" = ?, "code" = ?, "summary" = ?, "body" = ?,
				"cover_image" = ?, "published_at" = ?, "source" = ?, "keywords" = ?, "description" = ?,
				"sort_order" = ?, "featured" = ?, "visible" = ?, "model_id" = ?, "extra_data" = ? WHERE "id" = ?`,
				payload.Category, payload.Title, payload.Code, payload.Summary, payload.Content, payload.CoverImage,
				payload.PublishedAt, payload.Source, payload.Keywords, payload.Description, payload.OrderID, payload.Featured, payload.Visible,
				modelID, extraDataStr, id)
			if err != nil {
				http.Error(response, "database error", http.StatusInternalServerError)
				return
			}
			affected, err := result.RowsAffected()
			if err != nil || affected == 0 {
				http.Error(response, "content not found", http.StatusNotFound)
				return
			}
		} else {
			result, err := transaction.ExecContext(request.Context(), `
				UPDATE "gocms_content" SET "category_id" = ?, "code" = ?,
				"cover_image" = ?, "published_at" = ?, "source" = ?,
				"sort_order" = ?, "featured" = ?, "visible" = ?, "model_id" = ?, "extra_data" = ? WHERE "id" = ?`,
				payload.Category, payload.Code, payload.CoverImage,
				payload.PublishedAt, payload.Source, payload.OrderID, payload.Featured, payload.Visible,
				modelID, extraDataStr, id)
			if err != nil {
				http.Error(response, "database error", http.StatusInternalServerError)
				return
			}
			affected, err := result.RowsAffected()
			if err != nil || affected == 0 {
				http.Error(response, "content not found", http.StatusNotFound)
				return
			}
		}

		if err := syncContentMediaRefs(request.Context(), transaction, id, payload.Content, payload.CoverImage); err != nil {
			http.Error(response, "media reference error", http.StatusInternalServerError)
			return
		}
		if err := transaction.Commit(); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
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

func (s *Server) queryContent(ctx context.Context, query string, categoryID int64, lang string, page, pageSize int, visibleOnly bool) (ContentPage, error) {
	defaultLang, fallbackLang := s.getDefaultAndFallbackLang(ctx)
	if lang == "" {
		lang = defaultLang
	}

	where := "1=1"
	var args []any
	if visibleOnly {
		where += ` AND "visible" = 1`
	}
	if categoryID > 0 {
		where += ` AND "category_id" = ?`
		args = append(args, categoryID)
	}
	if query != "" {
		where += ` AND ("title" LIKE ? OR "summary" LIKE ? OR "body" LIKE ? OR "code" LIKE ?)`
		pattern := "%" + query + "%"
		args = append(args, pattern, pattern, pattern, pattern)
	}
	var total int64
	if err := s.database.QueryRowContext(ctx, `SELECT COUNT(*) FROM "gocms_content" WHERE `+where, args...).Scan(&total); err != nil {
		return ContentPage{}, err
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.database.QueryContext(ctx, `
		SELECT "id", COALESCE("route_key", ''), COALESCE("category_id", 0), COALESCE("title", ''),
		       COALESCE("code", ''), COALESCE("summary", ''), COALESCE("body", ''), COALESCE("cover_image", ''),
		       COALESCE("published_at", ''), COALESCE("source", ''), COALESCE("keywords", ''), COALESCE("description", ''),
		       COALESCE("sort_order", 0), COALESCE("featured", 0), COALESCE("visible", 0),
		       COALESCE("model_id", 1), COALESCE("extra_data", '{}')
		FROM "gocms_content" WHERE `+where+` ORDER BY "sort_order" ASC, "id" DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return ContentPage{}, err
	}
	items := make([]Content, 0, pageSize)
	for rows.Next() {
		item, err := scanContent(rows)
		if err != nil {
			return ContentPage{}, err
		}
		items = append(items, item)
	}
	_ = rows.Close()

	if len(items) > 0 {
		ids := make([]int64, len(items))
		for i := range items {
			ids[i] = items[i].ID
		}
		allTrans, _ := s.loadContentTranslationsForIDs(ctx, ids)
		for i := range items {
			s.applyContentTranslation(ctx, &items[i], allTrans[items[i].ID], lang, defaultLang, fallbackLang)
		}
	}
	return ContentPage{Query: query, Page: page, PageSize: pageSize, Total: total, Items: items}, nil
}

func (s *Server) applyContentTranslation(ctx context.Context, item *Content, translations map[string]ContentTranslationItem, lang, defaultLang, fallbackLang string) {
	if translations == nil {
		translations = make(map[string]ContentTranslationItem)
	}
	if _, ok := translations[defaultLang]; !ok {
		translations[defaultLang] = ContentTranslationItem{
			Title:         item.Title,
			Summary:       item.Summary,
			Content:       item.Content,
			Keywords:      item.Keywords,
			Description:   item.Description,
			ExtraData:     make(map[string]any),
			PublishStatus: "published",
		}
	}

	transFields := s.getTranslatableFieldNames(ctx, nil, item.ModelID)
	trans := translations[lang]
	fallbackTrans := translations[fallbackLang]

	if strings.TrimSpace(trans.Title) != "" {
		item.Title = trans.Title
	} else if strings.TrimSpace(fallbackTrans.Title) != "" {
		item.Title = fallbackTrans.Title
		if lang != fallbackLang {
			item.IsFallback = true
			item.FallbackLang = fallbackLang
		}
	}

	if strings.TrimSpace(trans.Summary) != "" {
		item.Summary = trans.Summary
	} else if strings.TrimSpace(fallbackTrans.Summary) != "" {
		item.Summary = fallbackTrans.Summary
	}

	if strings.TrimSpace(trans.Content) != "" {
		item.Content = trans.Content
	} else if strings.TrimSpace(fallbackTrans.Content) != "" {
		item.Content = fallbackTrans.Content
	}

	if strings.TrimSpace(trans.Keywords) != "" {
		item.Keywords = trans.Keywords
	} else if strings.TrimSpace(fallbackTrans.Keywords) != "" {
		item.Keywords = fallbackTrans.Keywords
	}

	if strings.TrimSpace(trans.Description) != "" {
		item.Description = trans.Description
	} else if strings.TrimSpace(fallbackTrans.Description) != "" {
		item.Description = fallbackTrans.Description
	}

	if trans.PublishStatus != "" {
		item.PublishStatus = trans.PublishStatus
	} else if fallbackTrans.PublishStatus != "" {
		item.PublishStatus = fallbackTrans.PublishStatus
	} else {
		item.PublishStatus = "published"
	}

	for k := range transFields {
		if val, exists := trans.ExtraData[k]; exists && val != nil && val != "" {
			item.ExtraData[k] = val
		} else if val, fExists := fallbackTrans.ExtraData[k]; fExists && val != nil && val != "" {
			item.ExtraData[k] = val
		}
	}

	item.Lang = lang
	item.Translations = translations
}

type contentScanner interface {
	Scan(...any) error
}

func scanContent(scanner contentScanner) (Content, error) {
	var item Content
	var extraJSON string
	err := scanner.Scan(
		&item.ID, &item.RouteKey, &item.Category, &item.Title, &item.Code, &item.Summary,
		&item.Content, &item.CoverImage, &item.PublishedAt, &item.Source, &item.Keywords,
		&item.Description, &item.OrderID, &item.Featured, &item.Visible,
		&item.ModelID, &extraJSON,
	)
	if err == nil {
		item.ExtraData = make(map[string]any)
		if strings.TrimSpace(extraJSON) != "" {
			_ = json.Unmarshal([]byte(extraJSON), &item.ExtraData)
		}
	}
	return item, err
}

func (s *Server) readContent(ctx context.Context, id int64, lang string, visibleOnly bool) (Content, error) {
	defaultLang, fallbackLang := s.getDefaultAndFallbackLang(ctx)
	if lang == "" {
		lang = defaultLang
	}
	where := `"id" = ?`
	if visibleOnly {
		where += ` AND "visible" = 1`
	}
	row := s.database.QueryRowContext(ctx, `
		SELECT "id", COALESCE("route_key", ''), COALESCE("category_id", 0), COALESCE("title", ''),
		       COALESCE("code", ''), COALESCE("summary", ''), COALESCE("body", ''), COALESCE("cover_image", ''),
		       COALESCE("published_at", ''), COALESCE("source", ''), COALESCE("keywords", ''), COALESCE("description", ''),
		       COALESCE("sort_order", 0), COALESCE("featured", 0), COALESCE("visible", 0),
		       COALESCE("model_id", 1), COALESCE("extra_data", '{}')
		FROM "gocms_content" WHERE `+where, id)
	item, err := scanContent(row)
	if err != nil {
		return Content{}, err
	}
	trans, _ := s.loadContentTranslations(ctx, item.ID)
	s.applyContentTranslation(ctx, &item, trans, lang, defaultLang, fallbackLang)
	return item, nil
}

func (s *Server) contentJSON(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(response)
		return
	}
	query := strings.TrimSpace(request.URL.Query().Get("q"))
	lang := strings.TrimSpace(request.URL.Query().Get("lang"))
	result, err := s.queryContent(request.Context(), query, 0, lang, positiveInt(request.URL.Query().Get("page"), 1), positiveInt(request.URL.Query().Get("page_size"), 20), true)
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
	lang := strings.TrimSpace(request.URL.Query().Get("lang"))
	item, err := s.readContent(request.Context(), id, lang, true)
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
			FROM "gocms_category" WHERE "id" = ?`, categoryID).Scan(&storedDirectory, &storedPattern)
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
