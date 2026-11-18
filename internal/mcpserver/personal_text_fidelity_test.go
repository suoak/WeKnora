package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

type technicalKnowledgeRepo struct{ interfaces.KnowledgeRepository }

type technicalMCPDirectory struct{}

func (technicalMCPDirectory) ListPortalMCPCandidates(context.Context) ([]access.PortalKBDescriptor, error) {
	return []access.PortalKBDescriptor{{ID: "selected", TenantID: 99}}, nil
}

func (technicalKnowledgeRepo) GetKnowledgeByID(context.Context, uint64, string) (*types.Knowledge, error) {
	return &types.Knowledge{ID: "doc", TenantID: 99, KnowledgeBaseID: "selected", Title: "NETCONF <get-config>"}, nil
}

type technicalChunkRepo struct {
	interfaces.ChunkRepository
	text string
}

func (r technicalChunkRepo) ListChunksByKnowledgeID(context.Context, uint64, string) ([]*types.Chunk, error) {
	return []*types.Chunk{{ID: "technical", TenantID: 99, KnowledgeBaseID: "selected", KnowledgeID: "doc", Content: r.text, IsEnabled: true, ChunkType: types.ChunkTypeText}}, nil
}

func TestPersonalTechnicalReadDocumentHTTPAndQuery(t *testing.T) {
	s, _, ctx := selectedMCPFixture()
	s.knowledge.Resolver.Directory = technicalMCPDirectory{}
	text := "| **NETCONF 操作** | `<edit-config>`, `<get-config>` | `<get>` |\n```xml\n<rpc><get/></rpc>\n```\n&lt;filter&gt; %2F"
	s.knowledge.Knowledge = technicalKnowledgeRepo{}
	s.knowledge.Chunks = technicalChunkRepo{text: text}
	for _, query := range []string{"", "<get-config>"} {
		payload, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": 1, "method": "tools/call",
			"params": map[string]any{"name": "read_document", "arguments": map[string]any{"knowledge_id": "doc", "query": query}},
		})
		require.NoError(t, err)
		r := httptest.NewRequest("POST", personalPath, bytes.NewReader(payload)).WithContext(ctx)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		require.Equal(t, 200, w.Code, w.Body.String())
		var rpc struct {
			Result mcp.CallToolResult `json:"result"`
			Error  json.RawMessage    `json:"error"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rpc))
		require.Empty(t, rpc.Error, w.Body.String())
		require.False(t, rpc.Result.IsError, w.Body.String())
		require.NotEmpty(t, rpc.Result.Content)
		var result struct {
			Total  int                        `json:"total"`
			Chunks []struct{ Content string } `json:"chunks"`
		}
		require.NoError(t, json.Unmarshal([]byte(rpc.Result.Content[0].(mcp.TextContent).Text), &result))
		require.Equal(t, 1, result.Total)
		require.Len(t, result.Chunks, 1)
		require.Equal(t, text, result.Chunks[0].Content)
	}
}
