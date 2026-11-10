// Package utils provides shared application utility functions.
package utils

import "strings"

// IsPersonalMCPPath covers the original and isolated personal MCP paths,
// including rejected suffixes that must never enter an upload log.
func IsPersonalMCPPath(path string) bool {
	return path == "/api/v1/mcp/personal" || strings.HasPrefix(path, "/api/v1/mcp/personal/") ||
		IsIsolatedPersonalMCPPath(path)
}

// IsIsolatedPersonalMCPPath identifies only the isolated server-side route.
func IsIsolatedPersonalMCPPath(path string) bool {
	return path == "/internal/mcp/isolated/personal" ||
		strings.HasPrefix(path, "/internal/mcp/isolated/personal/")
}

// IsAuthenticationPath covers successful, rejected and unknown auth routes.
// Authentication bodies and query strings are opaque to observability layers.
func IsAuthenticationPath(path string) bool {
	return path == "/api/v1/auth" || strings.HasPrefix(path, "/api/v1/auth/") ||
		path == "/api/v1/me/feishu" || strings.HasPrefix(path, "/api/v1/me/feishu/") ||
		path == "/api/v1/system/admin/feishu" || strings.HasPrefix(path, "/api/v1/system/admin/feishu/")
}
