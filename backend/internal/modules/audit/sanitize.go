package audit

import "encoding/json"

// Sanitize removes secrets from a JSON snapshot before it is persisted.
func Sanitize(raw JSON) JSON {
	if raw.IsZero() {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	cleaned, err := json.Marshal(sanitizeValue(value))
	if err != nil {
		return nil
	}
	return JSON(cleaned)
}

func sanitizeValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		cleaned := make(map[string]any, len(v))
		for key, item := range v {
			if SensitiveKey(key) {
				continue
			}
			cleaned[key] = sanitizeValue(item)
		}
		return cleaned
	case []any:
		cleaned := make([]any, len(v))
		for i, item := range v {
			cleaned[i] = sanitizeValue(item)
		}
		return cleaned
	default:
		return value
	}
}
