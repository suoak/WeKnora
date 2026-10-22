package access

import (
	"html"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	portalImage         = regexp.MustCompile(`!\[[^\]\r\n]*\]\([^\r\n]*?\)|!\[[^\]\r\n]*\]\[[^\]\r\n]*\]`)
	portalLink          = regexp.MustCompile(`\[([^\]\r\n]*)\]\([^\r\n]*?\)|\[([^\]\r\n]*)\]\[[^\]\r\n]*\]`)
	portalDefinition    = regexp.MustCompile(`(?m)^\s{0,3}\[[^\]\r\n]+\]:[^\r\n]*`)
	portalTag           = regexp.MustCompile(`<[^>\r\n]*>`)
	portalSchemeSlashes = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*:)[\\/]+`)
	portalLocation      = regexp.MustCompile(`(?i)(?:[a-z][a-z0-9+.-]*:/+|//|(?:resource|storage|backend|local|cos|s3|file|data|javascript):|/(?:files|r|api|preview|download|attachments?)(?:/|\?))[^\s<>"'\[\]{}]*`)
)

// SanitizePortalText projects already-authorized text to link-free plain text.
// All destinations are removed, including ordinary HTTP links: without a file
// broker, a signed URL cannot safely be classified by host or filename. This
// function grants no authority and never changes stored/indexed canonical text.
// Legacy M2/M3 readers retain their strict predicate; only the personal Portal
// surface opts into this projection, followed by the same strict predicate.
func SanitizePortalText(text string) (string, error) {
	if !utf8.ValidString(text) || len(text) > 2*1024*1024 {
		return "", ErrForbidden
	}
	text = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, text)
	// Decode bounded common encodings before stripping. Further nesting fails
	// closed rather than allowing an escaped bearer URL through the last gate.
	for i := 0; i < 3; i++ {
		next := html.UnescapeString(text)
		if decoded, err := url.PathUnescape(next); err == nil {
			next = decoded
		}
		if next == text {
			break
		}
		text = next
	}
	if decoded, err := url.PathUnescape(text); err == nil && decoded != text {
		return "", ErrForbidden
	}
	if html.UnescapeString(text) != text {
		return "", ErrForbidden
	}
	// Backslashes are accepted by some clients as URL separators.
	text = portalSchemeSlashes.ReplaceAllString(text, "$1//")
	text = portalImage.ReplaceAllString(text, "[image omitted]")
	text = portalLink.ReplaceAllString(text, "$1$2")
	text = portalDefinition.ReplaceAllString(text, "[link omitted]")
	text = portalTag.ReplaceAllString(text, "[markup omitted]")
	text = portalLocation.ReplaceAllString(text, "[link omitted]")
	if !SafeDocumentText(text) || strings.ContainsRune(text, '\x00') {
		return "", ErrForbidden
	}
	return text, nil
}
