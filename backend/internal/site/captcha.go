package site

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) captchaSlide(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(response)
		return
	}
	if s.captcha == nil {
		http.Error(response, "captcha service not initialized", http.StatusInternalServerError)
		return
	}

	data, err := s.captcha.GenerateSlide()
	if err != nil {
		http.Error(response, "failed to generate captcha", http.StatusInternalServerError)
		return
	}

	writeJSON(response, http.StatusOK, map[string]any{
		"ok":          true,
		"key":         data.Key,
		"image":       data.ImageBase64,
		"tile":        data.TileBase64,
		"tile_width":  data.TileWidth,
		"tile_height": data.TileHeight,
		"tile_x":      data.TileX,
		"tile_y":      data.TileY,
	})
}

func (s *Server) captchaCheck(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(response)
		return
	}
	if s.captcha == nil {
		http.Error(response, "captcha service not initialized", http.StatusInternalServerError)
		return
	}

	key := ""
	pointStr := ""
	userX := 0
	userY := 0

	if strings.Contains(request.Header.Get("Content-Type"), "application/json") {
		var payload struct {
			Key   string `json:"key"`
			Point string `json:"point"`
			X     int    `json:"x"`
			Y     int    `json:"y"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err == nil {
			key = strings.TrimSpace(payload.Key)
			pointStr = strings.TrimSpace(payload.Point)
			userX = payload.X
			userY = payload.Y
		}
	} else {
		if err := request.ParseForm(); err == nil {
			key = strings.TrimSpace(request.FormValue("key"))
			pointStr = strings.TrimSpace(request.FormValue("point"))
			if request.FormValue("x") != "" {
				userX, _ = strconv.Atoi(request.FormValue("x"))
			}
			if request.FormValue("y") != "" {
				userY, _ = strconv.Atoi(request.FormValue("y"))
			}
		}
	}

	if pointStr != "" {
		parts := strings.Split(pointStr, ",")
		if len(parts) >= 1 {
			userX, _ = strconv.Atoi(strings.TrimSpace(parts[0]))
		}
		if len(parts) >= 2 {
			userY, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
		}
	}

	if key == "" {
		writeJSON(response, http.StatusBadRequest, map[string]any{
			"ok":      false,
			"message": "key is required",
		})
		return
	}

	if s.captcha.ValidateSlide(key, userX, userY) {
		writeJSON(response, http.StatusOK, map[string]any{
			"ok":      true,
			"token":   key,
			"message": "success",
		})
		return
	}

	writeJSON(response, http.StatusBadRequest, map[string]any{
		"ok":      false,
		"message": "verification failed",
	})
}
