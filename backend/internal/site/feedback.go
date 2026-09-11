package site

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gocms/internal/db"
)

type FeedbackItem struct {
	ID          int64          `json:"id"`
	ClassID     int64          `json:"class_id"`
	ClassName   string         `json:"class_name,omitempty"`
	Title       string         `json:"title"`
	Name        string         `json:"name"`
	Phone       string         `json:"phone"`
	Mobile      string         `json:"mobile"`
	Email       string         `json:"email"`
	Address     string         `json:"address"`
	Content     string         `json:"content"`
	CreatedAt   string         `json:"created_at"`
	State       int64          `json:"state"`
	ContentID   int64          `json:"content_id"`
	IP          string         `json:"ip"`
	ExtraData   map[string]any `json:"extra_data"`
}

type FeedbackClassItem struct {
	ID           int64           `json:"id"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	FieldsConfig json.RawMessage `json:"fields_config"`
	MustFields   json.RawMessage `json:"must_fields"`
	SortOrder    int             `json:"sort_order"`
	CreatedAt    string          `json:"created_at"`
	ItemCount    int             `json:"item_count,omitempty"`
}

type FeedbackFieldItem struct {
	ID           int64  `json:"id"`
	FieldName    string `json:"field_name"`
	FieldLabel   string `json:"field_label"`
	FieldType    string `json:"field_type"`
	FieldOptions string `json:"field_options"`
	Description  string `json:"description"`
	SortOrder    int    `json:"sort_order"`
	IsSystem     int    `json:"is_system"`
}

// Public Submission Handler (supports /api/feedback and /api/messages)
func (s *Server) feedbackSubmit(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(response)
		return
	}
	if err := request.ParseForm(); err != nil {
		http.Error(response, "invalid form", http.StatusBadRequest)
		return
	}

	classID := parseIntOrZero(request.FormValue("class_id"))
	if classID == 0 {
		classID = parseIntOrZero(request.FormValue("bid"))
	}
	if classID == 0 {
		classID = 1
	}

	var mustFieldsJSON string
	err := s.database.QueryRowContext(request.Context(), `
		SELECT "must_fields" FROM "`+db.FeedbackClassTable+`" WHERE "id" = ?`, classID).Scan(&mustFieldsJSON)
	if err != nil {
		// Fallback to default check if class not found
		mustFieldsJSON = `["title", "name", "phone"]`
	}

	var mustFields []string
	_ = json.Unmarshal([]byte(mustFieldsJSON), &mustFields)
	for _, reqField := range mustFields {
		reqField = strings.TrimSpace(reqField)
		if reqField == "" {
			continue
		}
		if strings.TrimSpace(request.FormValue(reqField)) == "" {
			http.Error(response, fmt.Sprintf("%s is required", reqField), http.StatusBadRequest)
			return
		}
	}

	title := strings.TrimSpace(request.FormValue("title"))
	if title == "" {
		title = "用户反馈 - " + time.Now().Format("2006-01-02 15:04:05")
	}
	name := strings.TrimSpace(request.FormValue("name"))
	phone := strings.TrimSpace(request.FormValue("phone"))
	mobile := strings.TrimSpace(request.FormValue("mobile"))
	fax := strings.TrimSpace(request.FormValue("fax"))
	email := strings.TrimSpace(request.FormValue("email"))
	address := strings.TrimSpace(request.FormValue("address"))
	content := strings.TrimSpace(request.FormValue("content"))
	if content == "" {
		content = strings.TrimSpace(request.FormValue("saytext"))
	}
	contentID := parseIntOrZero(request.FormValue("content_id"))

	// Collect extra fields
	standardKeys := map[string]bool{
		"class_id": true, "bid": true, "title": true, "name": true,
		"phone": true, "mobile": true, "fax": true, "email": true,
		"address": true, "content": true, "saytext": true, "content_id": true,
	}
	extraData := make(map[string]any)
	for key, values := range request.Form {
		if !standardKeys[key] && len(values) > 0 {
			if len(values) == 1 {
				extraData[key] = values[0]
			} else {
				extraData[key] = values
			}
		}
	}
	extraBytes, _ := json.Marshal(extraData)

	ip := clientIP(request)
	now := time.Now().Format("2006-01-02 15:04:05")

	_, err = s.database.ExecContext(request.Context(), `
		INSERT INTO "gocms_message"
		("class_id", "title", "name", "phone", "mobile", "fax", "email", "address", "content", "created_at", "state", "content_id", "extra_data", "ip")
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?)`,
		classID, title, name, phone, mobile, fax, email, address, content, now, contentID, string(extraBytes), ip)
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{"ok": true})
}

func clientIP(request *http.Request) string {
	if xff := request.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if host, _, err := net.SplitHostPort(request.RemoteAddr); err == nil {
		return host
	}
	return request.RemoteAddr
}

// Admin Feedbacks

func (s *Server) adminFeedbacks(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet || !s.requireAdmin(response, request) {
		return
	}
	page := positiveInt(request.URL.Query().Get("page"), 1)
	pageSize := positiveInt(request.URL.Query().Get("page_size"), 20)
	if pageSize > 100 {
		pageSize = 100
	}

	classID := parseIntOrZero(request.URL.Query().Get("class_id"))
	stateStr := request.URL.Query().Get("state")
	keyword := strings.TrimSpace(request.URL.Query().Get("keyword"))

	whereClauses := []string{"1=1"}
	var args []any

	if classID > 0 {
		whereClauses = append(whereClauses, `m."class_id" = ?`)
		args = append(args, classID)
	}
	if stateStr == "0" || stateStr == "1" {
		whereClauses = append(whereClauses, `COALESCE(m."state", 0) = ?`)
		args = append(args, stateStr)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		whereClauses = append(whereClauses, `(m."title" LIKE ? OR m."name" LIKE ? OR m."phone" LIKE ? OR m."email" LIKE ? OR m."content" LIKE ? OR m."ip" LIKE ? OR m."extra_data" LIKE ?)`)
		args = append(args, like, like, like, like, like, like, like)
	}

	whereSQL := strings.Join(whereClauses, " AND ")

	var total int64
	countQuery := `SELECT COUNT(*) FROM "gocms_message" m WHERE ` + whereSQL
	if err := s.database.QueryRowContext(request.Context(), countQuery, args...).Scan(&total); err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}

	query := `
		SELECT m."id", m."class_id", COALESCE(c."name", '') as class_name,
		       COALESCE(m."title", ''), COALESCE(m."name", ''), COALESCE(m."phone", ''),
		       COALESCE(m."mobile", ''), COALESCE(m."email", ''), COALESCE(m."address", ''),
		       COALESCE(m."content", ''), COALESCE(m."created_at", ''), COALESCE(m."state", 0),
		       COALESCE(m."content_id", 0), COALESCE(m."ip", ''), COALESCE(m."extra_data", '{}')
		FROM "gocms_message" m
		LEFT JOIN "` + db.FeedbackClassTable + `" c ON c."id" = m."class_id"
		WHERE ` + whereSQL + `
		ORDER BY m."id" DESC LIMIT ? OFFSET ?`

	queryArgs := append(args, pageSize, (page-1)*pageSize)
	rows, err := s.database.QueryContext(request.Context(), query, queryArgs...)
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := make([]FeedbackItem, 0, pageSize)
	for rows.Next() {
		var item FeedbackItem
		var extraJSON string
		if err := rows.Scan(&item.ID, &item.ClassID, &item.ClassName, &item.Title, &item.Name, &item.Phone,
			&item.Mobile, &item.Email, &item.Address, &item.Content, &item.CreatedAt, &item.State,
			&item.ContentID, &item.IP, &extraJSON); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		item.ExtraData = make(map[string]any)
		_ = json.Unmarshal([]byte(extraJSON), &item.ExtraData)
		items = append(items, item)
	}

	writeJSON(response, http.StatusOK, map[string]any{
		"page":      page,
		"page_size": pageSize,
		"total":     total,
		"items":     items,
	})
}

func (s *Server) adminFeedbackItem(response http.ResponseWriter, request *http.Request, rawID string) {
	if !s.requireAdmin(response, request) {
		return
	}
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id < 1 {
		http.Error(response, "invalid id", http.StatusBadRequest)
		return
	}
	switch request.Method {
	case http.MethodPatch:
		var payload struct {
			State int64 `json:"state"`
		}
		if err := decodeRequest(request, &payload); err != nil {
			http.Error(response, "invalid payload", http.StatusBadRequest)
			return
		}
		if payload.State != 0 && payload.State != 1 {
			http.Error(response, "state must be 0 or 1", http.StatusBadRequest)
			return
		}
		_, err = s.database.ExecContext(request.Context(), `UPDATE "gocms_message" SET "state" = ? WHERE "id" = ?`, payload.State, id)
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, map[string]bool{"ok": true})

	case http.MethodDelete:
		result, err := s.database.ExecContext(request.Context(), `DELETE FROM "gocms_message" WHERE "id" = ?`, id)
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			http.Error(response, "not found", http.StatusNotFound)
			return
		}
		writeJSON(response, http.StatusOK, map[string]bool{"ok": true})

	default:
		methodNotAllowed(response)
	}
}

func (s *Server) adminFeedbackBatchDelete(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost || !s.requireAdmin(response, request) {
		return
	}
	var payload struct {
		IDs []int64 `json:"ids"`
	}
	if err := decodeRequest(request, &payload); err != nil || len(payload.IDs) == 0 {
		http.Error(response, "ids array required", http.StatusBadRequest)
		return
	}

	placeholders := make([]string, len(payload.IDs))
	args := make([]any, len(payload.IDs))
	for i, id := range payload.IDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`DELETE FROM "gocms_message" WHERE "id" IN (%s)`, strings.Join(placeholders, ","))
	if _, err := s.database.ExecContext(request.Context(), query, args...); err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

// Admin Feedback Classes

func (s *Server) adminFeedbackClasses(response http.ResponseWriter, request *http.Request) {
	if !s.requireAdmin(response, request) {
		return
	}
	switch request.Method {
	case http.MethodGet:
		rows, err := s.database.QueryContext(request.Context(), `
			SELECT c."id", c."name", c."description", c."fields_config", c."must_fields", c."sort_order", c."created_at",
			       (SELECT COUNT(*) FROM "gocms_message" m WHERE m."class_id" = c."id") as item_count
			FROM "`+db.FeedbackClassTable+`" c
			ORDER BY c."sort_order" ASC, c."id" ASC`)
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		items := make([]FeedbackClassItem, 0)
		for rows.Next() {
			var item FeedbackClassItem
			var cfgJSON, mustJSON string
			if err := rows.Scan(&item.ID, &item.Name, &item.Description, &cfgJSON, &mustJSON, &item.SortOrder, &item.CreatedAt, &item.ItemCount); err != nil {
				http.Error(response, "database error", http.StatusInternalServerError)
				return
			}
			item.FieldsConfig = json.RawMessage(cfgJSON)
			item.MustFields = json.RawMessage(mustJSON)
			items = append(items, item)
		}
		writeJSON(response, http.StatusOK, items)

	case http.MethodPost:
		var payload struct {
			Name         string          `json:"name"`
			Description  string          `json:"description"`
			FieldsConfig json.RawMessage `json:"fields_config"`
			MustFields   json.RawMessage `json:"must_fields"`
			SortOrder    int             `json:"sort_order"`
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
		cfgStr := string(payload.FieldsConfig)
		if strings.TrimSpace(cfgStr) == "" {
			cfgStr = "[]"
		}
		mustStr := string(payload.MustFields)
		if strings.TrimSpace(mustStr) == "" {
			mustStr = "[]"
		}
		now := time.Now().Format("2006-01-02 15:04:05")
		res, err := s.database.ExecContext(request.Context(), `
			INSERT INTO "`+db.FeedbackClassTable+`" ("name", "description", "fields_config", "must_fields", "sort_order", "created_at")
			VALUES (?, ?, ?, ?, ?, ?)`,
			payload.Name, payload.Description, cfgStr, mustStr, payload.SortOrder, now)
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

func (s *Server) adminFeedbackClassItem(response http.ResponseWriter, request *http.Request, rawID string) {
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
			Name         string          `json:"name"`
			Description  string          `json:"description"`
			FieldsConfig json.RawMessage `json:"fields_config"`
			MustFields   json.RawMessage `json:"must_fields"`
			SortOrder    int             `json:"sort_order"`
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
		cfgStr := string(payload.FieldsConfig)
		if strings.TrimSpace(cfgStr) == "" {
			cfgStr = "[]"
		}
		mustStr := string(payload.MustFields)
		if strings.TrimSpace(mustStr) == "" {
			mustStr = "[]"
		}
		if _, err := s.database.ExecContext(request.Context(), `
			UPDATE "`+db.FeedbackClassTable+`"
			SET "name" = ?, "description" = ?, "fields_config" = ?, "must_fields" = ?, "sort_order" = ?
			WHERE "id" = ?`,
			payload.Name, payload.Description, cfgStr, mustStr, payload.SortOrder, id); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"ok": true})

	case http.MethodDelete:
		if id == 1 {
			http.Error(response, "cannot delete default feedback class", http.StatusBadRequest)
			return
		}
		if _, err := s.database.ExecContext(request.Context(), `DELETE FROM "`+db.FeedbackClassTable+`" WHERE "id" = ?`, id); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"ok": true})

	default:
		methodNotAllowed(response)
	}
}

// Admin Feedback Fields

func (s *Server) adminFeedbackFields(response http.ResponseWriter, request *http.Request) {
	if !s.requireAdmin(response, request) {
		return
	}
	switch request.Method {
	case http.MethodGet:
		rows, err := s.database.QueryContext(request.Context(), `
			SELECT "id", "field_name", "field_label", "field_type", "field_options", "description", "sort_order", "is_system"
			FROM "`+db.FeedbackFieldTable+`"
			ORDER BY "sort_order" ASC, "id" ASC`)
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		items := make([]FeedbackFieldItem, 0)
		for rows.Next() {
			var item FeedbackFieldItem
			if err := rows.Scan(&item.ID, &item.FieldName, &item.FieldLabel, &item.FieldType, &item.FieldOptions, &item.Description, &item.SortOrder, &item.IsSystem); err != nil {
				http.Error(response, "database error", http.StatusInternalServerError)
				return
			}
			items = append(items, item)
		}
		writeJSON(response, http.StatusOK, items)

	case http.MethodPost:
		var payload struct {
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
		if payload.FieldName == "" || payload.FieldLabel == "" {
			http.Error(response, "field_name and field_label are required", http.StatusBadRequest)
			return
		}
		if payload.FieldType == "" {
			payload.FieldType = "text"
		}
		res, err := s.database.ExecContext(request.Context(), `
			INSERT INTO "`+db.FeedbackFieldTable+`" ("field_name", "field_label", "field_type", "field_options", "description", "sort_order", "is_system")
			VALUES (?, ?, ?, ?, ?, ?, 0)`,
			payload.FieldName, payload.FieldLabel, payload.FieldType, payload.FieldOptions, payload.Description, payload.SortOrder)
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

func (s *Server) adminFeedbackFieldItem(response http.ResponseWriter, request *http.Request, rawID string) {
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
			UPDATE "`+db.FeedbackFieldTable+`"
			SET "field_label" = ?, "field_type" = ?, "field_options" = ?, "description" = ?, "sort_order" = ?
			WHERE "id" = ?`,
			payload.FieldLabel, payload.FieldType, payload.FieldOptions, payload.Description, payload.SortOrder, id); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"ok": true})

	case http.MethodDelete:
		var isSystem int
		if err := s.database.QueryRowContext(request.Context(), `SELECT "is_system" FROM "`+db.FeedbackFieldTable+`" WHERE "id" = ?`, id).Scan(&isSystem); err != nil {
			http.Error(response, "not found", http.StatusNotFound)
			return
		}
		if isSystem == 1 {
			http.Error(response, "cannot delete system field", http.StatusBadRequest)
			return
		}
		if _, err := s.database.ExecContext(request.Context(), `DELETE FROM "`+db.FeedbackFieldTable+`" WHERE "id" = ?`, id); err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"ok": true})

	default:
		methodNotAllowed(response)
	}
}
