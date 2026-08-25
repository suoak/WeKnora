package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/infrastructure/chunker"
	"github.com/Tencent/WeKnora/internal/types"
)

type materializedParserSegment struct {
	Policy   types.ChunkingPolicy
	Content  string
	Start    int
	End      int
	Chunks   []types.ParsedChunk
	Metadata map[string]string
}

// materializeParserDefinedSegments validates a complete raw document
// partition, materializes every segment before normalization, and rebuilds the
// normalized document with global rune offsets. Child spans are relative to
// their segment by protocol definition.
func materializeParserDefinedSegments(
	result *types.ReadResult, hardMaxChars int,
) ([]materializedParserSegment, bool, error) {
	if result == nil || len(result.ParsedSegments) == 0 {
		return nil, false, nil
	}
	if hardMaxChars <= 0 {
		hardMaxChars = types.DefaultParserSemanticChunkMaxChars
	}

	rawRunes := []rune(result.MarkdownContent)
	rawCursor := 0
	normalizedCursor := 0
	parts := make([]string, 0, len(result.ParsedSegments))
	segments := make([]materializedParserSegment, 0, len(result.ParsedSegments))
	seenSeq := make(map[int]struct{}, len(result.ParsedSegments))

	for index, segment := range result.ParsedSegments {
		policy := segment.ChunkingPolicy
		if policy == "" {
			policy = types.ChunkingPolicyDefault
		}
		if _, exists := seenSeq[segment.Seq]; exists {
			return nil, false, fmt.Errorf("duplicate parser segment seq %d", segment.Seq)
		}
		seenSeq[segment.Seq] = struct{}{}
		if segment.Seq != index {
			return nil, false, fmt.Errorf(
				"parser segment seq must be contiguous: got %d, expected %d",
				segment.Seq, index,
			)
		}
		if segment.Start != rawCursor {
			return nil, false, fmt.Errorf(
				"parser segment gap or overlap at seq %d: start=%d, expected=%d",
				segment.Seq, segment.Start, rawCursor,
			)
		}
		if segment.End <= segment.Start || segment.End > len(rawRunes) {
			return nil, false, fmt.Errorf(
				"parser segment span out of range at seq %d: [%d,%d), content_runes=%d",
				segment.Seq, segment.Start, segment.End, len(rawRunes),
			)
		}
		if err := validateParserMetadata(segment.Metadata); err != nil {
			return nil, false, fmt.Errorf("parser segment seq %d: %w", segment.Seq, err)
		}

		rawSegment := string(rawRunes[segment.Start:segment.End])
		materialized := materializedParserSegment{
			Policy:   policy,
			Start:    normalizedCursor,
			Metadata: cloneStringMap(segment.Metadata),
		}
		switch policy {
		case types.ChunkingPolicyDefault:
			materialized.Content = chunker.NormalizeLineEndings(rawSegment)
		case types.ChunkingPolicyPreserveParserChunks:
			chunks, normalized, err := materializeSegmentChunks(
				rawSegment, segment.ParsedChunks, segment.Metadata,
				normalizedCursor, hardMaxChars,
			)
			if err != nil {
				return nil, false, fmt.Errorf("parser segment seq %d: %w", segment.Seq, err)
			}
			materialized.Content = normalized
			materialized.Chunks = chunks
		default:
			return nil, false, fmt.Errorf(
				"unknown parser segment chunking policy %q at seq %d",
				policy, segment.Seq,
			)
		}
		if strings.TrimSpace(materialized.Content) == "" {
			return nil, false, fmt.Errorf("blank parser segment at seq %d", segment.Seq)
		}
		materialized.End = materialized.Start + utf8.RuneCountInString(materialized.Content)
		parts = append(parts, materialized.Content)
		segments = append(segments, materialized)
		rawCursor = segment.End
		normalizedCursor = materialized.End
	}

	if rawCursor != len(rawRunes) {
		return nil, false, fmt.Errorf(
			"parser segments do not cover complete content: covered=%d, content_runes=%d",
			rawCursor, len(rawRunes),
		)
	}
	result.MarkdownContent = strings.Join(parts, "")
	return segments, true, nil
}

