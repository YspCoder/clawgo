package tools

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func MapStringArg(args map[string]interface{}, key string) string {
	return strings.TrimSpace(MapRawStringArg(args, key))
}

func MapRawStringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	raw, ok := args[key]
	if !ok || raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case json.Number:
		return v.String()
	case fmt.Stringer:
		return v.String()
	case int, int8, int16, int32, int64, float32, float64, bool, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprint(v)
	default:
		return ""
	}
}

func MapBoolArg(args map[string]interface{}, key string) (bool, bool) {
	if args == nil {
		return false, false
	}
	raw, ok := args[key]
	if !ok || raw == nil {
		return false, false
	}
	switch v := raw.(type) {
	case bool:
		return v, true
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "on":
			return true, true
		case "false", "0", "no", "off":
			return false, true
		}
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n != 0, true
		}
	case float64:
		return v != 0, true
	case int:
		return v != 0, true
	case int64:
		return v != 0, true
	}
	return false, false
}

func MapIntArg(args map[string]interface{}, key string, fallback int) int {
	if args == nil {
		return fallback
	}
	raw, ok := args[key]
	if !ok || raw == nil {
		return fallback
	}
	switch v := raw.(type) {
	case int:
		if v > 0 {
			return v
		}
	case int64:
		if v > 0 {
			return int(v)
		}
	case float64:
		if v > 0 {
			return int(v)
		}
	case json.Number:
		if n, err := v.Int64(); err == nil && n > 0 {
			return int(n)
		}
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

func MapStringListArg(args map[string]interface{}, key string) []string {
	if args == nil {
		return nil
	}
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return normalizeArgStringList(v)
	case []interface{}:
		items := make([]string, 0, len(v))
		for _, item := range v {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" && s != "<nil>" {
				items = append(items, s)
			}
		}
		return normalizeArgStringList(items)
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return normalizeArgStringList(strings.Split(v, ","))
	default:
		return nil
	}
}

func MapObjectArg(args map[string]interface{}, key string) map[string]interface{} {
	if args == nil {
		return map[string]interface{}{}
	}
	raw, ok := args[key]
	if !ok || raw == nil {
		return map[string]interface{}{}
	}
	obj, _ := raw.(map[string]interface{})
	if obj == nil {
		return map[string]interface{}{}
	}
	return obj
}

func normalizeArgStringList(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
