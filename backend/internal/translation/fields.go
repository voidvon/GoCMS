package translation

import "strings"

// Text resolves a field using only the requested and configured fallback values.
func Text(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	return ""
}

func empty(value any) bool {
	switch value := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(value) == ""
	case []any:
		return len(value) == 0
	case map[string]any:
		return len(value) == 0
	default:
		return false
	}
}

// ExtraData preserves shared fields and resolves translatable fields separately.
func ExtraData(base, target, fallback map[string]any, fields map[string]bool) map[string]any {
	result := make(map[string]any, len(base))
	for key, value := range base {
		if !fields[key] {
			result[key] = value
		}
	}
	for key := range fields {
		if value := target[key]; !empty(value) {
			result[key] = value
		} else if value := fallback[key]; !empty(value) {
			result[key] = value
		}
	}
	return result
}
