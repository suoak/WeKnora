package service

import "strings"

func sourceIDClaimValue(claims map[string]interface{}, path string) interface{} {
	var value interface{} = claims
	for _, part := range strings.Split(path, ".") {
		object, ok := value.(map[string]interface{})
		if !ok || part == "" {
			return nil
		}
		value = object[part]
	}
	return value
}

func strictSourceIDClaim(claims map[string]interface{}, path string) string {
	value := sourceIDClaimValue(claims, path)
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

func sourceIDStringClaim(claims map[string]interface{}, path string) string {
	value, ok := sourceIDClaimValue(claims, path).(string)
	if !ok || len(value) > 512 {
		return ""
	}
	return strings.TrimSpace(value)
}
