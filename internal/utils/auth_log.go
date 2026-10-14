package utils

import "strings"

// IsAuthenticationPath covers successful, rejected and unknown auth routes.
// Authentication bodies and query strings are opaque to observability layers.
func IsAuthenticationPath(path string) bool {
	return path == "/api/v1/auth" || strings.HasPrefix(path, "/api/v1/auth/") ||
		path == "/api/v1/me/feishu" || strings.HasPrefix(path, "/api/v1/me/feishu/") ||
		path == "/api/v1/system/admin/feishu" || strings.HasPrefix(path, "/api/v1/system/admin/feishu/")
}
