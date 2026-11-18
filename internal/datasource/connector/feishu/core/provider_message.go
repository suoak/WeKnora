package core

// SafeProviderMessage preserves reviewed, content-free provider phrases exactly
// as returned. Unknown text may echo a URL, credential or document content, so
// it is omitted rather than guessed, truncated or reconstructed from a code.
func SafeProviderMessage(message string) string {
	switch message {
	case "no permission", "No permission", "permission denied", "Permission denied",
		"access denied", "Access denied", "forbidden", "Forbidden",
		"无权限", "没有权限", "无阅读或导出权限", "无导出权限",
		"too many request", "too many requests", "Too many requests",
		"not found", "Not found", "file not found", "File not found",
		"invalid access token", "access token expired":
		return message
	default:
		return ""
	}
}
