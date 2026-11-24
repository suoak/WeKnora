package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

type largePortalMetadataStore struct {
	*selectedMCPStore
	count int
}

func (s *largePortalMetadataStore) LoadDocumentReadSnapshot(
	ctx context.Context, user string, key uint64, kb string,
) (*types.EnterpriseReadSnapshot, error) {
	snapshot, err := s.selectedMCPStore.LoadDocumentReadSnapshot(ctx, user, key, kb)
	if err != nil {
		return nil, err
	}
	template := snapshot.Documents[0]
	snapshot.Documents = nil
	for i := 0; i < s.count; i++ {
		row := template
		row.ID = fmt.Sprintf("doc-%04d", i)
		snapshot.Documents = append(snapshot.Documents, row)
	}
	return snapshot, nil
}

type pageMetadataRepository struct {
	interfaces.KnowledgeRepository
	requested []int
	titleOnly []bool
	after     func()
}

func (*pageMetadataRepository) GetKnowledgeByID(context.Context, uint64, string) (*types.Knowledge, error) {
	panic("pagination must not read all document bodies")
}

func (r *pageMetadataRepository) LoadPortalDocumentMetadata(
	_ context.Context, tenant uint64, kb string, ids []string, titlesOnly bool,
) ([]*types.Knowledge, error) {
	r.requested = append(r.requested, len(ids))
	r.titleOnly = append(r.titleOnly, titlesOnly)
	rows := []*types.Knowledge{}
	for _, id := range ids {
		rows = append(rows, &types.Knowledge{
			ID:              id,
			TenantID:        tenant,
			KnowledgeBaseID: kb,
			Title:           "NETCONF <get-config> " + id,
			FileName:        id + ".txt",
			FileType:        "txt",
			Type:            "file",
			ParseStatus:     "completed",
			CreatedAt:       time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
			UpdatedAt:       time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC),
		})
	}
	if r.after != nil {
		r.after()
	}
	return rows, nil
}

func largeMetadataFixture(
	count int,
) (*PersonalServer, *largePortalMetadataStore, *pageMetadataRepository, context.Context) {
	s, base, _, ctx := catalogScopeFixture()
	store := &largePortalMetadataStore{selectedMCPStore: base.selectedMCPStore, count: count}
	s.knowledge.Resolver.Store = store
	s.knowledge.Resolver.Documents = access.NewDocumentReader(true, store, selectedMCPBase{}, selectedMCPEmployee{})
	repo := &pageMetadataRepository{}
	s.knowledge.Knowledge = repo
	return s, store, repo, ctx
}

func TestPersonalDocumentMetadataPaginationIsBoundedAndCountsAgree(t *testing.T) {
	s, _, repo, ctx := largeMetadataFixture(1007)
	var request mcp.CallToolRequest
	request.Params.Name = "list_documents"
	request.Params.Arguments = map[string]any{"knowledge_base_id": "selected", "page": 2, "page_size": 20}
	result, err := s.guard(s.call)(ctx, request)
	require.NoError(t, err)
	require.False(t, result.IsError, "%+v", result.Content)
	require.Equal(t, []int{20}, repo.requested)
	require.Equal(t, []bool{false}, repo.titleOnly)
	var page struct {
		Total     int              `json:"total"`
		Documents []map[string]any `json:"documents"`
	}
	require.NoError(t, json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &page))
	require.Equal(t, 1007, page.Total)
	require.Len(t, page.Documents, 20)
	require.Equal(t, "doc-0020", page.Documents[0]["knowledge_id"])
	require.Equal(t, "doc-0020.txt", page.Documents[0]["file_name"])
	require.Equal(t, "completed", page.Documents[0]["parse_status"])
	require.Equal(t, "2026-01-02T03:04:05Z", page.Documents[0]["created_at"])
	request.Params.Name = "get_knowledge_base"
	request.Params.Arguments = map[string]any{"knowledge_base_id": "selected"}
	result, err = s.guard(s.call)(ctx, request)
	require.NoError(t, err)
	require.False(t, result.IsError)
	count := result.StructuredContent.(map[string]any)["document_count"].(json.Number).String()
	require.Equal(t, fmt.Sprint(page.Total), count)
}

func TestPersonalDocumentKeywordTotalsAndEmptyPages(t *testing.T) {
	s, _, repo, ctx := largeMetadataFixture(1007)
	for _, test := range []struct {
		keyword           string
		page, total, rows int
	}{
		{"doc-00", 2, 100, 20}, {"<get-config>", 51, 1007, 7}, {"missing", 1, 0, 0}, {"", 10000, 1007, 0},
	} {
		var request mcp.CallToolRequest
		request.Params.Name = "list_documents"
		request.Params.Arguments = map[string]any{
			"knowledge_base_id": "selected",
			"keyword":           test.keyword,
			"page":              test.page,
			"page_size":         20,
		}
		result, err := s.guard(s.call)(ctx, request)
		require.NoError(t, err)
		require.False(t, result.IsError, "%+v", result.Content)
		value := result.StructuredContent.(map[string]any)
		require.Equal(t, fmt.Sprint(test.total), value["total"].(json.Number).String())
		require.Len(t, value["documents"], test.rows)
	}
	for _, size := range repo.requested {
		require.LessOrEqual(t, size, 500)
	}
}

func TestPersonalPagedMetadataDiscardsRevokedOutput(t *testing.T) {
	s, store, repo, ctx := largeMetadataFixture(1007)
	repo.after = func() { store.revoked = true }
	var request mcp.CallToolRequest
	request.Params.Name = "list_documents"
	request.Params.Arguments = map[string]any{"knowledge_base_id": "selected"}
	result, err := s.guard(s.call)(ctx, request)
	require.NoError(t, err)
	require.True(t, result.IsError)
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "doc-0000.txt")
}
