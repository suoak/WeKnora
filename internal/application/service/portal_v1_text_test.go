package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type portalV1Driver struct {
	f func(context.Context, types.RetrieveParams) ([]*types.RetrieveResult, error)
}

func (d portalV1Driver) EngineType() types.RetrieverEngineType {
	return types.PostgresRetrieverEngineType
}
func (d portalV1Driver) Retrieve(ctx context.Context, p types.RetrieveParams) ([]*types.RetrieveResult, error) {
	return d.f(ctx, p)
}

func TestPortalV1LinkTextAuthorizationAndModelBoundary(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	s := publishedServiceM7(t, f)
	ctx := fixture.Key("A", 1, 1, true)
	var original types.Chunk
	require.NoError(t, f.DB.Where("knowledge_base_id=? AND chunk_type=?", "public-kb", types.ChunkTypeText).First(&original).Error)
	text := "PUBLIC ordinary office policy [manual](https://docs.test/normal) https://objects.test/private?token=V1_URL_SECRET ![private](/files/V1_URL_SECRET)"
	require.NoError(t, f.DB.Model(&types.Chunk{}).Where("id=?", original.ID).Update("content", text).Error)
	require.NoError(t, f.DB.Model(&types.Knowledge{}).Where("id=?", original.KnowledgeID).Update("title", "PUBLIC [title](https://docs.test/V1_URL_SECRET)").Error)
	rows, err := s.ReadDocument(ctx, original.KnowledgeID)
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	for _, row := range rows {
		require.NotContains(t, row.Text, "V1_URL_SECRET")
		require.True(t, access.SafeDocumentText(row.Text))
		require.True(t, access.SafeDocumentText(row.Title))
	}
	s.Engine = func(context.Context, access.PortalKBDescriptor) (access.DocumentRetriever, error) {
		return portalV1Driver{f: func(context.Context, types.RetrieveParams) ([]*types.RetrieveResult, error) {
			return []*types.RetrieveResult{{Results: []*types.IndexWithScore{{KnowledgeID: original.KnowledgeID, KnowledgeBaseID: original.KnowledgeBaseID, ChunkID: original.ID, SourceType: types.ChunkSourceType, Content: text}}}}, nil
		}}, nil
	}
	embeds, consumed := 0, 0
	s.Embed = func(_ context.Context, _ access.PortalKBDescriptor, q string) ([]float32, error) {
		embeds++
		require.NotContains(t, q, "V1_URL_SECRET")
		return []float32{1}, nil
	}
	s.Consumers = []func(context.Context, []PortalMCPHit) error{func(_ context.Context, hits []PortalMCPHit) error {
		consumed++
		require.NotEmpty(t, hits)
		for _, h := range hits {
			require.NotContains(t, h.Text, "V1_URL_SECRET")
			require.NotContains(t, h.Title, "https:")
			require.Contains(t, h.Text, "ordinary office policy")
		}
		return nil
	}}
	result, err := s.Search(ctx, []string{"public-kb"}, "PUBLIC https://objects.test/V1_URL_SECRET", "hybrid", 10)
	require.NoError(t, err)
	require.NotEmpty(t, result.Results)
	require.Equal(t, 1, embeds)
	require.Equal(t, 1, consumed)
	// A sanitized projection does not authorize an unrelated document or tenant.
	_, err = s.ReadDocument(ctx, "sensitive")
	require.Error(t, err)
	// Model consumer revokes after receiving authorized, sanitized text. Nothing
	// can be returned to the caller after that boundary changes.
	s.Consumers = append(s.Consumers, func(context.Context, []PortalMCPHit) error {
		return f.DB.Exec("UPDATE tenant_portal_configs SET status='draft' WHERE tenant_id=99").Error
	})
	_, err = s.Search(ctx, []string{"public-kb"}, "PUBLIC", "hybrid", 10)
	require.Error(t, err)
	_, err = s.ReadDocument(ctx, original.KnowledgeID)
	require.Error(t, err)
}
