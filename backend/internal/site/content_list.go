package site

import (
	"context"
	"strings"

	"gocms/internal/db"
	"gocms/internal/routing"
	"gocms/internal/translation"
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
	URL         string `json:"url"`
}

type AdminContentPage struct {
	Query    string             `json:"query"`
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
	Total    int64              `json:"total"`
	Items    []AdminContentItem `json:"items"`
}

func (s *Server) queryAdminContent(ctx context.Context, siteID int64, query string, categoryID int64, includeChildren bool, lang string, page, pageSize int, visibility contentVisibility) (AdminContentPage, error) {
	defaultLang, fallbackLang := s.getDefaultAndFallbackLang(ctx, siteID)
	if lang == "" {
		lang = defaultLang
	}
	rows, total, err := s.queryContentRows(ctx, siteID, query, categoryID, includeChildren, lang, page, pageSize, visibility, false, `
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
	if err := s.populateAdminContentURLs(ctx, siteID, items, lang); err != nil {
		return AdminContentPage{}, err
	}
	return AdminContentPage{Query: query, Page: page, PageSize: pageSize, Total: total, Items: items}, nil
}

func (s *Server) populateAdminContentURLs(ctx context.Context, siteID int64, items []AdminContentItem, lang string) error {
	if len(items) == 0 {
		return nil
	}
	base, err := db.SitePublicOrigin(ctx, s.database, siteID)
	if err != nil {
		return err
	}
	languages, err := s.getLanguages(ctx, siteID)
	if err != nil {
		return err
	}
	prefix := ""
	available := false
	for _, language := range languages {
		if language.Code != lang || language.IsEnabled != 1 {
			continue
		}
		available = true
		if language.IsDefault != 1 {
			if language.SiteMode == db.LanguageSiteModeStandalone {
				base = db.PublicOrigin(language.Domain)
				if base == "" {
					return nil
				}
			} else {
				prefix = strings.Trim(strings.TrimSpace(language.PathPrefix), "/")
				if prefix == "" {
					prefix = strings.ToLower(language.Code)
				}
				prefix = "/" + prefix
			}
		}
		break
	}
	if !available {
		return nil
	}
	placeholders := make([]string, len(items))
	args := []any{routing.DefaultDetailPath, routing.DefaultDetailPattern, siteID}
	indexes := make(map[int64]int, len(items))
	for i, item := range items {
		placeholders[i] = "?"
		args = append(args, item.ID)
		indexes[item.ID] = i
	}
	// Fetch route metadata for this page together; keep bodies and translations
	// out of the compact list, and avoid one database query per content item.
	rows, err := s.database.QueryContext(ctx, `SELECT c.id,COALESCE(c.route_key,''),
		COALESCE(NULLIF(category.detail_path,''),?),COALESCE(NULLIF(category.detail_file_pattern,''),?)
		FROM gocms_content c LEFT JOIN gocms_category category ON category.id=c.category_id AND category.site_id=c.site_id
		WHERE c.site_id=? AND c.id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var key, directory, pattern string
		if err := rows.Scan(&id, &key, &directory, &pattern); err != nil {
			return err
		}
		directory, err := routing.NormalizeDirectory(directory)
		if err != nil {
			continue
		}
		filename := routing.DetailFilename(pattern, key, id)
		items[indexes[id]].URL = base + prefix + routing.DetailURL(directory, filename)
	}
	return rows.Err()
}

func (s *Server) populateAdminContentTitles(ctx context.Context, items []AdminContentItem, lang, defaultLang, fallbackLang string) error {
	if len(items) == 0 {
		return nil
	}
	placeholders := make([]string, len(items))
	args := make([]any, 0, len(items))
	for i, item := range items {
		placeholders[i] = "?"
		args = append(args, item.ID)
	}
	// Load titles only for this page. Translation presence distinguishes legacy
	// base-only entries from a missing translation in multilingual entries.
	rows, err := s.database.QueryContext(ctx, `
		SELECT "content_id", "lang", COALESCE("title", '')
		FROM "`+db.ContentTranslationTable+`"
		WHERE "content_id" IN (`+strings.Join(placeholders, ",")+`)`, args...)
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
		requested := translations[lang]
		fallback := translations[fallbackLang]
		if len(translations) == 0 {
			if lang == defaultLang {
				requested = items[i].Title
			}
			if fallbackLang == defaultLang {
				fallback = items[i].Title
			}
		}
		items[i].Title = translation.Text(requested, fallback)
	}
	return nil
}