func materializeSegmentChunks(
	rawSegment string,
	spans []types.ParserChunkSpan,
	segmentMetadata map[string]string,
	globalStart int,
	hardMaxChars int,
) ([]types.ParsedChunk, string, error) {
	if len(spans) == 0 {
		return nil, "", fmt.Errorf("preserve segment requires non-empty parsed chunks")
	}
	runes := []rune(rawSegment)
	cursor := 0
	normalizedCursor := 0
	parts := make([]string, 0, len(spans))
	chunks := make([]types.ParsedChunk, 0, len(spans))
	seenSeq := make(map[int]struct{}, len(spans))
	for index, span := range spans {
		if _, exists := seenSeq[span.Seq]; exists {
			return nil, "", fmt.Errorf("duplicate parser chunk seq %d", span.Seq)
		}
		seenSeq[span.Seq] = struct{}{}
		if span.Seq != index {
			return nil, "", fmt.Errorf(
				"parser chunk seq must be contiguous: got %d, expected %d", span.Seq, index,
			)
		}
		if span.Start != cursor {
			return nil, "", fmt.Errorf(
				"parser chunk gap or overlap at seq %d: start=%d, expected=%d",
				span.Seq, span.Start, cursor,
			)
		}
		if span.End <= span.Start || span.End > len(runes) {
			return nil, "", fmt.Errorf(
				"parser chunk span out of range at seq %d: [%d,%d), segment_runes=%d",
				span.Seq, span.Start, span.End, len(runes),
			)
		}
		metadata, err := mergeParserMetadataStrict(segmentMetadata, span.Metadata)
		if err != nil {
			return nil, "", fmt.Errorf("parser chunk seq %d: %w", span.Seq, err)
		}
		content := chunker.NormalizeLineEndings(string(runes[span.Start:span.End]))
		if strings.TrimSpace(content) == "" {
			return nil, "", fmt.Errorf("blank parser semantic chunk at seq %d", span.Seq)
		}
		contentLen := utf8.RuneCountInString(content)
		if contentLen > hardMaxChars {
			return nil, "", fmt.Errorf(
				"parser semantic chunk too large at seq %d: size=%d, hard_max_chars=%d",
				span.Seq, contentLen, hardMaxChars,
			)
		}
		chunks = append(chunks, types.ParsedChunk{
			Content:     content,
			Seq:         span.Seq,
			Start:       globalStart + normalizedCursor,
			End:         globalStart + normalizedCursor + contentLen,
			Metadata:    metadata,
			ParentIndex: -1,
		})
		parts = append(parts, content)
		cursor = span.End
		normalizedCursor += contentLen
	}
	if cursor != len(runes) {
		return nil, "", fmt.Errorf(
			"parser chunks do not cover complete segment: covered=%d, segment_runes=%d",
			cursor, len(runes),
		)
	}
	return chunks, strings.Join(parts, ""), nil
}

func validateParserMetadata(metadata map[string]string) error {
	for key := range metadata {
		if !strings.HasPrefix(key, "parser.") {
			return fmt.Errorf("parser metadata key %q must use the parser. namespace", key)
		}
	}
	return nil
}

func mergeParserMetadataStrict(base, additional map[string]string) (map[string]string, error) {
	if err := validateParserMetadata(base); err != nil {
		return nil, err
	}
	if err := validateParserMetadata(additional); err != nil {
		return nil, err
	}
	merged := cloneStringMap(base)
	if merged == nil && len(additional) > 0 {
		merged = make(map[string]string, len(additional))
	}
	for key, value := range additional {
		if existing, exists := merged[key]; exists && existing != value {
			return nil, fmt.Errorf(
				"conflicting parser metadata %q: %q != %q", key, existing, value,
			)
		}
		merged[key] = value
	}
	return merged, nil
}

func splitMaterializedParserSegments(
	segments []materializedParserSegment,
	baseConfig chunker.SplitterConfig,
	enableParentChild bool,
	parentConfig chunker.SplitterConfig,
	childConfig chunker.SplitterConfig,
) ([]types.ParsedChunk, []types.ParsedParentChunk, error) {
	chunks := make([]types.ParsedChunk, 0)
	parents := make([]types.ParsedParentChunk, 0)
	for segmentIndex, segment := range segments {
		switch segment.Policy {
		case types.ChunkingPolicyPreserveParserChunks:
			for _, preserved := range segment.Chunks {
				preserved.Seq = len(chunks)
				preserved.ParentIndex = -1
				chunks = append(chunks, preserved)
			}
		case types.ChunkingPolicyDefault:
			if enableParentChild {
				parentBase := len(parents)
				result := chunker.SplitParentChild(
					segment.Content, parentConfig, childConfig,
				)
				for _, parent := range result.Parents {
					parents = append(parents, types.ParsedParentChunk{
						Content:  parent.Content,
						Seq:      len(parents),
						Start:    segment.Start + parent.Start,
						End:      segment.Start + parent.End,
						Metadata: cloneStringMap(segment.Metadata),
					})
				}
				for _, child := range result.Children {
					parentIndex := -1
					if child.ParentIndex >= 0 {
						parentIndex = parentBase + child.ParentIndex
					}
					chunks = append(chunks, types.ParsedChunk{
						Content:       child.Content,
						ContextHeader: child.ContextHeader,
						Seq:           len(chunks),
						Start:         segment.Start + child.Start,
						End:           segment.Start + child.End,
						ParentIndex:   parentIndex,
						Metadata:      cloneStringMap(segment.Metadata),
					})
				}
			} else {
				for _, split := range chunker.Split(segment.Content, baseConfig) {
					chunks = append(chunks, types.ParsedChunk{
						Content:       split.Content,
						ContextHeader: split.ContextHeader,
						Seq:           len(chunks),
						Start:         segment.Start + split.Start,
						End:           segment.Start + split.End,
						ParentIndex:   -1,
						Metadata:      cloneStringMap(segment.Metadata),
					})
				}
			}
		default:
			return nil, nil, fmt.Errorf(
				"unknown materialized parser segment policy %q at index %d",
				segment.Policy, segmentIndex,
			)
		}
	}
	return chunks, parents, nil
}

