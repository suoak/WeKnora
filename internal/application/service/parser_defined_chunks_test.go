package service

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/infrastructure/chunker"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestMaterializeParserDefinedChunks_DefaultIsUntouched(t *testing.T) {
	t.Parallel()
	result := &types.ReadResult{MarkdownContent: "a\r\nb", ChunkingPolicy: types.ChunkingPolicyDefault}

	chunks, preserved, err := materializeParserDefinedChunks(result, 10)

	require.NoError(t, err)
	require.False(t, preserved)
	require.Nil(t, chunks)
	require.Equal(t, "a\r\nb", result.MarkdownContent)
}

func TestMaterializeParserDefinedChunks_NormalizesEachSpanAndRebuildsRuneOffsets(t *testing.T) {
	t.Parallel()
	parts := []string{
		"LF\n中文🙂\n",
		"CRLF\r\n中文🙂\r\n",
		"CR\r中文🙂\r",
		"cell: 第一行\r\n第二行🙂\n",
	}
	result := preserveResult(parts)
	for i := range result.ParsedChunks {
		result.ParsedChunks[i].Metadata = map[string]string{
			"parser.source_kind": "test_record",
			"parser.row":         string(rune('1' + i)),
		}
	}

	chunks, preserved, err := materializeParserDefinedChunks(result, 100)

	require.NoError(t, err)
	require.True(t, preserved)
	require.Len(t, chunks, len(parts))
	require.Equal(t, "LF\n中文🙂\nCRLF\n中文🙂\nCR\n中文🙂\ncell: 第一行\n第二行🙂\n", result.MarkdownContent)
	cursor := 0
	for i, chunk := range chunks {
		require.Equal(t, i, chunk.Seq)
		require.Equal(t, cursor, chunk.Start)
		require.Equal(t, utf8.RuneCountInString(chunk.Content), chunk.End-chunk.Start)
		require.NotContains(t, chunk.Content, "\r")
		require.Equal(t, "test_record", chunk.Metadata["parser.source_kind"])
		cursor = chunk.End
	}
	require.Equal(t, utf8.RuneCountInString(result.MarkdownContent), cursor)
}

func TestMaterializeParserDefinedChunks_RegressionWideRowAboveOrdinaryChunkSize(t *testing.T) {
	t.Parallel()
	row1 := "编号: 2024,三级规格: 最大并发连接数（IPv4+IPv6）," + strings.Repeat("MODEL-X: 50W,", 360) + "RG-NBR-N7204-E: 50W\n"
	row2 := "编号: 793,三级规格: 静态ARP数量\n"
	require.Greater(t, utf8.RuneCountInString(row1), 4000)
	result := preserveResult([]string{row1, row2})

	chunks, preserved, err := materializeParserDefinedChunks(result, 7500)

	require.NoError(t, err)
	require.True(t, preserved)
	require.Len(t, chunks, 2)
	target := chunks[0].Content
	require.Contains(t, target, "RG-NBR-N7204-E: 50W")
	require.Contains(t, target, "最大并发连接数（IPv4+IPv6）")
	require.NotContains(t, target, "静态ARP数量")
	require.Empty(t, chunks[0].ContextHeader)
	require.Zero(t, chunks[0].ParentIndex)
}

