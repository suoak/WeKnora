package service

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/infrastructure/chunker"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// TestRealParserSegmentsAcceptance is an opt-in bridge for validating a real
// DocReader wire result without committing private source documents or writing
// to a production database. Set WEKNORA_REAL_PARSER_OUTPUT_JSON to a JSON file
// containing a types.ReadResult-shaped payload.
func TestRealParserSegmentsAcceptance(t *testing.T) {
	path := os.Getenv("WEKNORA_REAL_PARSER_OUTPUT_JSON")
	if path == "" {
		t.Skip("WEKNORA_REAL_PARSER_OUTPUT_JSON is not set")
	}
	payload, err := os.ReadFile(path)
	require.NoError(t, err)
	var result types.ReadResult
	require.NoError(t, json.Unmarshal(payload, &result))

	segments, segmented, err := materializeParserDefinedSegments(&result, 7500)
	require.NoError(t, err)
	require.True(t, segmented)
	require.NotEmpty(t, segments)

	config := chunker.NormalizeSplitterConfig(chunker.SplitterConfig{
		ChunkSize: 4000, ChunkOverlap: 200,
	})
	chunks, parents, err := splitMaterializedParserSegments(
		segments, config, false, chunker.SplitterConfig{}, chunker.SplitterConfig{},
	)
	require.NoError(t, err)
	require.Empty(t, parents)

	metric := "最大并发连接数（IPv4+IPv6）"
	product := "RG-NBR-N7204-E: 50W"
	var target *types.ParsedChunk
	longPreservedRow := false
	longPreservedRows := make([]string, 0)
	defaultOpticsChunks := 0
	documentRunes := []rune(result.MarkdownContent)
	for index := range chunks {
		parsed := &chunks[index]
		require.Equal(t, index, parsed.Seq)
		require.Equal(t, parsed.Content, string(documentRunes[parsed.Start:parsed.End]))
		if parsed.Metadata["parser.source_kind"] == "excel_row" &&
			utf8.RuneCountInString(parsed.Content) > 4000 {
			longPreservedRow = true
			longPreservedRows = append(longPreservedRows,
				parsed.Metadata["parser.sheet"]+":"+parsed.Metadata["parser.row"])
		}
		if strings.Contains(parsed.Content, metric) && strings.Contains(parsed.Content, product) {
			target = parsed
		}
	}
	require.NotNil(t, target)
	require.Equal(t, "容量指标", target.Metadata["parser.sheet"])
	require.Equal(t, -1, target.ParentIndex)
	require.NotContains(t, target.Content, "编号: 793")
	require.NotContains(t, target.Content, "静态ARP数量")
	require.True(t, longPreservedRow, "expected a real preserved row above chunk_size")

	for _, parsed := range chunks {
		if parsed.Metadata["parser.sheet"] == "光模块兼容性" {
			defaultOpticsChunks++
			require.NotContains(t, parsed.Content, metric)
			require.NotContains(t, parsed.Content, product)
		}
	}
	t.Logf("target seq=%d start=%d end=%d chars=%d sheet=%s row=%s",
		target.Seq, target.Start, target.End, utf8.RuneCountInString(target.Content),
		target.Metadata["parser.sheet"], target.Metadata["parser.row"])
	t.Logf("real preserved rows above 4000=%v", longPreservedRows)
	t.Logf("光模块兼容性 default chunks=%d", defaultOpticsChunks)
}
