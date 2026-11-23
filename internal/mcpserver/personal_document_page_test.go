package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func (selectedMCPDirectory) LookupPortalDocumentKB(_ context.Context, id string) (string, error) {
	if id != "doc" {
		return "", access.ErrForbidden
	}
	return "selected", nil
}

type largeDocumentPager struct {
	interfaces.ChunkRepository
	calls int
	after func()
}

func (*largeDocumentPager) ListChunksByKnowledgeID(context.Context, uint64, string) ([]*types.Chunk, error) {
	panic("paged MCP reads must not materialize the complete document")
}

func (p *largeDocumentPager) ListReadableTextPage(
	_ context.Context, tenant uint64, doc, kb string, offset, limit int,
) ([]*types.Chunk, int64, error) {
	if tenant != 99 || doc != "doc" || kb != "selected" || limit < 1 || limit > 100 {
		return nil, 0, access.ErrForbidden
	}
	p.calls++
	rows := []*types.Chunk{}
	for i := offset; i < min(offset+limit, 1007); i++ {
		rows = append(rows, &types.Chunk{
			ID: fmt.Sprintf("chunk-%04d", i), TenantID: 99,
			KnowledgeBaseID: kb, KnowledgeID: doc, ChunkIndex: i, IsEnabled: true,
			ChunkType: types.ChunkTypeText, Content: fmt.Sprintf("paragraph %04d <get-config>", i),
		})
	}
	if p.after != nil {
		p.after()
	}
	return rows, 1007, nil
}

func documentPageRPC(ctx context.Context, t *testing.T, s *PersonalServer, offset, limit int) mcp.CallToolResult {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "read_document", "arguments": map[string]any{
			"knowledge_id": "doc", "offset": offset, "limit": limit,
		}},
	})
	require.NoError(t, err)
	r := httptest.NewRequest("POST", personalPath, bytes.NewReader(payload)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	require.Equal(t, 200, w.Code, w.Body.String())
	var envelope struct {
		Result mcp.CallToolResult `json:"result"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	return envelope.Result
}

func TestPersonalDocumentPagesRead1007ChunksWithinLiveSelectedScope(t *testing.T) {
	s, store, ctx := selectedMCPFixture()
	s.knowledge.Knowledge = technicalKnowledgeRepo{}
	pager := &largeDocumentPager{}
	s.knowledge.Chunks = pager
	seen := map[string]bool{}
	for offset := 0; offset < 1007; offset += 100 {
		result := documentPageRPC(ctx, t, s, offset, 100)
		require.False(t, result.IsError)
		var page struct {
			Total  int `json:"total"`
			Chunks []struct {
				ChunkID string `json:"chunk_id"`
				Content string `json:"content"`
			} `json:"chunks"`
		}
		require.NoError(t, json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &page))
		require.Equal(t, 1007, page.Total)
		require.Len(t, page.Chunks, min(100, 1007-offset))
		for _, chunk := range page.Chunks {
			require.False(t, seen[chunk.ChunkID])
			seen[chunk.ChunkID] = true
			require.Contains(t, chunk.Content, "<get-config>")
		}
	}
	require.Len(t, seen, 1007)
	require.Equal(t, 11, pager.calls)
	require.Zero(t, store.unrelatedReads, "another KB cannot enter this document's live read boundary")
}

func TestPersonalDocumentPageDiscardsRevokedOrChangedOutput(t *testing.T) {
	for _, mutation := range []string{"revoked", "changed", "held"} {
		t.Run(mutation, func(t *testing.T) {
			s, store, ctx := selectedMCPFixture()
			s.knowledge.Knowledge = technicalKnowledgeRepo{}
			pager := &largeDocumentPager{after: func() {
				switch mutation {
				case "revoked":
					store.revoked = true
				case "changed":
					store.version = "changed"
				case "held":
					store.held = true
				}
			}}
			s.knowledge.Chunks = pager
			result := documentPageRPC(ctx, t, s, 0, 100)
			require.True(t, result.IsError)
			raw, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "paragraph")
			require.Zero(t, store.unrelatedReads)
		})
	}
}

func TestPersonalDocumentDeniedReadKeepsNeutralRPCWithoutHandler(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		s, underlying, ctx := selectedMCPFixture()
		store := &protocolOnlyStore{selectedMCPStore: underlying}
		if revoked {
			store.err = errors.New("revoked")
		}
		s.knowledge.Resolver.Store = store
		called := false
		s.externalHandler = http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
		body := []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/call",` +
			`"params":{"name":"read_document","arguments":{"knowledge_id":"private-or-missing"}}}`)
		r := httptest.NewRequest("POST", personalPath, bytes.NewReader(body)).WithContext(ctx)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		require.False(t, called, "a protocol grant never admits a document handler")
		require.Equal(t, 1, store.calls)
		if revoked {
			require.Equal(t, http.StatusForbidden, w.Code)
		} else {
			require.Equal(t, http.StatusOK, w.Code)
			var envelope struct {
				ID     int                `json:"id"`
				Result mcp.CallToolResult `json:"result"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
			require.Equal(t, 7, envelope.ID)
			require.True(t, envelope.Result.IsError)
			require.Contains(t, w.Body.String(), "resource denied or unavailable")
			require.NotContains(t, w.Body.String(), "private-or-missing")
		}
	}
}
