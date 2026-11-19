package service

import (
	"context"
	"regexp"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPortalPatternSearchFiltersCanonicalChunksAndRetainsRevocation(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	s := publishedServiceM7(t, f)
	ctx := fixture.Key("A", 1, 1, true)
	var chunk types.Chunk
	require.NoError(
		t,
		f.DB.Where("knowledge_base_id=? AND chunk_type=?", "public-kb", types.ChunkTypeText).First(&chunk).Error,
	)
	require.NoError(
		t,
		f.DB.Model(&types.Chunk{}).Where("id=?", chunk.ID).Update("content", "timeout on port 443").Error,
	)
	s.Engine = func(context.Context, access.PortalKBDescriptor) (access.DocumentRetriever, error) {
		return portalV1Driver{f: func(_ context.Context, p types.RetrieveParams) ([]*types.RetrieveResult, error) {
			require.Equal(t, "timeout deadline", p.Query)
			require.Equal(t, types.KeywordsRetrieverType, p.RetrieverType)
			var current types.Chunk
			require.NoError(t, f.DB.Where("id=?", chunk.ID).First(&current).Error)
			return []*types.RetrieveResult{
				{
					Results: []*types.IndexWithScore{
						{
							KnowledgeID:     chunk.KnowledgeID,
							KnowledgeBaseID: chunk.KnowledgeBaseID,
							ChunkID:         chunk.ID,
							SourceType:      types.ChunkSourceType,
							Content:         current.Content,
						},
					},
				},
			}, nil
		}}, nil
	}
	pattern := regexp.MustCompile("(?i)timeout|deadline")
	result, err := s.SearchPattern(ctx, []string{"public-kb"}, "timeout deadline", pattern, 10)
	require.NoError(t, err)
	require.Len(t, result.Results, 1)
	require.Equal(t, "timeout on port 443", result.Results[0].Text)
	require.NoError(
		t,
		f.DB.Model(&types.Chunk{}).Where("id=?", chunk.ID).Update("content", "unrelated indexed candidate").Error,
	)
	result, err = s.SearchPattern(ctx, []string{"public-kb"}, "timeout deadline", pattern, 10)
	require.NoError(t, err)
	require.Empty(t, result.Results, "keyword candidates must still satisfy the regex on canonical text")
	require.NoError(t, f.DB.Exec("UPDATE tenant_api_keys SET revoked_at=CURRENT_TIMESTAMP WHERE id=1").Error)
	_, err = s.SearchPattern(ctx, []string{"public-kb"}, "timeout deadline", pattern, 10)
	require.Error(t, err)
}
