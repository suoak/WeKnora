package access

import (
	"html"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	portalImage         = regexp.MustCompile(`!\[[^\]\r\n]*\]\([^\r\n]*?\)|!\[[^\]\r\n]*\]\[[^\]\r\n]*\]`)
	portalLink          = regexp.MustCompile(`\[([^\]\r\n]*)\]\([^\r\n]*?\)|\[([^\]\r\n]*)\]\[[^\]\r\n]*\]`)
	portalDefinition    = regexp.MustCompile(`(?m)^\s{0,3}\[[^\]\r\n]+\]:[^\r\n]*`)
	portalEntity        = regexp.MustCompile(`&(?:#[xX][0-9a-fA-F]+;?|#[0-9]+;?|[A-Za-z][A-Za-z0-9]*;?)`)
	portalPercent       = regexp.MustCompile(`(?:%[0-9a-fA-F]{2})+`)
	portalFormat        = regexp.MustCompile(`\p{Cf}`)
	portalSchemeSlashes = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*:)[\\/]+`)
	portalLocation      = regexp.MustCompile(`(?i)(?:[a-z][a-z0-9+.-]*:/+|//|(?:resource|storage|backend|local|cos|s3|file|data|javascript):|/(?:files|r|api|preview|download|attachments?)(?:/|\?))[^\s<>"'\[\]{}]*`)
	portalAbsolutePath  = regexp.MustCompile(`(^|[\s"'=:\x60(\[{])(?:[A-Za-z]:[\\/]|\\\\|/)[^\s<>"'\x60\[\]{}(),;!?，。；！？、]+`)
)

// SanitizePortalText projects already-authorized text to link-free inert text.
// All destinations are removed, including ordinary HTTP links: without a file
// broker, a signed URL cannot safely be classified by host or filename. This
// function grants no authority and never changes stored/indexed canonical text.
// Legacy M2/M3 readers retain their strict predicate; only the personal Portal
// surface opts into this projection, followed by the same strict predicate.
func SanitizePortalText(text string) (string, error) {
	if !utf8.ValidString(text) || len(text) > 2*1024*1024 {
		return "", ErrForbidden
	}
	// Normalize only a detection view. Preserve original bytes outside redacted
	// spans: percent escapes and HTML entities are meaningful technical text.
	// Angle brackets are data, including inside inline/fenced code. MCP sends
	// JSON text (not executable HTML); capability destinations remain redacted.
	for _, rule := range []struct {
		pattern *regexp.Regexp
		marker  string
	}{
		{portalImage, "[image omitted]"},
		{portalLink, ""},
		{portalDefinition, "[link omitted]"},
		{portalLocation, "[link omitted]"},
	} {
		var err error
		text, err = redactPortalSpans(text, rule.pattern, rule.marker)
		if err != nil {
			return "", err
		}
	}
	view, err := portalDetectionView(text)
	if err != nil || !SafeDocumentText(view.text) || strings.ContainsRune(view.text, '\x00') {
		return "", ErrForbidden
	}
	return text, nil
}

// SanitizePortalOutputText additionally applies the existing personal MCP
// machine-path containment policy, using the same encoded detection view.
func SanitizePortalOutputText(text string) (string, error) {
	text, err := SanitizePortalText(text)
	if err != nil {
		return "", err
	}
	return redactPortalSpans(text, portalAbsolutePath, "[path omitted]")
}

func redactPortalSpans(text string, pattern *regexp.Regexp, marker string) (string, error) {
	view, err := portalDetectionView(text)
	if err != nil {
		return "", err
	}
	matches := pattern.FindAllStringSubmatchIndex(view.text, -1)
	if len(matches) == 0 {
		return text, nil
	}
	var out strings.Builder
	last := 0
	for _, match := range matches {
		start, end := view.sourceRange(match[0], match[1])
		if start < last { // one encoded entity can expand to multiple runes
			return "", ErrForbidden
		}
		out.WriteString(text[last:start])
		if pattern == portalLink || pattern == portalAbsolutePath {
			for g := 2; g < len(match); g += 2 {
				if match[g] >= 0 && match[g+1] > match[g] {
					a, b := view.sourceRange(match[g], match[g+1])
					out.WriteString(text[a:b])
				}
			}
		}
		out.WriteString(marker)
		last = end
	}
	out.WriteString(text[last:])
	return out.String(), nil
}

// ValidatePortalQuery never rewrites search syntax or literals. Destinations
// which cannot be forwarded to an embedder are rejected instead of silently
// searching for a redaction marker.
func ValidatePortalQuery(text string) error {
	clean, err := SanitizePortalText(text)
	if err != nil || clean != text {
		return ErrForbidden
	}
	return nil
}

type portalTextSpan struct{ start, end int }
type portalTextView struct {
	text  string
	spans []portalTextSpan // nil is the identity map, avoiding allocations for ordinary text
}

func (v portalTextView) sourceRange(start, end int) (int, int) {
	if v.spans == nil {
		return start, end
	}
	return v.spans[start].start, v.spans[end-1].end
}

func (v portalTextView) replace(pattern *regexp.Regexp, replacement func(string) string) portalTextView {
	matches := pattern.FindAllStringIndex(v.text, -1)
	if len(matches) == 0 {
		return v
	}
	var out strings.Builder
	spans := make([]portalTextSpan, 0, len(v.text))
	appendOriginal := func(start, end int) {
		out.WriteString(v.text[start:end])
		for i := start; i < end; i++ {
			if v.spans == nil {
				spans = append(spans, portalTextSpan{i, i + 1})
			} else {
				spans = append(spans, v.spans[i])
			}
		}
	}
	last := 0
	for _, match := range matches {
		appendOriginal(last, match[0])
		value := replacement(v.text[match[0]:match[1]])
		out.WriteString(value)
		start, end := v.sourceRange(match[0], match[1])
		for i := 0; i < len(value); i++ {
			spans = append(spans, portalTextSpan{start, end})
		}
		last = match[1]
	}
	appendOriginal(last, len(v.text))
	return portalTextView{out.String(), spans}
}

func portalDetectionView(text string) (portalTextView, error) {
	v := portalTextView{text: text}
	v = v.replace(portalFormat, func(string) string { return "" })
	for i := 0; i < 3; i++ {
		before := v.text
		v = v.replace(portalEntity, html.UnescapeString)
		v = v.replace(portalPercent, func(s string) string {
			decoded, _ := url.PathUnescape(s) // the regexp admits only complete escapes
			return decoded
		})
		v = v.replace(portalFormat, func(string) string { return "" })
		if v.text == before {
			break
		}
	}
	if decoded, err := url.PathUnescape(v.text); err == nil && decoded != v.text {
		return portalTextView{}, ErrForbidden
	}
	if html.UnescapeString(v.text) != v.text || !utf8.ValidString(v.text) {
		return portalTextView{}, ErrForbidden
	}
	// Some clients accept backslashes as URL separators.
	v = v.replace(portalSchemeSlashes, func(s string) string {
		return s[:strings.IndexByte(s, ':')+1] + "//"
	})
	return v, nil
}
