package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPortalConsistencyModeFailureMatrix(t *testing.T) {
	for _, scenario := range []struct {
		name                       string
		keyword, vector, embedding bool
		status                     string
		hits                       int
	}{
		{"both", false, false, false, "complete", 1},
		{"vector", false, true, false, "partial", 1},
		{"keyword", true, false, false, "partial", 1},
		{"embedding", false, false, true, "partial", 1},
		{"both_failed", true, true, false, "failed", 0},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f := fixture.New(t)
			f.PreparePublished(t)
			s := publishedServiceM7(t, f)
			var c types.Chunk
			require.NoError(
				t,
				f.DB.Where("knowledge_base_id=? AND chunk_type=?", "public-kb", types.ChunkTypeText).First(&c).Error,
			)
			s.Embed = func(context.Context, access.PortalKBDescriptor, string) ([]float32, error) {
				if scenario.embedding {
					return nil, errors.New("secret provider error")
				}
				return []float32{1}, nil
			}
			s.Engine = func(context.Context, access.PortalKBDescriptor) (access.DocumentRetriever, error) {
				return portalV1Driver{
					f: func(_ context.Context, p types.RetrieveParams) ([]*types.RetrieveResult, error) {
						if p.RetrieverType == types.KeywordsRetrieverType && scenario.keyword ||
							p.RetrieverType == types.VectorRetrieverType && scenario.vector {
							return nil, errors.New("secret database error")
						}
						return []*types.RetrieveResult{
							{
								Results: []*types.IndexWithScore{
									{
										KnowledgeID:     c.KnowledgeID,
										KnowledgeBaseID: c.KnowledgeBaseID,
										ChunkID:         c.ID,
										Content:         c.Content,
										SourceType:      types.ChunkSourceType,
									},
								},
							},
						}, nil
					},
				}, nil
			}
			result, e := s.Search(fixture.Key("A", 1, 1, true), []string{"public-kb"}, "query", "hybrid", 10)
			require.NoError(t, e)
			require.Equal(t, scenario.status, result.Status)
			require.Len(t, result.Results, scenario.hits)
			if scenario.hits > 0 {
				require.Equal(t, c.ID, result.Results[0].ChunkID)
				require.Equal(t, 1, result.Searched)
			} else {
				require.Equal(t, 1, result.Failed)
			}
			for _, failure := range result.PartialFailures {
				require.NotContains(t, failure.Reason, "secret")
			}
		})
	}
}

func TestPortalConsistencyIndexModes(t *testing.T) {
	for _, test := range []struct {
		name            string
		keyword, vector bool
		kind            types.RetrieverType
	}{
		{"keyword", true, false, types.KeywordsRetrieverType},
		{"vector", false, true, types.VectorRetrieverType},
		{"wiki", false, false, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := fixture.New(t)
			f.PreparePublished(t)
			s := publishedServiceM7(t, f)
			strategy := types.IndexingStrategy{
				KeywordEnabled: test.keyword,
				VectorEnabled:  test.vector,
				WikiEnabled:    true,
			}
			require.NoError(
				t,
				f.DB.Model(&types.KnowledgeBase{}).
					Where("id=?", "public-kb").
					Update("indexing_strategy", strategy).
					Error,
			)
			s.Embed = func(context.Context, access.PortalKBDescriptor, string) ([]float32, error) {
				return []float32{1}, nil
			}
			calls := 0
			s.Engine = func(context.Context, access.PortalKBDescriptor) (access.DocumentRetriever, error) {
				return portalV1Driver{
					f: func(_ context.Context, p types.RetrieveParams) ([]*types.RetrieveResult, error) {
						calls++
						require.Equal(t, test.kind, p.RetrieverType)
						return nil, nil
					},
				}, nil
			}
			result, e := s.Search(fixture.Key("A", 1, 1, true), []string{"public-kb"}, "query", "hybrid", 10)
			require.NoError(t, e)
			if test.kind == "" {
				require.Equal(t, "no_searchable_index", result.Status)
				require.Zero(t, calls)
			} else {
				require.Equal(t, "complete", result.Status)
				require.Equal(t, 1, calls)
				require.Len(t, result.ModeFallbacks, 1)
			}
			// FAQ does not advertise a keyword index merely because the common
			// indexing strategy has the keyword flag set.
			require.NoError(t, f.DB.Model(&types.KnowledgeBase{}).
				Where("id=?", "public-kb").Update("type", types.KnowledgeBaseTypeFAQ).Error)
			caps, e := s.Capabilities(fixture.Key("A", 1, 1, true), access.PortalKBDescriptor{
				ID: "public-kb", TenantID: 99, EmbeddingModelID: "synthetic", IndexingStrategy: &strategy,
			})
			require.NoError(t, e)
			require.NotContains(t, caps, "keyword")
			require.Equal(t, test.vector, len(caps) == 1)
		})
	}
}

