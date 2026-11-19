package mcpserver

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

type namedMCPDirectory struct{}

func (namedMCPDirectory) ListPortalMCPCandidates(context.Context) ([]access.PortalKBDescriptor, error) {
	return []access.PortalKBDescriptor{
		{ID: "selected", Name: "chosen name", TenantID: 99},
		{ID: "unrelated", TenantID: 99},
	}, nil
}

func TestPersonalMCPWikiSelectionNeverLoadsUnrelatedSnapshots(t *testing.T) {
	for _, name := range []string{"wiki_search", "wiki_index", "wiki_read_page"} {
		for _, selector := range []string{"selected", "chosen name"} {
			t.Run(name+"/"+selector, func(t *testing.T) {
				s, store, ctx := selectedMCPFixture()
				s.knowledge.Resolver.Directory = namedMCPDirectory{}
				args := map[string]any{"knowledge_base_id": selector}
				if name == "wiki_search" {
					args = map[string]any{"knowledge_base_ids": []string{selector}}
				}
				raw, err := json.Marshal(map[string]any{"params": map[string]any{"name": name, "arguments": args}})
				require.NoError(t, err)
				var request mcp.CallToolRequest
				require.NoError(t, json.Unmarshal(raw, &request))
				called := false
				result, err := s.guard(func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					called = true
					return mcp.NewToolResultText("ok"), nil
				})(ctx, request)
				require.NoError(t, err)
				require.False(t, result.IsError)
				require.True(t, called)
				require.Zero(t, store.unrelatedReads)
				s.knowledge.Wiki = boundedWikiReader{}
				// Exercise the coordinator too, including its single-KB name
				// selector. A read with no matching page may be denied, but must
				// never load an unrelated source before making that decision.
				_, err = s.guard(s.call)(ctx, request)
				require.NoError(t, err)
				require.Zero(t, store.unrelatedReads)
				store.revoked = true
				result, err = s.guard(func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					t.Fatal("revoked key reached tool")
					return nil, nil
				})(ctx, request)
				require.NoError(t, err)
				require.True(t, result.IsError)
			})
		}
	}
}

func TestPersonalMCPWikiInvalidSelectorsDoNotExpandScope(t *testing.T) {
	for _, name := range []string{"wiki_index", "wiki_read_page", "wiki_search"} {
		args := map[string]any{"knowledge_base_id": 42, "knowledge_base_ids": []any{42}}
		_, err := personalOperationTargets(name, args)
		require.Error(t, err)
	}
	for _, selector := range []string{"unknown", "not-granted"} {
		s, store, ctx := selectedMCPFixture()
		_, err := s.knowledge.Resolve(ctx, []string{selector})
		require.Error(t, err)
		require.Zero(t, store.unrelatedReads)
	}
}

func TestPersonalMCPGrepUsesUpstreamLiteralTerms(t *testing.T) {
	s, store, ctx := selectedMCPFixture()
	calls := 0
	s.knowledge.Engine = func(context.Context, access.PortalKBDescriptor) (access.DocumentRetriever, error) {
		return boundedPatternDriver{call: func(p types.RetrieveParams) {
			calls++
			require.Equal(t, "timeout deadline", p.Query)
			require.Equal(t, types.KeywordsRetrieverType, p.RetrieverType)
		}}, nil
	}
	request := selectedMCPRequest(
		t,
		map[string]any{"knowledge_base_ids": []string{"selected"}, "query": "timeout|deadline"},
	)
	request.Params.Name = "grep_chunks"
	result, err := s.guard(s.call)(ctx, request)
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Equal(t, 1, calls)
	require.Zero(t, store.unrelatedReads)
	request = selectedMCPRequest(t, map[string]any{"knowledge_base_ids": []string{"selected"}, "query": `^\d+$`})
	request.Params.Name = "grep_chunks"
	result, err = s.guard(s.call)(ctx, request)
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.Equal(t, 1, calls, "patterns without indexable literals must not scan")
}

type boundedPatternDriver struct{ call func(types.RetrieveParams) }

type boundedWikiReader struct{}

func (boundedWikiReader) VerifiedPages(context.Context, uint64, string, []string) ([]*types.WikiPage, bool, error) {
	return nil, false, nil
}
func (boundedWikiReader) RevalidatePages(context.Context, uint64, string, []string, []*types.WikiPage) error {
	return nil
}

func (boundedPatternDriver) EngineType() types.RetrieverEngineType {
	return types.PostgresRetrieverEngineType
}

func (d boundedPatternDriver) Retrieve(_ context.Context, p types.RetrieveParams) ([]*types.RetrieveResult, error) {
	d.call(p)
	return nil, nil
}