func TestMaterializeParserDefinedChunks_FailsClosed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		result  *types.ReadResult
		hardMax int
		wantErr string
	}{
		{"unknown policy", &types.ReadResult{ChunkingPolicy: "future"}, 10, "unknown parser chunking policy"},
		{"empty spans", &types.ReadResult{ChunkingPolicy: types.ChunkingPolicyPreserveParserChunks, MarkdownContent: "x"}, 10, "requires non-empty"},
		{"duplicate seq", &types.ReadResult{ChunkingPolicy: types.ChunkingPolicyPreserveParserChunks, MarkdownContent: "ab", ParsedChunks: []types.ParserChunkSpan{{Seq: 0, Start: 0, End: 1}, {Seq: 0, Start: 1, End: 2}}}, 10, "duplicate"},
		{"noncontiguous seq", &types.ReadResult{ChunkingPolicy: types.ChunkingPolicyPreserveParserChunks, MarkdownContent: "ab", ParsedChunks: []types.ParserChunkSpan{{Seq: 1, Start: 0, End: 2}}}, 10, "seq must be contiguous"},
		{"gap", &types.ReadResult{ChunkingPolicy: types.ChunkingPolicyPreserveParserChunks, MarkdownContent: "abc", ParsedChunks: []types.ParserChunkSpan{{Seq: 0, Start: 0, End: 1}, {Seq: 1, Start: 2, End: 3}}}, 10, "gap or overlap"},
		{"overlap", &types.ReadResult{ChunkingPolicy: types.ChunkingPolicyPreserveParserChunks, MarkdownContent: "abc", ParsedChunks: []types.ParserChunkSpan{{Seq: 0, Start: 0, End: 2}, {Seq: 1, Start: 1, End: 3}}}, 10, "gap or overlap"},
		{"out of range", &types.ReadResult{ChunkingPolicy: types.ChunkingPolicyPreserveParserChunks, MarkdownContent: "a", ParsedChunks: []types.ParserChunkSpan{{Seq: 0, Start: 0, End: 2}}}, 10, "out of range"},
		{"not fully covered", &types.ReadResult{ChunkingPolicy: types.ChunkingPolicyPreserveParserChunks, MarkdownContent: "ab", ParsedChunks: []types.ParserChunkSpan{{Seq: 0, Start: 0, End: 1}}}, 10, "do not cover complete"},
		{"blank", preserveResult([]string{" \r\n"}), 10, "blank parser semantic chunk"},
		{"too large", preserveResult([]string{"十一二三四"}), 4, "too large"},
		{"metadata namespace", &types.ReadResult{ChunkingPolicy: types.ChunkingPolicyPreserveParserChunks, MarkdownContent: "a", ParsedChunks: []types.ParserChunkSpan{{Seq: 0, Start: 0, End: 1, Metadata: map[string]string{"row": "1"}}}}, 10, "parser. namespace"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := materializeParserDefinedChunks(tt.result, tt.hardMax)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestMergeParserChunkMetadata_DoesNotOverwriteExistingKeys(t *testing.T) {
	t.Parallel()
	existing := types.JSON(`{"parser.row":"owned","custom":"kept"}`)

	merged := mergeParserChunkMetadata(existing, map[string]string{
		"parser.row":   "2",
		"parser.sheet": "Servers",
	})

	metadata, err := merged.Map()
	require.NoError(t, err)
	require.Equal(t, "owned", metadata["parser.row"])
	require.Equal(t, "kept", metadata["custom"])
	require.Equal(t, "Servers", metadata["parser.sheet"])
}

func TestValidateParserDefinedContentUnchangedFailsClosed(t *testing.T) {
	t.Parallel()
	require.NoError(t, validateParserDefinedContentUnchanged(false, "before", "after"))
	require.NoError(t, validateParserDefinedContentUnchanged(true, "same", "same"))
	require.ErrorContains(t,
		validateParserDefinedContentUnchanged(true, "before", "after"),
		"rewritten without synchronized chunk spans",
	)
}

func TestMaterializeParserDefinedSegments_NormalizesAfterTwoLevelMaterialization(t *testing.T) {
	t.Parallel()
	defaultPart := "notes\r\nline\r"
	row1 := "指标: 中文😀\r\n"
	row2 := "指标: next\n"
	content := defaultPart + row1 + row2
	result := &types.ReadResult{
		MarkdownContent: content,
		ParsedSegments: []types.ParserDefinedSegment{
			{
				Seq: 0, Start: 0, End: utf8.RuneCountInString(defaultPart),
				ChunkingPolicy: types.ChunkingPolicyDefault,
				Metadata:       map[string]string{"parser.sheet": "Notes"},
			},
			{
				Seq:            1,
				Start:          utf8.RuneCountInString(defaultPart),
				End:            utf8.RuneCountInString(content),
				ChunkingPolicy: types.ChunkingPolicyPreserveParserChunks,
				Metadata:       map[string]string{"parser.sheet": "Data"},
				ParsedChunks: []types.ParserChunkSpan{
					{Seq: 0, Start: 0, End: utf8.RuneCountInString(row1), Metadata: map[string]string{"parser.sheet": "Data", "parser.row": "2"}},
					{Seq: 1, Start: utf8.RuneCountInString(row1), End: utf8.RuneCountInString(row1 + row2), Metadata: map[string]string{"parser.sheet": "Data", "parser.row": "3"}},
				},
			},
		},
	}

	segments, ok, err := materializeParserDefinedSegments(result, 100)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "notes\nline\n指标: 中文😀\n指标: next\n", result.MarkdownContent)
	require.Len(t, segments, 2)
	require.Equal(t, segments[0].End, segments[1].Start)
	require.Len(t, segments[1].Chunks, 2)
	require.Equal(t, -1, segments[1].Chunks[0].ParentIndex)
	require.Equal(t, "2", segments[1].Chunks[0].Metadata["parser.row"])
	require.Equal(t, utf8.RuneCountInString(result.MarkdownContent), segments[1].End)
}

func TestParserDefinedSegments_MixedParentChildKeepsParentsSegmentLocal(t *testing.T) {
	t.Parallel()
	partA := strings.Repeat("alpha beta gamma delta ", 12)
	partB := "metric: complete semantic row\n"
	partC := strings.Repeat("one two three four five ", 12)
	content := partA + partB + partC
	startB := utf8.RuneCountInString(partA)
	startC := startB + utf8.RuneCountInString(partB)
	result := &types.ReadResult{
		MarkdownContent: content,
		ParsedSegments: []types.ParserDefinedSegment{
			{Seq: 0, Start: 0, End: startB, ChunkingPolicy: types.ChunkingPolicyDefault, Metadata: map[string]string{"parser.sheet": "A"}},
			{Seq: 1, Start: startB, End: startC, ChunkingPolicy: types.ChunkingPolicyPreserveParserChunks, Metadata: map[string]string{"parser.sheet": "B"}, ParsedChunks: []types.ParserChunkSpan{{Seq: 0, Start: 0, End: utf8.RuneCountInString(partB), Metadata: map[string]string{"parser.sheet": "B", "parser.row": "2"}}}},
			{Seq: 2, Start: startC, End: utf8.RuneCountInString(content), ChunkingPolicy: types.ChunkingPolicyDefault, Metadata: map[string]string{"parser.sheet": "C"}},
		},
	}
	segments, ok, err := materializeParserDefinedSegments(result, 1000)
	require.NoError(t, err)
	require.True(t, ok)

	base := chunker.NormalizeSplitterConfig(chunker.SplitterConfig{
		ChunkSize: 30, ChunkOverlap: 5,
		Separators: []string{" "}, Strategy: chunker.StrategyLegacy,
	})
	parent, child := chunker.DeriveParentChildConfigs(base, 80, 25)
	chunks, parents, err := splitMaterializedParserSegments(segments, base, true, parent, child)
	require.NoError(t, err)
	require.NotEmpty(t, parents)

	parentCountA := 0
	for _, parentChunk := range parents {
		if parentChunk.Metadata["parser.sheet"] == "A" {
			parentCountA++
		}
	}
	require.Positive(t, parentCountA)
	seenB := false
	seenCLinked := false
	for index, parsed := range chunks {
		require.Equal(t, index, parsed.Seq)
		switch parsed.Metadata["parser.sheet"] {
		case "A":
			require.NotContains(t, parsed.Content, "metric: complete")
			require.NotContains(t, parsed.Content, "one two")
		case "B":
			seenB = true
			require.Equal(t, -1, parsed.ParentIndex)
			require.Equal(t, partB, parsed.Content)
		case "C":
			require.NotContains(t, parsed.Content, "alpha beta")
			require.NotContains(t, parsed.Content, "metric: complete")
			if parsed.ParentIndex >= 0 {
				seenCLinked = true
				require.GreaterOrEqual(t, parsed.ParentIndex, parentCountA)
			}
		}
	}
	require.True(t, seenB)
	require.True(t, seenCLinked)
}

func TestParserDefinedSegments_MetadataConflictFailsClosed(t *testing.T) {
	t.Parallel()
	result := &types.ReadResult{
		MarkdownContent: "row\n",
		ParsedSegments: []types.ParserDefinedSegment{{
			Seq: 0, Start: 0, End: 4,
			ChunkingPolicy: types.ChunkingPolicyPreserveParserChunks,
			Metadata:       map[string]string{"parser.sheet": "A"},
			ParsedChunks:   []types.ParserChunkSpan{{Seq: 0, Start: 0, End: 4, Metadata: map[string]string{"parser.sheet": "B"}}},
		}},
	}
	_, _, err := materializeParserDefinedSegments(result, 10)
	require.ErrorContains(t, err, "conflicting parser metadata")
}

func TestParserDefinedSegments_FailsClosed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		result  *types.ReadResult
		wantErr string
	}{
		{"unknown policy", &types.ReadResult{MarkdownContent: "a", ParsedSegments: []types.ParserDefinedSegment{{Seq: 0, Start: 0, End: 1, ChunkingPolicy: "future"}}}, "unknown parser segment"},
		{"empty preserve chunks", &types.ReadResult{MarkdownContent: "a", ParsedSegments: []types.ParserDefinedSegment{{Seq: 0, Start: 0, End: 1, ChunkingPolicy: types.ChunkingPolicyPreserveParserChunks}}}, "requires non-empty"},
		{"duplicate segment seq", &types.ReadResult{MarkdownContent: "ab", ParsedSegments: []types.ParserDefinedSegment{{Seq: 0, Start: 0, End: 1}, {Seq: 0, Start: 1, End: 2}}}, "duplicate parser segment"},
		{"segment gap", &types.ReadResult{MarkdownContent: "abc", ParsedSegments: []types.ParserDefinedSegment{{Seq: 0, Start: 0, End: 1}, {Seq: 1, Start: 2, End: 3}}}, "gap or overlap"},
		{"segment overlap", &types.ReadResult{MarkdownContent: "abc", ParsedSegments: []types.ParserDefinedSegment{{Seq: 0, Start: 0, End: 2}, {Seq: 1, Start: 1, End: 3}}}, "gap or overlap"},
		{"segment out of range", &types.ReadResult{MarkdownContent: "a", ParsedSegments: []types.ParserDefinedSegment{{Seq: 0, Start: 0, End: 2}}}, "out of range"},
		{"segment incomplete", &types.ReadResult{MarkdownContent: "ab", ParsedSegments: []types.ParserDefinedSegment{{Seq: 0, Start: 0, End: 1}}}, "do not cover complete"},
		{"blank segment", &types.ReadResult{MarkdownContent: " \r\n", ParsedSegments: []types.ParserDefinedSegment{{Seq: 0, Start: 0, End: 3}}}, "blank parser segment"},
		{"child gap", &types.ReadResult{MarkdownContent: "abc", ParsedSegments: []types.ParserDefinedSegment{{Seq: 0, Start: 0, End: 3, ChunkingPolicy: types.ChunkingPolicyPreserveParserChunks, ParsedChunks: []types.ParserChunkSpan{{Seq: 0, Start: 0, End: 1}, {Seq: 1, Start: 2, End: 3}}}}}, "chunk gap or overlap"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := materializeParserDefinedSegments(tt.result, 100)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func preserveResult(parts []string) *types.ReadResult {
	result := &types.ReadResult{ChunkingPolicy: types.ChunkingPolicyPreserveParserChunks}
	cursor := 0
	for i, part := range parts {
		end := cursor + utf8.RuneCountInString(part)
		result.MarkdownContent += part
		result.ParsedChunks = append(result.ParsedChunks, types.ParserChunkSpan{
			Seq: i, Start: cursor, End: end,
		})
		cursor = end
	}
	return result
}
