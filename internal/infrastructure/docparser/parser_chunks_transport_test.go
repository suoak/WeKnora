package docparser

import (
	"testing"

	"github.com/Tencent/WeKnora/docreader/proto"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestParserChunksFromProtoPreservesRuneSpansAndMetadata(t *testing.T) {
	t.Parallel()
	spans := parserChunkSpansFromProto([]*proto.ParsedTextSpan{{
		Seq: 1, Start: 3, End: 9,
		Metadata: map[string]string{"parser.sheet": "服务器"},
	}})

	require.Equal(t, []types.ParserChunkSpan{{
		Seq: 1, Start: 3, End: 9,
		Metadata: map[string]string{"parser.sheet": "服务器"},
	}}, spans)
}

func TestChunkingPolicyFromProtoKeepsUnknownPolicyFailClosed(t *testing.T) {
	t.Parallel()
	require.Equal(t, types.ChunkingPolicyDefault,
		chunkingPolicyFromProto(proto.ChunkingPolicy_CHUNKING_POLICY_DEFAULT))
	require.Equal(t, types.ChunkingPolicyPreserveParserChunks,
		chunkingPolicyFromProto(proto.ChunkingPolicy_CHUNKING_POLICY_PRESERVE_PARSER_CHUNKS))
	require.Equal(t, types.ChunkingPolicy("unknown:99"),
		chunkingPolicyFromProto(proto.ChunkingPolicy(99)))
}

func TestFromHTTPReadResponseCarriesParserDefinedChunks(t *testing.T) {
	t.Parallel()
	result := fromHTTPReadResponse(&httpReadResponse{
		MarkdownContent: "中文🙂\r\n",
		ChunkingPolicy:  string(types.ChunkingPolicyPreserveParserChunks),
		ParsedChunks: []httpParsedTextSpan{{
			Seq: 0, Start: 0, End: 5,
			Metadata: map[string]string{"parser.source_kind": "json_object"},
		}},
	})

	require.Equal(t, types.ChunkingPolicyPreserveParserChunks, result.ChunkingPolicy)
	require.Equal(t, []types.ParserChunkSpan{{
		Seq: 0, Start: 0, End: 5,
		Metadata: map[string]string{"parser.source_kind": "json_object"},
	}}, result.ParsedChunks)
}

func TestParserSegmentsFromProtoCarriesRelativeChunksAndMetadata(t *testing.T) {
	t.Parallel()
	segments := parserSegmentsFromProto([]*proto.ParsedSegment{{
		Seq: 0, Start: 2, End: 12,
		ChunkingPolicy: proto.ChunkingPolicy_CHUNKING_POLICY_PRESERVE_PARSER_CHUNKS,
		Metadata:       map[string]string{"parser.sheet": "Servers"},
		ParsedChunks: []*proto.ParsedTextSpan{{
			Seq: 0, Start: 0, End: 10,
			Metadata: map[string]string{"parser.sheet": "Servers", "parser.row": "2"},
		}},
	}})

	require.Equal(t, []types.ParserDefinedSegment{{
		Seq: 0, Start: 2, End: 12,
		ChunkingPolicy: types.ChunkingPolicyPreserveParserChunks,
		Metadata:       map[string]string{"parser.sheet": "Servers"},
		ParsedChunks: []types.ParserChunkSpan{{
			Seq: 0, Start: 0, End: 10,
			Metadata: map[string]string{"parser.sheet": "Servers", "parser.row": "2"},
		}},
	}}, segments)
}

func TestFromHTTPReadResponseCarriesParserDefinedSegments(t *testing.T) {
	t.Parallel()
	result := fromHTTPReadResponse(&httpReadResponse{
		MarkdownContent: "row\n",
		ParsedSegments: []httpParsedSegment{{
			Seq: 0, Start: 0, End: 4,
			ChunkingPolicy: string(types.ChunkingPolicyPreserveParserChunks),
			Metadata:       map[string]string{"parser.sheet": "Data"},
			ParsedChunks: []httpParsedTextSpan{{
				Seq: 0, Start: 0, End: 4,
				Metadata: map[string]string{"parser.row": "2"},
			}},
		}},
	})

	require.Len(t, result.ParsedSegments, 1)
	require.Equal(t, types.ChunkingPolicyPreserveParserChunks,
		result.ParsedSegments[0].ChunkingPolicy)
	require.Equal(t, "Data", result.ParsedSegments[0].Metadata["parser.sheet"])
	require.Equal(t, "2", result.ParsedSegments[0].ParsedChunks[0].Metadata["parser.row"])
}
