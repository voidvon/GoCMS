package site

import (
	"context"
	"database/sql"
	"strings"

	"gocms/internal/db"
	"gocms/internal/gallery"
)

func applyCoverGallery(ctx context.Context, tx *sql.Tx, siteID, modelID int64, payload *contentPayload) (string, []string, error) {
	var field string
	err := tx.QueryRowContext(ctx, `
		SELECT f.field_name
		FROM `+db.ModelTable+` m
		CROSS JOIN json_each(CASE WHEN json_valid(m.entry_fields) THEN m.entry_fields ELSE '[]' END) e
		JOIN `+db.ModelFieldTable+` f ON f.table_id = m.table_id
			AND f.field_name = json_extract(CASE WHEN e.type = 'object' THEN e.value END, '$.field')
		WHERE m.id = ? AND f.field_type = 'morepic' AND f.is_translatable = 0 AND f.is_system = 0
			AND instr(',' || json_extract(CASE WHEN e.type = 'object' THEN e.value END, '$.cover_source_sites') || ',', ',' || ? || ',') > 0
		ORDER BY e.key LIMIT 1`, modelID, siteID).Scan(&field)
	if err == sql.ErrNoRows {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}

	value, present := payload.ExtraData[field]
	photos := gallery.Parse(value)
	if !present {
		if cover := strings.TrimSpace(payload.CoverImage); cover != "" {
			photos = []gallery.Photo{{URL: cover}}
			payload.ExtraData[field] = photos
		}
	}
	payload.CoverImage = ""
	urls := make([]string, 0, len(photos))
	for _, photo := range photos {
		urls = append(urls, photo.URL)
	}
	if len(urls) > 0 {
		payload.CoverImage = urls[0]
	}
	return field, urls, nil
}
