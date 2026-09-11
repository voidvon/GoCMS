package site

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"gocms/internal/db"
)

type BoolInt int

func (b *BoolInt) UnmarshalJSON(data []byte) error {
	str := strings.Trim(string(data), `"`)
	if str == "true" || str == "1" {
		*b = 1
		return nil
	}
	if str == "false" || str == "0" || str == "null" || str == "" {
		*b = 0
		return nil
	}
	var i int
	if err := json.Unmarshal(data, &i); err == nil {
		if i != 0 {
			*b = 1
		} else {
			*b = 0
		}
		return nil
	}
	return nil
}

type LanguageItem struct {
	ID         int64  `json:"id"`
	Code       string `json:"code"`
	Name       string `json:"name"`
	IsDefault  int    `json:"is_default"`
	IsFallback int    `json:"is_fallback"`
	IsEnabled  int    `json:"is_enabled"`
	SortOrder  int    `json:"sort_order"`
	PathPrefix string `json:"path_prefix"`
}

func (s *Server) getLanguages(ctx context.Context) ([]LanguageItem, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT "id", "code", "name", "is_default", "is_fallback", "is_enabled", "sort_order", "path_prefix"
		FROM "`+db.LanguageTable+`"
		ORDER BY "is_default" DESC, "sort_order" ASC, "id" ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []LanguageItem
	for rows.Next() {
		var item LanguageItem
		if err := rows.Scan(&item.ID, &item.Code, &item.Name, &item.IsDefault, &item.IsFallback, &item.IsEnabled, &item.SortOrder, &item.PathPrefix); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Server) getDefaultAndFallbackLang(ctx context.Context) (defaultCode string, fallbackCode string) {
	defaultCode = "zh-CN"
	fallbackCode = "zh-CN"

	var dCode, fCode string
	_ = s.database.QueryRowContext(ctx, `SELECT "code" FROM "`+db.LanguageTable+`" WHERE "is_default" = 1 LIMIT 1`).Scan(&dCode)
	if dCode != "" {
		defaultCode = dCode
	}
	_ = s.database.QueryRowContext(ctx, `SELECT "code" FROM "`+db.LanguageTable+`" WHERE "is_fallback" = 1 LIMIT 1`).Scan(&fCode)
	if fCode != "" {
		fallbackCode = fCode
	} else {
		fallbackCode = defaultCode
	}
	return defaultCode, fallbackCode
}

func (s *Server) adminLanguages(response http.ResponseWriter, request *http.Request) {
	if !s.requireAdmin(response, request) {
		return
	}

	switch request.Method {
	case http.MethodGet:
		items, err := s.getLanguages(request.Context())
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, items)

	case http.MethodPost:
		var payload struct {
			Code       string `json:"code"`
			Name       string `json:"name"`
			IsDefault  BoolInt `json:"is_default"`
			IsFallback BoolInt `json:"is_fallback"`
			IsEnabled  BoolInt `json:"is_enabled"`
			SortOrder  int    `json:"sort_order"`
			PathPrefix string `json:"path_prefix"`
		}
		if err := decodeRequest(request, &payload); err != nil {
			http.Error(response, "invalid payload", http.StatusBadRequest)
			return
		}

		payload.Code = strings.TrimSpace(payload.Code)
		payload.Name = strings.TrimSpace(payload.Name)
		payload.PathPrefix = strings.Trim(strings.TrimSpace(payload.PathPrefix), "/")

		if payload.Code == "" || payload.Name == "" {
			http.Error(response, "code and name are required", http.StatusBadRequest)
			return
		}

		if payload.IsDefault == 1 {
			_, _ = s.database.ExecContext(request.Context(), `UPDATE "`+db.LanguageTable+`" SET "is_default" = 0`)
			payload.IsEnabled = 1 // Default language must be enabled
		}
		if payload.IsFallback == 1 {
			_, _ = s.database.ExecContext(request.Context(), `UPDATE "`+db.LanguageTable+`" SET "is_fallback" = 0`)
		}

		res, err := s.database.ExecContext(request.Context(), `
			INSERT INTO "`+db.LanguageTable+`" ("code", "name", "is_default", "is_fallback", "is_enabled", "sort_order", "path_prefix")
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			payload.Code, payload.Name, payload.IsDefault, payload.IsFallback, payload.IsEnabled, payload.SortOrder, payload.PathPrefix)
		if err != nil {
			http.Error(response, "duplicate language code or database error", http.StatusBadRequest)
			return
		}

		newID, _ := res.LastInsertId()
		writeJSON(response, http.StatusCreated, map[string]any{"ok": true, "id": newID})

	default:
		methodNotAllowed(response)
	}
}

func (s *Server) adminLanguageItem(response http.ResponseWriter, request *http.Request, rawID string) {
	if !s.requireAdmin(response, request) {
		return
	}
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id < 1 {
		http.Error(response, "invalid id", http.StatusBadRequest)
		return
	}

	switch request.Method {
	case http.MethodPut:
		var payload struct {
			Code       string `json:"code"`
			Name       string `json:"name"`
			IsDefault  BoolInt `json:"is_default"`
			IsFallback BoolInt `json:"is_fallback"`
			IsEnabled  BoolInt `json:"is_enabled"`
			SortOrder  int    `json:"sort_order"`
			PathPrefix string `json:"path_prefix"`
		}
		if err := decodeRequest(request, &payload); err != nil {
			http.Error(response, "invalid payload", http.StatusBadRequest)
			return
		}

		payload.Code = strings.TrimSpace(payload.Code)
		payload.Name = strings.TrimSpace(payload.Name)
		payload.PathPrefix = strings.Trim(strings.TrimSpace(payload.PathPrefix), "/")

		if payload.Code == "" || payload.Name == "" {
			http.Error(response, "code and name are required", http.StatusBadRequest)
			return
		}

		if payload.IsDefault == 1 {
			_, _ = s.database.ExecContext(request.Context(), `UPDATE "`+db.LanguageTable+`" SET "is_default" = 0 WHERE "id" != ?`, id)
			payload.IsEnabled = 1
		}
		if payload.IsFallback == 1 {
			_, _ = s.database.ExecContext(request.Context(), `UPDATE "`+db.LanguageTable+`" SET "is_fallback" = 0 WHERE "id" != ?`, id)
		}

		if _, err := s.database.ExecContext(request.Context(), `
			UPDATE "`+db.LanguageTable+`"
			SET "code" = ?, "name" = ?, "is_default" = ?, "is_fallback" = ?, "is_enabled" = ?, "sort_order" = ?, "path_prefix" = ?
			WHERE "id" = ?`,
			payload.Code, payload.Name, payload.IsDefault, payload.IsFallback, payload.IsEnabled, payload.SortOrder, payload.PathPrefix, id); err != nil {
			http.Error(response, "database error or duplicate code", http.StatusBadRequest)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"ok": true})

	case http.MethodDelete:
		var isDefault, isFallback int
		err := s.database.QueryRowContext(request.Context(), `
			SELECT "is_default", "is_fallback" FROM "`+db.LanguageTable+`" WHERE "id" = ?`, id).Scan(&isDefault, &isFallback)
		if err == sql.ErrNoRows {
			http.Error(response, "language not found", http.StatusNotFound)
			return
		}
		if isDefault == 1 || isFallback == 1 {
			http.Error(response, "cannot delete default or fallback language", http.StatusBadRequest)
			return
		}

		if _, err := s.database.ExecContext(request.Context(), `DELETE FROM "`+db.LanguageTable+`" WHERE "id" = ?`, id); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"ok": true})

	default:
		methodNotAllowed(response)
	}
}
