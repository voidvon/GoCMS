package site

import (
	"context"
	"strings"

	"gocms/internal/db"
)

// AdminContentItem contains only the fields used by the admin content table.
// Editing loads the complete Content through the item endpoint.
type AdminContentItem struct {
	ID          int64  `json:"id"`
	Category    int64  `json:"category_id"`
	Title       string `json:"title"`
	Code        string `json:"code"`
	CoverImage  string `json:"cover_image"`
	PublishedAt string `json:"published_at"`
	Featured    int64  `json:"featured"`
	Visible     int64  `json:"visible"`
}

type AdminContentPage struct {
	Query    string             `json:"query"`
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
	Total    int64              `json:"total"`
	Items    []AdminContentItem `json:"items"`
}

func (s *Server) queryAdminContent(ctx context.Context, siteID int64, query string, categoryID int64, includeChildren bool, lang string, page, pageSize int) (AdminContentPage, error) {
	defaultLang, fallbackLang := s.getDefaultAndFallbackLang(ctx)
	if lang == "" {
		lang = defaultLang
	}
	rows, total, err := s.queryContentRows(ctx, siteID, query, categoryID, includeChildren, lang, page, pageSize, false, `
		"id", COALESCE("category_id", 0), COALESCE("title", ''), COALESCE("code", ''),
		COALESCE("cover_image", ''), COALESCE("published_at", ''), COALESCE("featured", 0), COALESCE("visible", 0)`)
	if err != nil {
		return AdminContentPage{}, err
	}
	defer rows.Close()
	items := make([]AdminContentItem, 0, pageSize)
	for rows.Next() {
		var item AdminContentItem
		if err := rows.Scan(&item.ID, &item.Category, &item.Title, &item.Code, &item.CoverImage, &item.PublishedAt, &item.Featured, &item.Visible); err != nil {
			return AdminContentPage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return AdminContentPage{}, err
	}
	if err := rows.Close(); err != nil {
		return AdminContentPage{}, err
	}
	if err := s.populateAdminContentTitles(ctx, items, lang, defaultLang, fallbackLang); err != nil {
		return AdminContentPage{}, err
	}
	return AdminContentPage{Query: query, Page: page, PageSize: pageSize, Total: total, Items: items}, nil
}

func (s *Server) populateAdminContentTitles(ctx context.Context, items []AdminContentItem, lang, defaultLang, fallbackLang string) error {
	if len(items) == 0 {
		return nil
	}
	placeholders := make([]string, len(items))
	args := make([]any, 0, len(items)+2)
	for i, item := range items {
		placeholders[i] = "?"
		args = append(args, item.ID)
	}
	args = append(args, lang, fallbackLang)
	// Load titles only, for this page and the two languages that can be displayed.
	rows, err := s.database.QueryContext(ctx, `
		SELECT "content_id", "lang", COALESCE("title", '')
		FROM "`+db.ContentTranslationTable+`"
		WHERE "content_id" IN (`+strings.Join(placeholders, ",")+`) AND "lang" IN (?, ?)`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	titles := make(map[int64]map[string]string, len(items))
	for rows.Next() {
		var id int64
		var code, title string
		if err := rows.Scan(&id, &code, &title); err != nil {
			return err
		}
		if titles[id] == nil {
			titles[id] = make(map[string]string)
		}
		titles[id][code] = title
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range items {
		translations := titles[items[i].ID]
		requested, exists := translations[lang]
		// Match applyContentTranslation: a missing primary-language translation
		// uses the base title, while an existing blank title allows fallback.
		if !exists && lang == defaultLang {
			requested = items[i].Title
		}
		fallback, exists := translations[fallbackLang]
		if !exists && fallbackLang == defaultLang {
			fallback = items[i].Title
		}
		if strings.TrimSpace(requested) != "" {
			items[i].Title = requested
		} else if strings.TrimSpace(fallback) != "" {
			items[i].Title = fallback
		}
	}
	return nil
}
