package site

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"gocms/internal/db"
)

type ContentTranslationItem struct {
	Title         string         `json:"title"`
	Summary       string         `json:"summary"`
	Content       string         `json:"content"`
	Keywords      string         `json:"keywords"`
	Description   string         `json:"description"`
	ExtraData     map[string]any `json:"extra_data,omitempty"`
	PublishStatus string         `json:"publish_status,omitempty"`
}

type CategoryTranslationItem struct {
	Name         string `json:"name"`
	Keywords     string `json:"keywords"`
	Description  string `json:"description"`
	CoverContent string `json:"cover_content"`
}

type dbQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *Server) getTranslatableFieldNames(ctx context.Context, queryer dbQueryer, modelID int64) map[string]bool {
	if queryer == nil {
		queryer = s.database
	}
	result := map[string]bool{
		"title":       true,
		"summary":     true,
		"body":        true,
		"keywords":    true,
		"description": true,
	}

	var tableID int64
	_ = queryer.QueryRowContext(ctx, `SELECT "table_id" FROM "`+db.ModelTable+`" WHERE "id" = ?`, modelID).Scan(&tableID)
	if tableID < 1 {
		return result
	}

	rows, err := queryer.QueryContext(ctx, `
		SELECT "field_name", "is_translatable"
		FROM "`+db.ModelFieldTable+`"
		WHERE "table_id" = ?`, tableID)
	if err != nil {
		return result
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		var isTrans int
		if err := rows.Scan(&name, &isTrans); err == nil && isTrans == 1 {
			result[name] = true
		}
	}
	return result
}

func (s *Server) loadCategoryTranslations(ctx context.Context, categoryID int64) (map[string]CategoryTranslationItem, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT "lang", "name", "keywords", "description", "cover_content"
		FROM "`+db.CategoryTranslationTable+`"
		WHERE "category_id" = ?`, categoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	translations := make(map[string]CategoryTranslationItem)
	for rows.Next() {
		var lang, name, keywords, desc, cover string
		if err := rows.Scan(&lang, &name, &keywords, &desc, &cover); err == nil {
			translations[lang] = CategoryTranslationItem{
				Name:         name,
				Keywords:     keywords,
				Description:  desc,
				CoverContent: cover,
			}
		}
	}
	return translations, nil
}

func (s *Server) loadAllCategoryTranslations(ctx context.Context) (map[int64]map[string]CategoryTranslationItem, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT "category_id", "lang", "name", "keywords", "description", "cover_content"
		FROM "`+db.CategoryTranslationTable+`"`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64]map[string]CategoryTranslationItem)
	for rows.Next() {
		var catID int64
		var lang, name, keywords, desc, cover string
		if err := rows.Scan(&catID, &lang, &name, &keywords, &desc, &cover); err == nil {
			if result[catID] == nil {
				result[catID] = make(map[string]CategoryTranslationItem)
			}
			result[catID][lang] = CategoryTranslationItem{
				Name:         name,
				Keywords:     keywords,
				Description:  desc,
				CoverContent: cover,
			}
		}
	}
	return result, nil
}

func (s *Server) loadContentTranslationsForIDs(ctx context.Context, contentIDs []int64) (map[int64]map[string]ContentTranslationItem, error) {
	result := make(map[int64]map[string]ContentTranslationItem)
	if len(contentIDs) == 0 {
		return result, nil
	}

	placeholders := make([]string, len(contentIDs))
	args := make([]any, len(contentIDs))
	for i, id := range contentIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	rows, err := s.database.QueryContext(ctx, `
		SELECT "content_id", "lang", "title", "summary", "body", "keywords", "description", "extra_data", "publish_status"
		FROM "`+db.ContentTranslationTable+`"
		WHERE "content_id" IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var contentID int64
		var lang, title, summary, body, keywords, desc, extraStr, status string
		if err := rows.Scan(&contentID, &lang, &title, &summary, &body, &keywords, &desc, &extraStr, &status); err == nil {
			extra := make(map[string]any)
			if strings.TrimSpace(extraStr) != "" {
				_ = json.Unmarshal([]byte(extraStr), &extra)
			}
			if status == "" {
				status = "published"
			}
			if result[contentID] == nil {
				result[contentID] = make(map[string]ContentTranslationItem)
			}
			result[contentID][lang] = ContentTranslationItem{
				Title:         title,
				Summary:       summary,
				Content:       body,
				Keywords:      keywords,
				Description:   desc,
				ExtraData:     extra,
				PublishStatus: status,
			}
		}
	}
	return result, nil
}

func (s *Server) loadContentTranslations(ctx context.Context, contentID int64) (map[string]ContentTranslationItem, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT "lang", "title", "summary", "body", "keywords", "description", "extra_data", "publish_status"
		FROM "`+db.ContentTranslationTable+`"
		WHERE "content_id" = ?`, contentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	translations := make(map[string]ContentTranslationItem)
	for rows.Next() {
		var lang, title, summary, body, keywords, desc, extraStr, status string
		if err := rows.Scan(&lang, &title, &summary, &body, &keywords, &desc, &extraStr, &status); err == nil {
			extra := make(map[string]any)
			if strings.TrimSpace(extraStr) != "" {
				_ = json.Unmarshal([]byte(extraStr), &extra)
			}
			if status == "" {
				status = "published"
			}
			translations[lang] = ContentTranslationItem{
				Title:         title,
				Summary:       summary,
				Content:       body,
				Keywords:      keywords,
				Description:   desc,
				ExtraData:     extra,
				PublishStatus: status,
			}
		}
	}
	return translations, nil
}

func (s *Server) saveCategoryTranslations(ctx context.Context, execer dbExecer, categoryID int64, translations map[string]CategoryTranslationItem) error {
	for lang, trans := range translations {
		lang = strings.TrimSpace(lang)
		if lang == "" {
			continue
		}
		_, err := execer.ExecContext(ctx, `
			INSERT INTO "`+db.CategoryTranslationTable+`"
			("category_id", "lang", "name", "keywords", "description", "cover_content")
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT("category_id", "lang") DO UPDATE SET
			"name" = excluded."name",
			"keywords" = excluded."keywords",
			"description" = excluded."description",
			"cover_content" = excluded."cover_content"`,
			categoryID, lang, trans.Name, trans.Keywords, trans.Description, trans.CoverContent)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) saveContentTranslations(ctx context.Context, execer dbExecer, contentID int64, translations map[string]ContentTranslationItem) error {
	for lang, trans := range translations {
		lang = strings.TrimSpace(lang)
		if lang == "" {
			continue
		}
		extraJSON, err := json.Marshal(trans.ExtraData)
		if err != nil {
			extraJSON = []byte("{}")
		}
		status := trans.PublishStatus
		if status == "" {
			status = "published"
		}
		_, err = execer.ExecContext(ctx, `
			INSERT INTO "`+db.ContentTranslationTable+`"
			("content_id", "lang", "title", "summary", "body", "keywords", "description", "extra_data", "publish_status")
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT("content_id", "lang") DO UPDATE SET
			"title" = excluded."title",
			"summary" = excluded."summary",
			"body" = excluded."body",
			"keywords" = excluded."keywords",
			"description" = excluded."description",
			"extra_data" = excluded."extra_data",
			"publish_status" = excluded."publish_status"`,
			contentID, lang, trans.Title, trans.Summary, trans.Content, trans.Keywords, trans.Description, string(extraJSON), status)
		if err != nil {
			return err
		}
	}
	return nil
}

type dbExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}
