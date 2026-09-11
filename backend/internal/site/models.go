package site

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gocms/internal/db"
)

type ModelTableItem struct {
	ID          int64  `json:"id"`
	TableName   string `json:"table_name"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IsDefault   int    `json:"is_default"`
	CreatedAt   string `json:"created_at"`
	FieldCount  int    `json:"field_count,omitempty"`
}

type ModelFieldItem struct {
	ID           int64  `json:"id"`
	TableID      int64  `json:"table_id"`
	FieldName    string `json:"field_name"`
	FieldLabel   string `json:"field_label"`
	FieldType    string `json:"field_type"`
	FieldOptions string `json:"field_options"`
	Description  string `json:"description"`
	SortOrder    int    `json:"sort_order"`
	IsSystem     int    `json:"is_system"`
}

type SystemModelItem struct {
	ID          int64           `json:"id"`
	Name        string          `json:"name"`
	TableID     int64           `json:"table_id"`
	TableName   string          `json:"table_name,omitempty"`
	Description string          `json:"description"`
	EntryFields json.RawMessage `json:"entry_fields"`
	MustFields  json.RawMessage `json:"must_fields"`
	IsDefault   int             `json:"is_default"`
	SortOrder   int             `json:"sort_order"`
	CreatedAt   string          `json:"created_at"`
}

// Model Tables

func (s *Server) adminModelTables(response http.ResponseWriter, request *http.Request) {
	if !s.requireAdmin(response, request) {
		return
	}
	switch request.Method {
	case http.MethodGet:
		rows, err := s.database.QueryContext(request.Context(), `
			SELECT t."id", t."table_name", t."name", t."description", t."is_default", t."created_at",
			       (SELECT COUNT(*) FROM "`+db.ModelFieldTable+`" f WHERE f."table_id" = t."id") as field_count
			FROM "`+db.ModelTableTable+`" t ORDER BY t."is_default" DESC, t."id" ASC`)
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		items := make([]ModelTableItem, 0)
		for rows.Next() {
			var item ModelTableItem
			if err := rows.Scan(&item.ID, &item.TableName, &item.Name, &item.Description, &item.IsDefault, &item.CreatedAt, &item.FieldCount); err != nil {
				http.Error(response, "database error", http.StatusInternalServerError)
				return
			}
			items = append(items, item)
		}
		writeJSON(response, http.StatusOK, items)

	case http.MethodPost:
		var payload struct {
			TableName   string `json:"table_name"`
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := decodeRequest(request, &payload); err != nil {
			http.Error(response, "invalid payload", http.StatusBadRequest)
			return
		}
		payload.TableName = strings.ToLower(strings.TrimSpace(payload.TableName))
		payload.Name = strings.TrimSpace(payload.Name)
		if payload.TableName == "" || payload.Name == "" {
			http.Error(response, "table_name and name are required", http.StatusBadRequest)
			return
		}
		now := time.Now().Format("2006-01-02 15:04:05")
		res, err := s.database.ExecContext(request.Context(), `
			INSERT INTO "`+db.ModelTableTable+`" ("table_name", "name", "description", "is_default", "created_at")
			VALUES (?, ?, ?, 0, ?)`, payload.TableName, payload.Name, payload.Description, now)
		if err != nil {
			http.Error(response, "database error or duplicate table name", http.StatusBadRequest)
			return
		}
		newID, _ := res.LastInsertId()
		writeJSON(response, http.StatusCreated, map[string]any{"ok": true, "id": newID})

	default:
		methodNotAllowed(response)
	}
}

func (s *Server) adminModelTableItem(response http.ResponseWriter, request *http.Request, rawID string) {
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
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := decodeRequest(request, &payload); err != nil {
			http.Error(response, "invalid payload", http.StatusBadRequest)
			return
		}
		payload.Name = strings.TrimSpace(payload.Name)
		if payload.Name == "" {
			http.Error(response, "name is required", http.StatusBadRequest)
			return
		}
		if _, err := s.database.ExecContext(request.Context(), `
			UPDATE "`+db.ModelTableTable+`" SET "name" = ?, "description" = ? WHERE "id" = ?`,
			payload.Name, payload.Description, id); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"ok": true})

	case http.MethodDelete:
		var isDefault int
		if err := s.database.QueryRowContext(request.Context(), `SELECT "is_default" FROM "`+db.ModelTableTable+`" WHERE "id" = ?`, id).Scan(&isDefault); err != nil {
			http.Error(response, "not found", http.StatusNotFound)
			return
		}
		if isDefault == 1 {
			http.Error(response, "cannot delete default table", http.StatusBadRequest)
			return
		}
		var modelCount int
		if err := s.database.QueryRowContext(request.Context(), `SELECT COUNT(*) FROM "`+db.ModelTable+`" WHERE "table_id" = ?`, id).Scan(&modelCount); err == nil && modelCount > 0 {
			http.Error(response, "cannot delete table bound to models", http.StatusBadRequest)
			return
		}
		_, _ = s.database.ExecContext(request.Context(), `DELETE FROM "`+db.ModelFieldTable+`" WHERE "table_id" = ?`, id)
		_, err := s.database.ExecContext(request.Context(), `DELETE FROM "`+db.ModelTableTable+`" WHERE "id" = ?`, id)
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"ok": true})

	default:
		methodNotAllowed(response)
	}
}

// Model Fields

func (s *Server) adminModelFields(response http.ResponseWriter, request *http.Request) {
	if !s.requireAdmin(response, request) {
		return
	}
	switch request.Method {
	case http.MethodGet:
		tableID := parseIntOrZero(request.URL.Query().Get("table_id"))
		query := `SELECT "id", "table_id", "field_name", "field_label", "field_type", "field_options", "description", "sort_order", "is_system"
		          FROM "` + db.ModelFieldTable + `"`
		var args []any
		if tableID > 0 {
			query += ` WHERE "table_id" = ?`
			args = append(args, tableID)
		}
		query += ` ORDER BY "sort_order" ASC, "id" ASC`

		rows, err := s.database.QueryContext(request.Context(), query, args...)
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		items := make([]ModelFieldItem, 0)
		for rows.Next() {
			var item ModelFieldItem
			if err := rows.Scan(&item.ID, &item.TableID, &item.FieldName, &item.FieldLabel, &item.FieldType, &item.FieldOptions, &item.Description, &item.SortOrder, &item.IsSystem); err != nil {
				http.Error(response, "database error", http.StatusInternalServerError)
				return
			}
			items = append(items, item)
		}
		writeJSON(response, http.StatusOK, items)

	case http.MethodPost:
		var payload struct {
			TableID      int64  `json:"table_id"`
			FieldName    string `json:"field_name"`
			FieldLabel   string `json:"field_label"`
			FieldType    string `json:"field_type"`
			FieldOptions string `json:"field_options"`
			Description  string `json:"description"`
			SortOrder    int    `json:"sort_order"`
		}
		if err := decodeRequest(request, &payload); err != nil {
			http.Error(response, "invalid payload", http.StatusBadRequest)
			return
		}
		payload.FieldName = strings.ToLower(strings.TrimSpace(payload.FieldName))
		payload.FieldLabel = strings.TrimSpace(payload.FieldLabel)
		if payload.TableID < 1 || payload.FieldName == "" || payload.FieldLabel == "" {
			http.Error(response, "table_id, field_name and field_label are required", http.StatusBadRequest)
			return
		}
		if payload.FieldType == "" {
			payload.FieldType = "text"
		}
		res, err := s.database.ExecContext(request.Context(), `
			INSERT INTO "`+db.ModelFieldTable+`" ("table_id", "field_name", "field_label", "field_type", "field_options", "description", "sort_order", "is_system")
			VALUES (?, ?, ?, ?, ?, ?, ?, 0)`,
			payload.TableID, payload.FieldName, payload.FieldLabel, payload.FieldType, payload.FieldOptions, payload.Description, payload.SortOrder)
		if err != nil {
			http.Error(response, "database error or duplicate field name", http.StatusBadRequest)
			return
		}
		newID, _ := res.LastInsertId()
		writeJSON(response, http.StatusCreated, map[string]any{"ok": true, "id": newID})

	default:
		methodNotAllowed(response)
	}
}

func (s *Server) adminModelFieldItem(response http.ResponseWriter, request *http.Request, rawID string) {
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
			FieldLabel   string `json:"field_label"`
			FieldType    string `json:"field_type"`
			FieldOptions string `json:"field_options"`
			Description  string `json:"description"`
			SortOrder    int    `json:"sort_order"`
		}
		if err := decodeRequest(request, &payload); err != nil {
			http.Error(response, "invalid payload", http.StatusBadRequest)
			return
		}
		payload.FieldLabel = strings.TrimSpace(payload.FieldLabel)
		if payload.FieldLabel == "" {
			http.Error(response, "field_label is required", http.StatusBadRequest)
			return
		}
		if _, err := s.database.ExecContext(request.Context(), `
			UPDATE "`+db.ModelFieldTable+`"
			SET "field_label" = ?, "field_type" = ?, "field_options" = ?, "description" = ?, "sort_order" = ?
			WHERE "id" = ?`,
			payload.FieldLabel, payload.FieldType, payload.FieldOptions, payload.Description, payload.SortOrder, id); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"ok": true})

	case http.MethodDelete:
		var isSystem int
		if err := s.database.QueryRowContext(request.Context(), `SELECT "is_system" FROM "`+db.ModelFieldTable+`" WHERE "id" = ?`, id).Scan(&isSystem); err != nil {
			http.Error(response, "not found", http.StatusNotFound)
			return
		}
		if isSystem == 1 {
			http.Error(response, "cannot delete system field", http.StatusBadRequest)
			return
		}
		if _, err := s.database.ExecContext(request.Context(), `DELETE FROM "`+db.ModelFieldTable+`" WHERE "id" = ?`, id); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"ok": true})

	default:
		methodNotAllowed(response)
	}
}

// System Models

func (s *Server) adminModels(response http.ResponseWriter, request *http.Request) {
	if !s.requireAdmin(response, request) {
		return
	}
	switch request.Method {
	case http.MethodGet:
		rows, err := s.database.QueryContext(request.Context(), `
			SELECT m."id", m."name", m."table_id", COALESCE(t."name", '') as table_name, m."description",
			       m."entry_fields", m."must_fields", m."is_default", m."sort_order", m."created_at"
			FROM "`+db.ModelTable+`" m
			LEFT JOIN "`+db.ModelTableTable+`" t ON t."id" = m."table_id"
			ORDER BY m."is_default" DESC, m."sort_order" ASC, m."id" ASC`)
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		items := make([]SystemModelItem, 0)
		for rows.Next() {
			var item SystemModelItem
			var entryJSON, mustJSON string
			if err := rows.Scan(&item.ID, &item.Name, &item.TableID, &item.TableName, &item.Description, &entryJSON, &mustJSON, &item.IsDefault, &item.SortOrder, &item.CreatedAt); err != nil {
				http.Error(response, "database error", http.StatusInternalServerError)
				return
			}
			item.EntryFields = json.RawMessage(entryJSON)
			item.MustFields = json.RawMessage(mustJSON)
			items = append(items, item)
		}
		writeJSON(response, http.StatusOK, items)

	case http.MethodPost:
		var payload struct {
			Name        string          `json:"name"`
			TableID     int64           `json:"table_id"`
			Description string          `json:"description"`
			EntryFields json.RawMessage `json:"entry_fields"`
			MustFields  json.RawMessage `json:"must_fields"`
			SortOrder   int             `json:"sort_order"`
		}
		if err := decodeRequest(request, &payload); err != nil {
			http.Error(response, "invalid payload", http.StatusBadRequest)
			return
		}
		payload.Name = strings.TrimSpace(payload.Name)
		if payload.Name == "" || payload.TableID < 1 {
			http.Error(response, "name and table_id are required", http.StatusBadRequest)
			return
		}
		entryStr := string(payload.EntryFields)
		if strings.TrimSpace(entryStr) == "" {
			entryStr = "[]"
		}
		mustStr := string(payload.MustFields)
		if strings.TrimSpace(mustStr) == "" {
			mustStr = "[]"
		}
		now := time.Now().Format("2006-01-02 15:04:05")
		res, err := s.database.ExecContext(request.Context(), `
			INSERT INTO "`+db.ModelTable+`" ("name", "table_id", "description", "entry_fields", "must_fields", "is_default", "sort_order", "created_at")
			VALUES (?, ?, ?, ?, ?, 0, ?, ?)`,
			payload.Name, payload.TableID, payload.Description, entryStr, mustStr, payload.SortOrder, now)
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		newID, _ := res.LastInsertId()
		writeJSON(response, http.StatusCreated, map[string]any{"ok": true, "id": newID})

	default:
		methodNotAllowed(response)
	}
}

func (s *Server) adminModelItem(response http.ResponseWriter, request *http.Request, rawID string) {
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
			Name        string          `json:"name"`
			TableID     int64           `json:"table_id"`
			Description string          `json:"description"`
			EntryFields json.RawMessage `json:"entry_fields"`
			MustFields  json.RawMessage `json:"must_fields"`
			SortOrder   int             `json:"sort_order"`
		}
		if err := decodeRequest(request, &payload); err != nil {
			http.Error(response, "invalid payload", http.StatusBadRequest)
			return
		}
		payload.Name = strings.TrimSpace(payload.Name)
		if payload.Name == "" || payload.TableID < 1 {
			http.Error(response, "name and table_id are required", http.StatusBadRequest)
			return
		}
		entryStr := string(payload.EntryFields)
		if strings.TrimSpace(entryStr) == "" {
			entryStr = "[]"
		}
		mustStr := string(payload.MustFields)
		if strings.TrimSpace(mustStr) == "" {
			mustStr = "[]"
		}
		if _, err := s.database.ExecContext(request.Context(), `
			UPDATE "`+db.ModelTable+`"
			SET "name" = ?, "table_id" = ?, "description" = ?, "entry_fields" = ?, "must_fields" = ?, "sort_order" = ?
			WHERE "id" = ?`,
			payload.Name, payload.TableID, payload.Description, entryStr, mustStr, payload.SortOrder, id); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"ok": true})

	case http.MethodDelete:
		var isDefault int
		if err := s.database.QueryRowContext(request.Context(), `SELECT "is_default" FROM "`+db.ModelTable+`" WHERE "id" = ?`, id).Scan(&isDefault); err != nil {
			http.Error(response, "not found", http.StatusNotFound)
			return
		}
		if isDefault == 1 {
			http.Error(response, "cannot delete default model", http.StatusBadRequest)
			return
		}
		var categoryCount int
		if err := s.database.QueryRowContext(request.Context(), `SELECT COUNT(*) FROM "gocms_category" WHERE "model_id" = ?`, id).Scan(&categoryCount); err == nil && categoryCount > 0 {
			http.Error(response, "cannot delete model bound to categories", http.StatusBadRequest)
			return
		}
		if _, err := s.database.ExecContext(request.Context(), `DELETE FROM "`+db.ModelTable+`" WHERE "id" = ?`, id); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"ok": true})

	default:
		methodNotAllowed(response)
	}
}
