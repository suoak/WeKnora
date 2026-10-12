package service

import "strings"

func strictSourceIDClaim(claims map[string]interface{}, path string) string {
	var value interface{} = claims
	for _, part := range strings.Split(path, ".") {
		object, ok := value.(map[string]interface{})
		if !ok || part == "" {
			return ""
		}
		value = object[part]
	}
	switch v := value.(type) {
	case string:
		if len(v) > 512 {
			return ""
		}
		return strings.TrimSpace(v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}
