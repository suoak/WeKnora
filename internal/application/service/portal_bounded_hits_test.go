package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type boundedPortalChunks struct {
	interfaces.ChunkRepository
	reads int
}

func (s *boundedPortalChunks) GetChunkByID(ctx context.Context, tenant uint64, id string) (*types.Chunk, error) {
	s.reads++
	return s.ChunkRepository.GetChunkByID(ctx, tenant, id)
}

func TestPortalSearchHydratesOnlyFinalRankedHits(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	s := publishedServiceM7(t, f)
	var original types.Chunk
	require.NoError(
		t,
		f.DB.Where("knowledge_base_id=? AND chunk_type=?", "public-kb", types.ChunkTypeText).First(&original).Error,
	)
	var candidates []*types.IndexWithScore
	for i := 0; i < 20; i++ {
		chunk := original
		chunk.ID = fmt.Sprintf("bounded-hit-%02d", i)
		chunk.ChunkIndex = i + 100
		chunk.Content = fmt.Sprintf("bounded candidate %02d", i)
		require.NoError(t, f.DB.Create(&chunk).Error)
		candidates = append(
			candidates,
			&types.IndexWithScore{
				KnowledgeID:     chunk.KnowledgeID,
				KnowledgeBaseID: chunk.KnowledgeBaseID,
				ChunkID:         chunk.ID,
				Content:         chunk.Content,
				SourceType:      types.ChunkSourceType,
			},
		)
	}
	chunks := &boundedPortalChunks{ChunkRepository: s.Chunks}
	s.Chunks = chunks
	s.Engine = func(context.Context, access.PortalKBDescriptor) (access.DocumentRetriever, error) {
		return portalV1Driver{f: func(context.Context, types.RetrieveParams) ([]*types.RetrieveResult, error) {
			return []*types.RetrieveResult{{Results: candidates}}, nil
		}}, nil
	}
	ctx := fixture.Key("A", 1, 1, true)
	result, err := s.Search(ctx, []string{"public-kb"}, "bounded", "keyword", 3)
	require.NoError(t, err)
	require.Len(t, result.Results, 3)
	require.Equal(t, 3, chunks.reads, "the full candidate pool is ranked without hydrating discarded hits")
	for i, hit := range result.Results {
		require.Equal(t, candidates[i].ChunkID, hit.ChunkID)
		require.Equal(t, candidates[i].Content, hit.Text)
		require.NotEmpty(t, hit.Title)
	}
	// A mismatch in a selected result still closes the response. Index text is
	// never emitted in place of the current canonical body.
	candidates[0].Content = "stale index text"
	_, err = s.Search(ctx, []string{"public-kb"}, "bounded", "keyword", 3)
	require.Error(t, err)
	candidates[0].Content = "bounded candidate 00"
	s.Consumers = []func(context.Context, []PortalMCPHit) error{func(context.Context, []PortalMCPHit) error {
		return f.DB.Exec("UPDATE tenant_api_keys SET revoked_at=CURRENT_TIMESTAMP WHERE id=1").Error
	}}
	_, err = s.Search(ctx, []string{"public-kb"}, "bounded", "keyword", 3)
	require.Error(t, err, "final live credential revalidation must discard a concurrently revoked result")
}
