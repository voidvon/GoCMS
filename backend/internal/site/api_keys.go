package site

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"gocms/internal/apikey"
)

func (s *Server) adminApiKeys(response http.ResponseWriter, request *http.Request) {
	user, ok := s.requireAdminSession(response, request)
	if !ok {
		return
	}

	switch request.Method {
	case http.MethodGet:
		keys, err := apikey.List(request.Context(), s.database, nil)
		if err != nil {
			http.Error(response, "database error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{
			"ok":      true,
			"success": true,
			"data":    keys,
		})

	case http.MethodPost:
		var input struct {
			Name      string  `json:"name"`
			ExpiresAt *string `json:"expires_at"`
		}
		if err := decodeRequest(request, &input); err != nil {
			writeJSON(response, http.StatusBadRequest, map[string]any{
				"error":   "无效的请求格式",
				"message": "无效的请求格式",
				"code":    "API_KEY_INPUT_INVALID",
			})
			return
		}

		created, err := apikey.Create(request.Context(), s.database, user.ID, user.ID, apikey.CreateInput{
			Name:      input.Name,
			ExpiresAt: input.ExpiresAt,
		}, clientIP(request))
		if err != nil {
			handleApiKeyError(response, err, "API Key 创建失败")
			return
		}

		response.Header().Set("Cache-Control", "private, no-store, max-age=0")
		writeJSON(response, http.StatusCreated, map[string]any{
			"ok":      true,
			"success": true,
			"data":    created,
			"message": "API Key 已创建。完整 Key 只会显示这一次，请立即保存。",
		})

	default:
		methodNotAllowed(response)
	}
}

func (s *Server) adminApiKeyRoute(response http.ResponseWriter, request *http.Request, subpath string) {
	user, ok := s.requireAdminSession(response, request)
	if !ok {
		return
	}

	parts := strings.Split(strings.Trim(subpath, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(response, request)
		return
	}

	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		writeJSON(response, http.StatusBadRequest, map[string]any{
			"error":   "无效的 API Key ID",
			"message": "无效的 API Key ID",
			"code":    "API_KEY_INPUT_INVALID",
		})
		return
	}

	if len(parts) == 1 {
		switch request.Method {
		case http.MethodGet:
			key, err := apikey.GetByID(request.Context(), s.database, id)
			if err != nil {
				handleApiKeyError(response, err, "获取 API Key 失败")
				return
			}
			writeJSON(response, http.StatusOK, map[string]any{
				"ok":      true,
				"success": true,
				"data":    key,
			})
		case http.MethodDelete:
			revoked, err := apikey.Revoke(request.Context(), s.database, id, user.ID, clientIP(request), "")
			if err != nil {
				handleApiKeyError(response, err, "撤销 API Key 失败")
				return
			}
			writeJSON(response, http.StatusOK, map[string]any{
				"ok":      true,
				"success": true,
				"data":    revoked,
				"message": "API Key 已撤销",
			})
		default:
			methodNotAllowed(response)
		}
		return
	}

	action := strings.ToLower(parts[1])
	switch action {
	case "rotate":
		if request.Method != http.MethodPost {
			methodNotAllowed(response)
			return
		}
		rotated, err := apikey.Rotate(request.Context(), s.database, id, user.ID, clientIP(request))
		if err != nil {
			handleApiKeyError(response, err, "API Key 轮换失败")
			return
		}
		response.Header().Set("Cache-Control", "private, no-store, max-age=0")
		writeJSON(response, http.StatusOK, map[string]any{
			"ok":      true,
			"success": true,
			"data":    rotated,
			"message": "API Key 已轮换。旧 Key 已立即失效，请立即保存新 Key。",
		})

	case "revoke":
		if request.Method != http.MethodPost {
			methodNotAllowed(response)
			return
		}
		var input struct {
			Reason string `json:"reason"`
		}
		_ = decodeRequest(request, &input)
		revoked, err := apikey.Revoke(request.Context(), s.database, id, user.ID, clientIP(request), input.Reason)
		if err != nil {
			handleApiKeyError(response, err, "撤销 API Key 失败")
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{
			"ok":      true,
			"success": true,
			"data":    revoked,
			"message": "API Key 已撤销",
		})

	case "events":
		if request.Method != http.MethodGet {
			methodNotAllowed(response)
			return
		}
		events, err := apikey.ListEvents(request.Context(), s.database, id)
		if err != nil {
			handleApiKeyError(response, err, "获取 API Key 事件失败")
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{
			"ok":      true,
			"success": true,
			"data":    events,
		})

	default:
		http.NotFound(response, request)
	}
}

func handleApiKeyError(response http.ResponseWriter, err error, fallbackMessage string) {
	if errors.Is(err, apikey.ErrNotFound) {
		writeJSON(response, http.StatusNotFound, map[string]any{
			"error":   "API Key 不存在",
			"message": "API Key 不存在",
			"code":    "API_KEY_NOT_FOUND",
		})
		return
	}
	if errors.Is(err, apikey.ErrRevoked) {
		writeJSON(response, http.StatusConflict, map[string]any{
			"error":   "已撤销的 API Key 不能轮换",
			"message": "已撤销的 API Key 不能轮换",
			"code":    "API_KEY_REVOKED",
		})
		return
	}
	if errors.Is(err, apikey.ErrExpired) {
		writeJSON(response, http.StatusConflict, map[string]any{
			"error":   "已过期的 API Key 不能轮换",
			"message": "已过期的 API Key 不能轮换",
			"code":    "API_KEY_EXPIRED",
		})
		return
	}
	if errors.Is(err, apikey.ErrNameRequired) ||
		errors.Is(err, apikey.ErrNameTooLong) ||
		errors.Is(err, apikey.ErrExpiresInvalid) ||
		errors.Is(err, apikey.ErrExpiresPast) {
		writeJSON(response, http.StatusBadRequest, map[string]any{
			"error":   err.Error(),
			"message": err.Error(),
			"code":    "API_KEY_INPUT_INVALID",
		})
		return
	}
	writeJSON(response, http.StatusInternalServerError, map[string]any{
		"error":   fallbackMessage + ": " + err.Error(),
		"message": fallbackMessage,
		"code":    "API_KEY_OPERATION_FAILED",
	})
}
