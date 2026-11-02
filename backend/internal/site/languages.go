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
	SiteMode   string `json:"site_mode"`
	Domain     string `json:"domain"`
	OutputDir  string `json:"output_dir"`
	// IsAdminDefault marks the language the admin backend displays by default in
	// category and content lists. It is independent from IsDefault (the primary
	// site language rendered at the site root).
	IsAdminDefault int `json:"is_admin_default"`
}

func (s *Server) getLanguages(ctx context.Context, siteID int64) ([]LanguageItem, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT "id", "code", "name", "is_default", "is_fallback", "is_enabled", "sort_order", "path_prefix", "site_mode", "domain", "output_dir", "is_admin_default"
		FROM "`+db.LanguageTable+`" WHERE "site_id" = ?
		ORDER BY "is_default" DESC, "sort_order" ASC, "id" ASC`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]LanguageItem, 0)
	for rows.Next() {
		var item LanguageItem
		if err := rows.Scan(&item.ID, &item.Code, &item.Name, &item.IsDefault, &item.IsFallback, &item.IsEnabled, &item.SortOrder, &item.PathPrefix, &item.SiteMode, &item.Domain, &item.OutputDir, &item.IsAdminDefault); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) getDefaultAndFallbackLang(ctx context.Context, siteID int64) (defaultCode string, fallbackCode string) {
	defaultCode = "zh-CN"
	var dCode, fCode string
	_ = s.database.QueryRowContext(ctx, `SELECT "code" FROM "`+db.LanguageTable+`" WHERE "site_id" = ? AND "is_default" = 1 ORDER BY "id" LIMIT 1`, siteID).Scan(&dCode)
	if dCode != "" {
		defaultCode = dCode
	}
	_ = s.database.QueryRowContext(ctx, `SELECT "code" FROM "`+db.LanguageTable+`" WHERE "site_id" = ? AND "is_fallback" = 1 ORDER BY "id" LIMIT 1`, siteID).Scan(&fCode)
	fallbackCode = defaultCode
	if fCode != "" {
		fallbackCode = fCode
	}
	return defaultCode, fallbackCode
}

func (s *Server) adminLanguages(response http.ResponseWriter, request *http.Request) {
	if !s.requireAdmin(response, request) {
		return
	}
	siteID, err := s.resolveSiteID(request, s.currentAdmin(request))
	if err != nil {
		http.Error(response, err.Error(), http.StatusForbidden)
		return
	}
	switch request.Method {
	case http.MethodGet:
		items, err := s.getLanguages(request.Context(), siteID)
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, items)
	case http.MethodPost:
		s.saveLanguage(response, request, siteID, 0)
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
	siteID, err := s.resolveSiteID(request, s.currentAdmin(request))
	if err != nil {
		http.Error(response, err.Error(), http.StatusForbidden)
		return
	}
	switch request.Method {
	case http.MethodPut:
		s.saveLanguage(response, request, siteID, id)
	case http.MethodDelete:
		tx, err := s.database.BeginTx(request.Context(), nil)
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()
		var isDefault, isFallback, isAdminDefault int
		err = tx.QueryRowContext(request.Context(), `
			SELECT "is_default", "is_fallback", "is_admin_default" FROM "`+db.LanguageTable+`" WHERE "site_id" = ? AND "id" = ?`, siteID, id).Scan(&isDefault, &isFallback, &isAdminDefault)
		if err == sql.ErrNoRows {
			http.Error(response, "language not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		if isDefault == 1 || isFallback == 1 || isAdminDefault == 1 {
			http.Error(response, "cannot delete default, fallback or admin display language", http.StatusBadRequest)
			return
		}
		if _, err := tx.ExecContext(request.Context(), `DELETE FROM "`+db.LanguageTable+`" WHERE "site_id" = ? AND "id" = ?`, siteID, id); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		if err := tx.Commit(); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		s.invalidateSitePublication(siteID)
		writeJSON(response, http.StatusOK, map[string]any{"ok": true})
	default:
		methodNotAllowed(response)
	}
}

// saveLanguage changes flags and the language row atomically. A duplicate code
// or an ID owned by another site cannot clear this site's language defaults.
func (s *Server) saveLanguage(response http.ResponseWriter, request *http.Request, siteID, id int64) {
	var payload struct {
		Code           string  `json:"code"`
		Name           string  `json:"name"`
		IsDefault      BoolInt `json:"is_default"`
		IsFallback     BoolInt `json:"is_fallback"`
		IsEnabled      BoolInt `json:"is_enabled"`
		SortOrder      int     `json:"sort_order"`
		PathPrefix     string  `json:"path_prefix"`
		SiteMode       string  `json:"site_mode"`
		Domain         string  `json:"domain"`
		OutputDir      string  `json:"output_dir"`
		IsAdminDefault BoolInt `json:"is_admin_default"`
	}
	if err := decodeRequest(request, &payload); err != nil {
		http.Error(response, "invalid payload", http.StatusBadRequest)
		return
	}
	payload.Code = strings.TrimSpace(payload.Code)
	payload.Name = strings.TrimSpace(payload.Name)
	payload.PathPrefix = strings.Trim(strings.TrimSpace(payload.PathPrefix), "/")
	payload.SiteMode = strings.ToLower(strings.TrimSpace(payload.SiteMode))
	if payload.SiteMode != db.LanguageSiteModeStandalone {
		payload.SiteMode = db.LanguageSiteModeSubdir
	}
	if payload.SiteMode == db.LanguageSiteModeStandalone {
		payload.Domain = db.NormalizeDomain(payload.Domain)
		payload.OutputDir = strings.TrimSpace(strings.Trim(payload.OutputDir, "/\\"))
		if err := db.ValidateOutputDir(payload.OutputDir); err != nil {
			http.Error(response, "invalid output_dir: "+err.Error(), http.StatusBadRequest)
			return
		}
	} else {
		payload.Domain, payload.OutputDir = "", ""
	}
	if payload.Code == "" || payload.Name == "" {
		http.Error(response, "code and name are required", http.StatusBadRequest)
		return
	}
	if payload.IsDefault == 1 {
		payload.IsEnabled = 1
		payload.SiteMode = db.LanguageSiteModeSubdir
		payload.Domain, payload.OutputDir = "", ""
	}
	if payload.IsAdminDefault == 1 {
		payload.IsEnabled = 1
	}
	ctx := request.Context()
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	if id > 0 {
		var existingID int64
		err := tx.QueryRowContext(ctx, `SELECT "id" FROM "`+db.LanguageTable+`" WHERE "site_id" = ? AND "id" = ?`, siteID, id).Scan(&existingID)
		if err == sql.ErrNoRows {
			http.Error(response, "language not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
	}
	for _, flag := range []struct {
		name  string
		value BoolInt
	}{
		{"is_default", payload.IsDefault}, {"is_fallback", payload.IsFallback}, {"is_admin_default", payload.IsAdminDefault},
	} {
		if flag.value == 1 {
			if _, err := tx.ExecContext(ctx, `UPDATE "`+db.LanguageTable+`" SET "`+flag.name+`" = 0 WHERE "site_id" = ? AND "id" != ?`, siteID, id); err != nil {
				http.Error(response, "database error", http.StatusInternalServerError)
				return
			}
		}
	}
	args := []any{payload.Code, payload.Name, payload.IsDefault, payload.IsFallback, payload.IsEnabled, payload.SortOrder, payload.PathPrefix, payload.SiteMode, payload.Domain, payload.OutputDir, payload.IsAdminDefault, siteID}
	var result sql.Result
	if id == 0 {
		result, err = tx.ExecContext(ctx, `INSERT INTO "`+db.LanguageTable+`"
			("code", "name", "is_default", "is_fallback", "is_enabled", "sort_order", "path_prefix", "site_mode", "domain", "output_dir", "is_admin_default", "site_id")
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, args...)
	} else {
		args = append(args, id)
		result, err = tx.ExecContext(ctx, `UPDATE "`+db.LanguageTable+`"
			SET "code" = ?, "name" = ?, "is_default" = ?, "is_fallback" = ?, "is_enabled" = ?, "sort_order" = ?, "path_prefix" = ?, "site_mode" = ?, "domain" = ?, "output_dir" = ?, "is_admin_default" = ?
			WHERE "site_id" = ? AND "id" = ?`, args...)
	}
	if err != nil {
		http.Error(response, "duplicate language code or database error", http.StatusBadRequest)
		return
	}
	if _, err := tx.ExecContext(ctx, `UPDATE "`+db.LanguageTable+`" SET "is_admin_default" = 1
		WHERE "site_id" = ? AND "is_default" = 1 AND "id" = (SELECT MIN("id") FROM "`+db.LanguageTable+`" WHERE "site_id" = ? AND "is_default" = 1)
		AND NOT EXISTS (SELECT 1 FROM "`+db.LanguageTable+`" WHERE "site_id" = ? AND "is_admin_default" = 1)`, siteID, siteID, siteID); err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	s.invalidateSitePublication(siteID)
	if id == 0 {
		newID, _ := result.LastInsertId()
		writeJSON(response, http.StatusCreated, map[string]any{"ok": true, "id": newID})
	} else {
		writeJSON(response, http.StatusOK, map[string]any{"ok": true})
	}
}
