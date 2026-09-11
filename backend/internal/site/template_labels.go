package site

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"gocms/internal/generator"
	"gocms/internal/templatelabel"
)

type templateLabelsResponse struct {
	Page         int                      `json:"page"`
	PageSize     int                      `json:"page_size"`
	Total        int64                    `json:"total"`
	Items        []templatelabel.Label    `json:"items"`
	Categories   []templatelabel.Category `json:"categories"`
	TemplateTags []generator.TemplateTag  `json:"template_tags"`
}

func (s *Server) adminThemeLabelTemplates(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodPost {
		methodNotAllowed(response)
		return
	}
	if !s.requireAdmin(response, request) {
		return
	}
	if request.Method == http.MethodPost {
		var input templatelabel.Input
		if err := decodeRequest(request, &input); err != nil {
			http.Error(response, "invalid template label payload", http.StatusBadRequest)
			return
		}
		if err := s.validateTemplateLabelCategory(request, input.CategoryID); err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		item, err := templatelabel.Create(request.Context(), s.database, input)
		if err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		s.templateLabelSaved(response, request, item)
		return
	}

	page := positiveInt(request.URL.Query().Get("page"), 1)
	pageSize := positiveInt(request.URL.Query().Get("page_size"), 20)
	categoryID := parseIntOrZero(request.URL.Query().Get("category_id"))
	result, err := templatelabel.List(request.Context(), s.database, request.URL.Query().Get("q"), categoryID, page, pageSize)
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	categories, err := templatelabel.Categories(request.Context(), s.database)
	if err != nil {
		http.Error(response, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(response, http.StatusOK, templateLabelsResponse{
		Page: result.Page, PageSize: result.PageSize, Total: result.Total,
		Items: result.Items, Categories: categories, TemplateTags: generator.TemplateTags(),
	})
}

func (s *Server) adminThemeLabelTemplate(response http.ResponseWriter, request *http.Request, rawID string) {
	id, err := parseTemplateLabelID(rawID)
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.requireAdmin(response, request) {
		return
	}
	switch request.Method {
	case http.MethodGet:
		item, err := templatelabel.Get(request.Context(), s.database, id)
		if err == sql.ErrNoRows {
			http.Error(response, "标签模板不存在", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, item)
	case http.MethodPut:
		var input templatelabel.Input
		if err := decodeRequest(request, &input); err != nil {
			http.Error(response, "invalid template label payload", http.StatusBadRequest)
			return
		}
		if err := s.validateTemplateLabelCategory(request, input.CategoryID); err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		item, err := templatelabel.Update(request.Context(), s.database, id, input)
		if err != nil {
			status := http.StatusBadRequest
			if strings.Contains(err.Error(), "不存在") {
				status = http.StatusNotFound
			}
			http.Error(response, err.Error(), status)
			return
		}
		s.templateLabelSaved(response, request, item)
	case http.MethodDelete:
		if err := templatelabel.Delete(request.Context(), s.database, id); err != nil {
			status := http.StatusInternalServerError
			if strings.Contains(err.Error(), "不存在") {
				status = http.StatusNotFound
			}
			http.Error(response, err.Error(), status)
			return
		}
		s.templateLabelDeleted(response, request)
	default:
		methodNotAllowed(response)
	}
}

func (s *Server) adminThemeLabelCategories(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodPost {
		methodNotAllowed(response)
		return
	}
	if !s.requireAdmin(response, request) {
		return
	}
	if request.Method == http.MethodGet {
		categories, err := templatelabel.Categories(request.Context(), s.database)
		if err != nil {
			http.Error(response, "database error", http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, categories)
		return
	}
	var payload struct {
		Name      string `json:"name"`
		SortOrder int64  `json:"sort_order"`
	}
	if err := decodeRequest(request, &payload); err != nil {
		http.Error(response, "invalid template label category payload", http.StatusBadRequest)
		return
	}
	category, err := templatelabel.CreateCategory(request.Context(), s.database, payload.Name, payload.SortOrder)
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"ok": true, "category": category})
}

func (s *Server) adminThemeLabelCategory(response http.ResponseWriter, request *http.Request, rawID string) {
	id, err := parseTemplateLabelID(rawID)
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.requireAdmin(response, request) {
		return
	}
	switch request.Method {
	case http.MethodPut:
		var payload struct {
			Name      string `json:"name"`
			SortOrder int64  `json:"sort_order"`
		}
		if err := decodeRequest(request, &payload); err != nil {
			http.Error(response, "invalid template label category payload", http.StatusBadRequest)
			return
		}
		category, err := templatelabel.UpdateCategory(request.Context(), s.database, id, payload.SortOrder, payload.Name)
		if err != nil {
			status := http.StatusBadRequest
			if strings.Contains(err.Error(), "不存在") {
				status = http.StatusNotFound
			}
			http.Error(response, err.Error(), status)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"ok": true, "category": category})
	case http.MethodDelete:
		if err := templatelabel.DeleteCategory(request.Context(), s.database, id); err != nil {
			status := http.StatusInternalServerError
			if strings.Contains(err.Error(), "不存在") {
				status = http.StatusNotFound
			}
			http.Error(response, err.Error(), status)
			return
		}
		writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
	default:
		methodNotAllowed(response)
	}
}

func parseTemplateLabelID(rawID string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(rawID), 10, 64)
	if err != nil || id < 1 {
		return 0, fmt.Errorf("标签模板编号无效")
	}
	return id, nil
}

func (s *Server) validateTemplateLabelCategory(request *http.Request, categoryID int64) error {
	if categoryID == 0 {
		return nil
	}
	var exists int64
	if err := s.database.QueryRowContext(request.Context(), `SELECT COUNT(*) FROM "gocms_template_label_category" WHERE "id" = ?`, categoryID).Scan(&exists); err != nil {
		return fmt.Errorf("读取标签模板分类失败")
	}
	if exists == 0 {
		return fmt.Errorf("标签模板分类不存在")
	}
	return nil
}

func (s *Server) templateLabelSaved(response http.ResponseWriter, request *http.Request, item templatelabel.Label) {
	if request.URL.Query().Get("publish") == "1" && s.publication != nil {
		report, started := s.startPublish(true)
		writeJSON(response, http.StatusOK, map[string]any{"ok": true, "item": item, "publication": report, "publish_started": started})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"ok": true, "item": item})
}

func (s *Server) templateLabelDeleted(response http.ResponseWriter, request *http.Request) {
	if request.URL.Query().Get("publish") == "1" && s.publication != nil {
		report, started := s.startPublish(true)
		writeJSON(response, http.StatusOK, map[string]any{"ok": true, "publication": report, "publish_started": started})
		return
	}
	writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
}
