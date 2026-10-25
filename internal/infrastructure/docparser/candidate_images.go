package docparser

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"html"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type candidateImageEdit struct {
	start, end  int
	replacement string
}

// ErrCandidateImageData identifies a deterministic document-local image refusal.
var ErrCandidateImageData = errors.New("candidate image data unavailable or invalid")

func candidateImageInvalid(reason string) error {
	return fmt.Errorf("%w: %s", ErrCandidateImageData, reason)
}

// ResolveCandidateImages performs no remote downloads and swallows no upload
// errors. Only parser-supplied bytes or inline image data are accepted. The
// caller provides attempt-scoped private storage, not ordinary SaveBytes.
func ResolveCandidateImages(ctx context.Context, result *types.ReadResult, files interfaces.FileService,
	tenant uint64,
) ([]StoredImage, error) {
	before := result.MarkdownContent
	refs := map[string]types.ImageRef{}
	for _, ref := range result.ImageRefs {
		if old, ok := refs[ref.OriginalRef]; ok && sha256.Sum256(old.ImageData) != sha256.Sum256(ref.ImageData) {
			return nil, candidateImageInvalid("candidate image reference is ambiguous")
		}
		refs[ref.OriginalRef] = ref
	}
	var edits []candidateImageEdit
	for _, m := range scanMarkdownImageTargets(before) {
		raw := before[m.TargetStart:m.TargetEnd]
		// Inline data URIs have no parser ImageRef entry. Add only the exact
		// target for the existing destination/title scanner, never a URL fetch.
		if strings.HasPrefix(strings.TrimSpace(raw), "data:image/") {
			refs[strings.TrimSpace(raw)] = types.ImageRef{}
		}
		_, start, end, ok := splitMarkdownImageTarget(raw, refs)
		if !ok {
			return nil, candidateImageInvalid("candidate markdown image target invalid")
		}
		edits = append(edits, candidateImageEdit{start: m.TargetStart + start, end: m.TargetStart + end})
	}
	for _, m := range imgHTMLSrc.FindAllStringSubmatchIndex(before, -1) {
		edits = append(edits, candidateImageEdit{start: m[4], end: m[5]})
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	if len(edits) > 1000 {
		return nil, candidateImageInvalid("candidate image count exceeds limit")
	}
	stored := map[string]StoredImage{}
	var images []StoredImage
	for i := range edits {
		e := &edits[i]
		if i > 0 && e.start < edits[i-1].end {
			return nil, candidateImageInvalid("overlapping candidate image references")
		}
		ref := html.UnescapeString(before[e.start:e.end])
		if old, ok := stored[ref]; ok {
			e.replacement = old.ServingURL
			continue
		}
		var data []byte
		mimeType := ""
		if supplied, ok := refs[ref]; ok && len(supplied.ImageData) > 0 {
			data = supplied.ImageData
			mimeType = supplied.MimeType
		} else if mime, payload, ok := parseImageDataURI(ref); ok {
			var err error
			data, err = decodeBase64Flexible(cleanBase64Payload(payload))
			if err != nil {
				return nil, candidateImageInvalid("candidate image data invalid")
			}
			mimeType = mime
		} else {
			return nil, candidateImageInvalid("candidate image bytes unavailable")
		}
		if len(data) == 0 || len(data) > maxRemoteImageSize {
			return nil, candidateImageInvalid("candidate image size invalid")
		}
		detected := http.DetectContentType(data)
		if !strings.HasPrefix(detected, "image/") {
			return nil, candidateImageInvalid("candidate image content type invalid")
		}
		if mimeType == "" {
			mimeType = detected
		}
		name := fmt.Sprintf("%x%s", sha256.Sum256(data), extFromMime(detected))
		path, err := files.SaveBytes(ctx, data, tenant, name, false)
		if err != nil {
			return nil, err
		}
		img := StoredImage{OriginalRef: ref, ServingURL: path, MimeType: mimeType}
		stored[ref] = img
		images = append(images, img)
		e.replacement = path
	}
	// Translate exact rune boundaries. A boundary inside a replaced URL is
	// ambiguous and must fail, never discard parser semantic segmentation.
	position := func(offset int) (int, error) {
		delta := 0
		for _, e := range edits {
			start, end := utf8.RuneCountInString(before[:e.start]), utf8.RuneCountInString(before[:e.end])
			if offset > start && offset < end {
				return 0, candidateImageInvalid("parser boundary inside image reference")
			}
			if offset >= end {
				delta += utf8.RuneCountInString(e.replacement) - (end - start)
			}
		}
		return offset + delta, nil
	}
	rangeMap := func(start, end *int) error {
		a, err := position(*start)
		if err != nil {
			return err
		}
		b, err := position(*end)
		if err != nil {
			return err
		}
		*start, *end = a, b
		return nil
	}
	for i := range result.ParsedChunks {
		p := &result.ParsedChunks[i]
		if err := rangeMap(&p.Start, &p.End); err != nil {
			return nil, err
		}
	}
	for i := range result.ParsedSegments {
		p := &result.ParsedSegments[i]
		oldStart := p.Start
		for j := range p.ParsedChunks {
			c := &p.ParsedChunks[j]
			a, b := oldStart+c.Start, oldStart+c.End
			if err := rangeMap(&a, &b); err != nil {
				return nil, err
			}
			base, err := position(oldStart)
			if err != nil {
				return nil, err
			}
			c.Start, c.End = a-base, b-base
		}
		if err := rangeMap(&p.Start, &p.End); err != nil {
			return nil, err
		}
	}
	for i := range result.SourceBlocks {
		p := &result.SourceBlocks[i]
		if err := rangeMap(&p.Start, &p.End); err != nil {
			return nil, err
		}
	}
	var out strings.Builder
	cursor := 0
	for _, e := range edits {
		out.WriteString(before[cursor:e.start])
		out.WriteString(e.replacement)
		cursor = e.end
	}
	out.WriteString(before[cursor:])
	result.MarkdownContent = out.String()
	return images, nil
}
