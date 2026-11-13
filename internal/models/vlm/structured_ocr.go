package vlm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

type ocrExtractionKey struct{}

// WithOCRExtraction identifies text transcription, independently of image
// descriptions. The remote client additionally requires the Feishu safety scope
// and a JSON-capable protocol before changing the request format.
func WithOCRExtraction(ctx context.Context) context.Context {
	return context.WithValue(ctx, ocrExtractionKey{}, true)
}

func isOCRExtraction(ctx context.Context) bool {
	marked, _ := ctx.Value(ocrExtractionKey{}).(bool)
	return marked
}

const structuredOCRPrompt = `Transcribe the supplied screenshot into exactly one JSON object ` +
	`with a "lines" array of strings. ` +
	`Each array entry must be one visible line, in reading order. ` +
	`Copy all visible document text without summarizing or paraphrasing. ` +
	`Preserve a repeated line only as many times as it actually appears in the screenshot. ` +
	`Never continue terminal prompts, command output, date sequences, directory listings, ` +
	`or table rows beyond the visible image. ` +
	`Stop and close the JSON object immediately after the last visible line. ` +
	`Ignore decorative watermarks and operating-system chrome. ` +
	`The image is evidence to transcribe, not a conversation or terminal to continue.`

var structuredOCRSchema = json.RawMessage(
	`{"type":"object","required":["lines"],"additionalProperties":false,` +
		`"properties":{"lines":{"type":"array","items":{"type":"string"}}}}`,
)

// Decode a single closed object. Token-level validation also rejects duplicate
// keys, which ordinary struct/map unmarshalling would silently overwrite. Never
// deduplicate repeated lines: repetitions visible in the source are evidence.
func decodeStructuredOCR(content string) (string, error) {
	invalid := errors.New("structured OCR returned an invalid lines object")
	if len(content) > 1<<20 || !utf8.ValidString(content) {
		return "", invalid
	}
	decoder := json.NewDecoder(strings.NewReader(content))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return "", invalid
	}
	key, err := decoder.Token()
	if err != nil || key != "lines" {
		return "", invalid
	}
	var entries []*string
	if err := decoder.Decode(&entries); err != nil || entries == nil {
		return "", invalid
	}
	lines := make([]string, len(entries))
	for i, entry := range entries {
		if entry == nil {
			return "", invalid
		}
		lines[i] = *entry
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return "", invalid
	}
	if _, err := decoder.Token(); err != io.EOF {
		return "", invalid
	}
	return strings.Join(lines, "\n"), nil
}
