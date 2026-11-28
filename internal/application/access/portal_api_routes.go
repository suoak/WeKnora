package access

import (
	"regexp"
	"strings"
)

var (
	portalVersionedRouteCapability = regexp.MustCompile(`(?i)/v[1-9][0-9]{0,2}/[^\s<>"'\x60\[\]{}]*[?#][^\s<>"'\x60\[\]{}]*`)
	portalVersionedAPIRoute        = regexp.MustCompile(`^/v[1-9][0-9]{0,2}(?:/[a-z][A-Za-z0-9_-]{0,39}){2,12}/$`)
	portalHTTPMethod               = regexp.MustCompile("(?:^|[ \\t|`])(?:GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)(?:$|[ \\t|`])")
	portalURLField                 = regexp.MustCompile("(?im)^\\s*(?:[-*]\\s*)?(?:URL|URI|Path|接口路径)\\s*[:：]")
	portalMethodField              = regexp.MustCompile("(?im)^\\s*(?:[-*]\\s*)?(?:Method|HTTP Method|方法|请求方法)\\s*[:：]\\s*`?(?:GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\\b")
)

// A generic absolute path cannot establish that it is an API route. This
// deliberately narrow opt-in requires a plain ASCII versioned route, at least
// two named segments and an explicit HTTP method in the same line or bounded
// URL/method paragraph. Encoded text, filesystem roots, broker namespaces,
// secrets, traversal, filename extensions and identifier-like segments remain
// on the ordinary redaction path. Nothing is fetched or resolved from a route.
func documentedPortalAPIRoute(original string, view portalTextView, match []int) bool {
	if len(match) < 4 || match[3] < 0 {
		return false
	}
	start, end := match[3], match[1]
	route := view.text[start:end]
	if !portalVersionedAPIRoute.MatchString(route) {
		return false
	}
	a, b := view.sourceRange(start, end)
	if original[a:b] != route { // no encoded workaround of containment
		return false
	}
	for _, segment := range strings.Split(strings.Trim(route, "/"), "/")[1:] {
		if len(segment) >= 16 && strings.Trim(segment, "0123456789abcdefABCDEF") == "" {
			return false
		}
		switch strings.ToLower(segment) {
		case "api", "r", "file", "files", "resource", "resources", "storage", "preview", "download",
			"attachment", "attachments", "upload", "uploads", "cos", "s3", "backend", "local",
			"etc", "home", "root", "var", "tmp", "mnt", "proc", "sys", "dev", "app",
			"token", "secret", "password", "credentials", "auth", "oauth":
			return false
		}
		// Route actions may contain short version numbers; long digit runs,
		// UUIDs, digests and opaque capability-looking tokens are not actions.
		if strings.Count(segment, "-") >= 3 {
			return false
		}
		run := 0
		for _, r := range segment {
			if r >= '0' && r <= '9' {
				run++
				if run >= 4 {
					return false
				}
			} else {
				run = 0
			}
		}
	}
	lineStart := strings.LastIndex(view.text[:start], "\n") + 1
	lineEnd := len(view.text)
	if next := strings.IndexByte(view.text[end:], '\n'); next >= 0 {
		lineEnd = end + next
	}
	line := view.text[lineStart:lineEnd]
	if len(line) <= 2048 && portalHTTPMethod.MatchString(line) {
		return true
	}
	paragraphStart := strings.LastIndex(view.text[:start], "\n\n") + 2
	if paragraphStart == 1 {
		paragraphStart = 0
	}
	paragraphEnd := len(view.text)
	if next := strings.Index(view.text[end:], "\n\n"); next >= 0 {
		paragraphEnd = end + next
	}
	paragraph := view.text[paragraphStart:paragraphEnd]
	return len(paragraph) <= 1024 && portalURLField.MatchString(paragraph) && portalMethodField.MatchString(paragraph)
}
