package service

import (
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/spreadsheet"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestBuildSpreadsheetEntityChunksFromParserSchemas(t *testing.T) {
	schemas, err := json.Marshal([]spreadsheet.Schema{{
		SheetName:     "SPEC",
		EntityAxis:    "columns",
		EntityHeaders: []string{"Model-A", "Model-B"},
		EntityCount:   2,
		Confidence:    0.95,
	}})
	require.NoError(t, err)

	chunks, err := buildSpreadsheetEntityChunks(
		&types.Knowledge{ID: "knowledge-1", KnowledgeBaseID: "kb-1", TenantID: 1},
		map[string]string{spreadsheet.SchemasMetadataKey: string(schemas)},
		7,
	)

	require.NoError(t, err)
	require.Len(t, chunks, 1)
	require.Equal(t, types.ChunkTypeSheetEntityIndex, chunks[0].ChunkType)
	require.Equal(t, 7, chunks[0].ChunkIndex)
	require.Contains(t, chunks[0].Content, "Model-A")
	require.JSONEq(t, `{
		"parser":{"sheet":"SPEC"},
		"spreadsheet":{"entity_axis":"columns","entity_count":2,"confidence":0.95}
	}`, string(chunks[0].Metadata))
}