func TestPortalConsistencyTitleSearchAndNames(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	s := publishedServiceM7(t, f)
	ctx := fixture.Key("A", 1, 1, true)
	require.NoError(
		t,
		f.DB.Model(&types.Knowledge{}).
			Where("id=?", "public-doc").
			Updates(map[string]any{"title": "HA commands", "file_name": "HA.docx"}).
			Error,
	)
	result, e := s.SearchDocuments(ctx, nil, "HA", 20)
	require.NoError(t, e)
	require.NotEmpty(t, result.Documents)
	found := false
	for _, d := range result.Documents {
		require.NotEqual(t, "sensitive", d.DocumentID)
		if d.DocumentID == "public-doc" {
			found = true
			require.Equal(t, "HA.docx", d.FileName)
		}
	}
	require.True(t, found)
	require.NoError(t, f.DB.Model(&types.KnowledgeBase{}).Where("id=?", "public-kb").Update("name", "  NTOS  ").Error)
	targets, e := s.Resolve(ctx, []string{" ntos "})
	require.NoError(t, e)
	require.Len(t, targets, 1)
	require.NoError(t, f.DB.Model(&types.KnowledgeBase{}).Where("id=?", "public-two").Update("name", "ntos").Error)
	_, e = s.Resolve(ctx, []string{"NTOS"})
	require.Error(t, e)
	targets, e = s.Resolve(ctx, []string{"public-kb"})
	require.NoError(t, e)
	require.Len(t, targets, 1)
	require.NoError(t, f.DB.Exec("UPDATE tenant_portal_configs SET status='archived' WHERE tenant_id=99").Error)
	result, e = s.SearchDocuments(ctx, nil, "HA", 20)
	if e != nil {
		require.ErrorIs(t, e, access.ErrForbidden)
		require.Nil(t, result, "revoked scope must never expose a partial unauthorized response")
	} else {
		for _, d := range result.Documents {
			require.NotEqual(t, "public-doc", d.DocumentID)
		}
	}
	s.Resolver.Directory = emptyConsistencyDirectory{}
	s.Engine = func(context.Context, access.PortalKBDescriptor) (access.DocumentRetriever, error) {
		t.Fatal("empty scope must not call the retriever")
		return nil, nil
	}
	empty, e := s.Search(ctx, nil, "HA", "hybrid", 10)
	require.NoError(t, e)
	require.Equal(t, "no_scope", empty.Status)
	require.Zero(t, empty.Searched)
}

type emptyConsistencyDirectory struct{}

func (emptyConsistencyDirectory) ListPortalMCPCandidates(context.Context) ([]access.PortalKBDescriptor, error) {
	return nil, nil
}

func TestPortalConsistencyRequestEmbeddingSingleflight(t *testing.T) {
	ctx := withPortalQueryEmbeddings(context.Background())
	cache := ctx.Value(portalQueryEmbeddingKey{}).(*portalQueryEmbeddings)
	var calls atomic.Int32
	var group sync.WaitGroup
	for i := 0; i < 16; i++ {
		group.Go(func() {
			v, e := cache.embed(
				ctx,
				"identity+endpoint+dimension",
				func(context.Context) ([]float32, error) { calls.Add(1); return []float32{1}, nil },
			)
			require.NoError(t, e)
			v[0] = 42
		})
	}
	group.Wait()
	require.EqualValues(t, 1, calls.Load())
	v, e := cache.embed(ctx, "identity+endpoint+dimension", nil)
	require.NoError(t, e)
	require.Equal(t, float32(1), v[0])
	_, e = cache.embed(
		ctx,
		"different endpoint",
		func(context.Context) ([]float32, error) { calls.Add(1); return []float32{2}, nil },
	)
	require.NoError(t, e)
	require.EqualValues(t, 2, calls.Load())
}
