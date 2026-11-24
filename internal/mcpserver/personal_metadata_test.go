package mcpserver

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func testCatalogMetadata(targets []*access.PortalKBTarget) map[string]access.PortalKBMetadata {
	rows := map[string]access.PortalKBMetadata{}
	for _, target := range targets {
		d := target.Descriptor()
		rows[d.ID] = access.PortalKBMetadata{
			ID:          d.ID,
			Description: "Authoritative description",
			CreatorName: "Display name",
			Type:        "document",
			CreatedAt:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
			UpdatedAt:   time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC),
		}
	}
	return rows
}

func (selectedMCPDirectory) LoadPortalMCPMetadata(
	_ context.Context, targets []*access.PortalKBTarget,
) (map[string]access.PortalKBMetadata, error) {
	return testCatalogMetadata(targets), nil
}

func (*catalogScopeDirectory) LoadPortalMCPMetadata(
	_ context.Context, targets []*access.PortalKBTarget,
) (map[string]access.PortalKBMetadata, error) {
	return testCatalogMetadata(targets), nil
}

func (technicalMCPDirectory) LoadPortalMCPMetadata(
	_ context.Context, targets []*access.PortalKBTarget,
) (map[string]access.PortalKBMetadata, error) {
	return testCatalogMetadata(targets), nil
}

func (namedMCPDirectory) LoadPortalMCPMetadata(
	_ context.Context, targets []*access.PortalKBTarget,
) (map[string]access.PortalKBMetadata, error) {
	return testCatalogMetadata(targets), nil
}

func TestPersonalMetadataListAndExactIDDetail(t *testing.T) {
	s, _, _, ctx := catalogScopeFixture()
	var list mcp.CallToolRequest
	list.Params.Name = "list_knowledge_bases"
	result, err := s.guard(s.call)(ctx, list)
	require.NoError(t, err)
	require.False(t, result.IsError, "%+v", result.Content)
	var rows []map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &rows))
	require.Len(t, rows, 1)
	row := rows[0]
	require.Equal(t, "Authoritative description", row["description"])
	require.Equal(t, "Display name", row["creator_name"])
	require.Equal(t, "2026-01-02T03:04:05Z", row["created_at"])
	require.Equal(t, "2026-02-03T04:05:06Z", row["updated_at"])
	require.Equal(t, "document", row["type"])
	require.EqualValues(t, 1, row["document_count"])
	require.NotContains(t, row, "creator_id")
	require.NotContains(t, row, "email")
	for _, id := range []string{"selected", "wrong", "catalog-selected", ""} {
		var req mcp.CallToolRequest
		req.Params.Name = "get_knowledge_base"
		req.Params.Arguments = map[string]any{"knowledge_base_id": id}
		result, err = s.guard(s.call)(ctx, req)
		require.NoError(t, err)
		if id != "selected" {
			require.True(t, result.IsError)
			continue
		}
		require.False(t, result.IsError, "%+v", result.Content)
		var detail map[string]any
		require.NoError(t, json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &detail))
		require.Equal(t, row, detail)
	}
}