// materializeParserDefinedChunks validates spans against the exact raw parser
// output before any length-changing preprocessing, normalizes every semantic
// chunk independently, and rebuilds canonical content and rune offsets.
func materializeParserDefinedChunks(
	result *types.ReadResult, hardMaxChars int,
) ([]types.ParsedChunk, bool, error) {
	if result == nil {
		return nil, false, nil
	}
	policy := result.ChunkingPolicy
	if policy == "" {
		policy = types.ChunkingPolicyDefault
	}
	switch policy {
	case types.ChunkingPolicyDefault:
		return nil, false, nil
	case types.ChunkingPolicyPreserveParserChunks:
		// Continue below.
	default:
		return nil, false, fmt.Errorf("unknown parser chunking policy %q", policy)
	}

	if len(result.ParsedChunks) == 0 {
		return nil, false, fmt.Errorf("preserve_parser_chunks requires non-empty parsed chunks")
	}
	if hardMaxChars <= 0 {
		hardMaxChars = types.DefaultParserSemanticChunkMaxChars
	}

	runes := []rune(result.MarkdownContent)
	cursor := 0
	normalizedCursor := 0
	normalizedParts := make([]string, 0, len(result.ParsedChunks))
	chunks := make([]types.ParsedChunk, 0, len(result.ParsedChunks))
	seenSeq := make(map[int]struct{}, len(result.ParsedChunks))

	for index, span := range result.ParsedChunks {
		if _, exists := seenSeq[span.Seq]; exists {
			return nil, false, fmt.Errorf("duplicate parser chunk seq %d", span.Seq)
		}
		seenSeq[span.Seq] = struct{}{}
		if span.Seq != index {
			return nil, false, fmt.Errorf(
				"parser chunk seq must be contiguous: got %d, expected %d", span.Seq, index,
			)
		}
		if span.Start != cursor {
			return nil, false, fmt.Errorf(
				"parser chunk gap or overlap at seq %d: start=%d, expected=%d",
				span.Seq, span.Start, cursor,
			)
		}
		if span.End <= span.Start || span.End > len(runes) {
			return nil, false, fmt.Errorf(
				"parser chunk span out of range at seq %d: [%d,%d), content_runes=%d",
				span.Seq, span.Start, span.End, len(runes),
			)
		}
		for key := range span.Metadata {
			if !strings.HasPrefix(key, "parser.") {
				return nil, false, fmt.Errorf(
					"parser chunk metadata key %q must use the parser. namespace", key,
				)
			}
		}

		content := chunker.NormalizeLineEndings(string(runes[span.Start:span.End]))
		if strings.TrimSpace(content) == "" {
			return nil, false, fmt.Errorf("blank parser semantic chunk at seq %d", span.Seq)
		}
		contentLen := utf8.RuneCountInString(content)
		if contentLen > hardMaxChars {
			return nil, false, fmt.Errorf(
				"parser semantic chunk too large at seq %d: size=%d, hard_max_chars=%d",
				span.Seq, contentLen, hardMaxChars,
			)
		}
		normalizedEnd := normalizedCursor + contentLen
		chunks = append(chunks, types.ParsedChunk{
			Content:  content,
			Seq:      span.Seq,
			Start:    normalizedCursor,
			End:      normalizedEnd,
			Metadata: cloneStringMap(span.Metadata),
		})
		normalizedParts = append(normalizedParts, content)
		cursor = span.End
		normalizedCursor = normalizedEnd
	}

	if cursor != len(runes) {
		return nil, false, fmt.Errorf(
			"parser chunks do not cover complete content: covered=%d, content_runes=%d",
			cursor, len(runes),
		)
	}
	result.MarkdownContent = strings.Join(normalizedParts, "")
	result.ChunkingPolicy = types.ChunkingPolicyPreserveParserChunks
	return chunks, true, nil
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func validateParserDefinedContentUnchanged(preserved bool, before, after string) error {
	if preserved && before != after {
		return fmt.Errorf(
			"preserve_parser_chunks content was rewritten without synchronized chunk spans",
		)
	}
	return nil
}

// mergeParserChunkMetadata uses the existing Chunk.Metadata JSON column; no
// schema change is needed. Existing keys win so parser provenance never
// overwrites metadata owned by another pipeline stage.
func mergeParserChunkMetadata(existing types.JSON, parserMetadata map[string]string) types.JSON {
	if len(parserMetadata) == 0 {
		return existing
	}
	merged := make(map[string]interface{}, len(parserMetadata))
	if len(existing) > 0 {
		_ = json.Unmarshal(existing, &merged)
	}
	for key, value := range parserMetadata {
		if _, exists := merged[key]; !exists {
			merged[key] = value
		}
	}
	encoded, err := json.Marshal(merged)
	if err != nil {
		return existing
	}
	return types.JSON(encoded)
}
