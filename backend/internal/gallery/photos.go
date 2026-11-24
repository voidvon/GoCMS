package gallery

import (
	"encoding/json"
	"strings"
)

type Photo struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Order int    `json:"order,omitempty"`
}

// Parse accepts editor arrays, serialized arrays and legacy line-delimited galleries.
func Parse(value any) []Photo {
	if raw, ok := value.(string); ok {
		raw = strings.TrimSpace(raw)
		if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
			var entries []any
			if json.Unmarshal([]byte(raw), &entries) == nil {
				return Parse(entries)
			}
		}
		var photos []Photo
		for index, line := range strings.Split(raw, "\n") {
			parts := strings.Split(strings.TrimSpace(line), "::::::")
			photo := Photo{URL: strings.TrimSpace(parts[0]), Order: index + 1}
			if len(parts) >= 3 {
				photo.Title = strings.TrimSpace(parts[2])
			} else if len(parts) == 2 {
				photo.Title = strings.TrimSpace(parts[1])
			}
			if photo.URL != "" {
				photos = append(photos, photo)
			}
		}
		return photos
	}
	entries, _ := value.([]any)
	var photos []Photo
	for index, entry := range entries {
		photo := Photo{Order: index + 1}
		switch item := entry.(type) {
		case string:
			photo.URL = item
		case map[string]any:
			photo.URL, _ = item["url"].(string)
			photo.Title, _ = item["title"].(string)
			if order, ok := item["order"].(float64); ok && order != 0 {
				photo.Order = int(order)
			}
		}
		photo.URL = strings.TrimSpace(photo.URL)
		if photo.URL != "" {
			photos = append(photos, photo)
		}
	}
	return photos
}
