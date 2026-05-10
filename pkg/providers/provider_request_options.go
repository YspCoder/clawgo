package providers

import (
	"fmt"
	"net/url"
	"strings"
)

func rawOption(options map[string]interface{}, key string) (interface{}, bool) {
	if options == nil {
		return nil, false
	}
	v, ok := options[key]
	if !ok || v == nil {
		return nil, false
	}
	return v, true
}

func stringOption(options map[string]interface{}, key string) (string, bool) {
	v, ok := rawOption(options, key)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	return strings.TrimSpace(s), true
}

func mapOption(options map[string]interface{}, key string) (map[string]interface{}, bool) {
	v, ok := rawOption(options, key)
	if !ok {
		return nil, false
	}
	m, ok := v.(map[string]interface{})
	return m, ok
}

func stringSliceOption(options map[string]interface{}, key string) ([]string, bool) {
	v, ok := rawOption(options, key)
	if !ok {
		return nil, false
	}
	switch t := v.(type) {
	case []string:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := strings.TrimSpace(item); s != "" {
				out = append(out, s)
			}
		}
		return out, true
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, item := range t {
			s := strings.TrimSpace(fmt.Sprintf("%v", item))
			if s != "" {
				out = append(out, s)
			}
		}
		return out, true
	}
	return nil, false
}

func mapSliceOption(options map[string]interface{}, key string) ([]map[string]interface{}, bool) {
	v, ok := rawOption(options, key)
	if !ok {
		return nil, false
	}
	switch t := v.(type) {
	case []map[string]interface{}:
		return t, true
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(t))
		for _, item := range t {
			m, ok := item.(map[string]interface{})
			if ok {
				out = append(out, m)
			}
		}
		return out, true
	}
	return nil, false
}

func previewResponseBody(body []byte) string {
	preview := strings.TrimSpace(string(body))
	preview = strings.ReplaceAll(preview, "\n", " ")
	preview = strings.ReplaceAll(preview, "\r", " ")
	if preview == "" {
		return "<empty body>"
	}
	const maxLen = 600
	if len(preview) > maxLen {
		return preview[:maxLen] + "..."
	}
	return preview
}

func int64FromOption(options map[string]interface{}, key string) (int64, bool) {
	if options == nil {
		return 0, false
	}
	v, ok := options[key]
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case int:
		return int64(t), true
	case int64:
		return t, true
	case float64:
		return int64(t), true
	default:
		return 0, false
	}
}

func float64FromOption(options map[string]interface{}, key string) (float64, bool) {
	if options == nil {
		return 0, false
	}
	v, ok := options[key]
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case float32:
		return float64(t), true
	case float64:
		return t, true
	case int:
		return float64(t), true
	default:
		return 0, false
	}
}

func normalizeAPIBase(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return strings.TrimRight(trimmed, "/")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return strings.TrimRight(u.String(), "/")
}

func endpointFor(base, relative string) string {
	b := strings.TrimRight(strings.TrimSpace(base), "/")
	if b == "" {
		return relative
	}
	if strings.HasSuffix(b, relative) {
		return b
	}
	if relative == "/responses/compact" && strings.HasSuffix(b, "/responses") {
		return b + "/compact"
	}
	if relative == "/responses" && strings.HasSuffix(b, "/responses/compact") {
		return strings.TrimSuffix(b, "/compact")
	}
	return b + relative
}
