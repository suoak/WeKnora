package service

import (
	"context"
	"math"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPortalCompatibleRanksRemoveArtificialSmallKBFirstPlace(t *testing.T) {
	engine := &portalV1Driver{}
	domain, ok := portalCompatibleRankDomain(engine, "embedding-a", types.KeywordsRetrieverType, nil)
	require.True(t, ok)
	hits := map[string]*PortalMCPHit{
		"z-relevant":  {RankScore: 1.0 / 61},
		"z-second":    {RankScore: 1.0 / 62},
		"a-unrelated": {RankScore: 1.0 / 61},
	}
	// Two KBs share the same actual lexical engine. The small KB's sole
	// result must not tie the best result from the shared scoring corpus.
	outcomes := []portalTargetSearchOutcome{
		{ranks: map[portalRankDomain]map[string]portalRankCandidate{domain: {
			"z-relevant": {key: "z-relevant", id: "10", score: 100, localRank: 1},
			"z-second":   {key: "z-second", id: "11", score: 95, localRank: 2},
		}}},
		{ranks: map[portalRankDomain]map[string]portalRankCandidate{domain: {
			"a-unrelated": {key: "a-unrelated", id: "1", score: 1, localRank: 1},
		}}},
	}
	require.NoError(t, portalApplyCompatibleRanks(hits, outcomes))
	require.InDelta(t, 1.0/61, hits["z-relevant"].RankScore, 1e-15)
	require.InDelta(t, 1.0/62, hits["z-second"].RankScore, 1e-15)
	require.InDelta(t, 1.0/63, hits["a-unrelated"].RankScore, 1e-15)
}

func TestPortalRankDomainKeepsDifferentStoresAndModelsIndependent(t *testing.T) {
	first, second := &portalV1Driver{}, &portalV1Driver{}
	lexicalA, ok := portalCompatibleRankDomain(first, "model-a", types.KeywordsRetrieverType, nil)
	require.True(t, ok)
	lexicalB, _ := portalCompatibleRankDomain(first, "model-b", types.KeywordsRetrieverType, nil)
	require.Equal(t, lexicalA, lexicalB, "lexical relevance does not depend on the embedding model")
	otherStore, _ := portalCompatibleRankDomain(second, "model-a", types.KeywordsRetrieverType, nil)
	// Domain maps use Go interface/pointer identity, not recursive equality of
	// the fields inside two independently constructed adapters.
	require.False(t, lexicalA == otherStore, "driver type alone does not identify a scoring corpus")
	vectorA, _ := portalCompatibleRankDomain(first, "model-a", types.VectorRetrieverType, []float32{0.1, 0.2})
	vectorB, _ := portalCompatibleRankDomain(first, "model-b", types.VectorRetrieverType, []float32{0.1, 0.2})
	require.NotEqual(t, vectorA, vectorB)
	changedQuery, _ := portalCompatibleRankDomain(first, "model-a", types.VectorRetrieverType, []float32{0.2, 0.1})
	require.NotEqual(t, vectorA, changedQuery, "a model change or fresh query vector cannot share cosine ranks")
	_, ok = portalCompatibleRankDomain(first, "", types.VectorRetrieverType, []float32{0.1, 0.2})
	require.False(t, ok, "an absent model binding cannot make cosine scores comparable")
	_, ok = portalCompatibleRankDomain(portalV1Driver{}, "model-a", types.KeywordsRetrieverType, nil)
	require.False(t, ok, "non-comparable custom adapters retain independent ranking")
	batched, ok := portalCompatibleRankDomain(
		&portalBatchEngine{engine: first}, "model-a", types.VectorRetrieverType, []float32{0.1, 0.2},
	)
	require.True(t, ok)
	require.Equal(t, vectorA, batched, "SQL batching must not change scoring identity")

	hits := map[string]*PortalMCPHit{"a": {RankScore: 1.0 / 61}, "b": {RankScore: 1.0 / 61}}
	outcomes := []portalTargetSearchOutcome{
		{ranks: map[portalRankDomain]map[string]portalRankCandidate{vectorA: {
			"a": {key: "a", id: "1", score: 0.01, localRank: 1},
		}}},
		{ranks: map[portalRankDomain]map[string]portalRankCandidate{vectorB: {
			"b": {key: "b", id: "2", score: 0.99, localRank: 1},
		}}},
	}
	require.NoError(t, portalApplyCompatibleRanks(hits, outcomes))
	require.Equal(t, hits["a"].RankScore, hits["b"].RankScore)
}

func TestPortalCompatibleRanksPreserveUncomparableLegAndNativeTieOrder(t *testing.T) {
	domain, _ := portalCompatibleRankDomain(&portalV1Driver{}, "", types.KeywordsRetrieverType, nil)
	hits := map[string]*PortalMCPHit{
		"a": {RankScore: 1.0/61 + 1.0/65},
		"b": {RankScore: 1.0 / 61},
	}
	outcomes := []portalTargetSearchOutcome{{ranks: map[portalRankDomain]map[string]portalRankCandidate{domain: {
		"a": {key: "a", id: "10", score: 2, localRank: 1},
		"b": {key: "b", id: "2", score: 2, localRank: 1},
	}}}}
	require.NoError(t, portalApplyCompatibleRanks(hits, outcomes))
	require.InDelta(t, 1.0/62+1.0/65, hits["a"].RankScore, 1e-15,
		"retain the independent contribution from a leg with no comparable model identity")
	require.InDelta(t, 1.0/61, hits["b"].RankScore, 1e-15,
		"equal PostgreSQL scores retain numeric embedding-ID order")
}

func TestPortalCompatibleRanksRejectInvalidCandidateEvidence(t *testing.T) {
	domain, _ := portalCompatibleRankDomain(&portalV1Driver{}, "", types.KeywordsRetrieverType, nil)
	for _, score := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		hits := map[string]*PortalMCPHit{"a": {RankScore: 1.0 / 61}}
		outcomes := []portalTargetSearchOutcome{{ranks: map[portalRankDomain]map[string]portalRankCandidate{domain: {
			"a": {key: "a", id: "1", score: score, localRank: 1},
		}}}}
		require.Error(t, portalApplyCompatibleRanks(hits, outcomes))
		require.Equal(t, 1.0/61, hits["a"].RankScore)
	}
	hits := map[string]*PortalMCPHit{"a": {RankScore: 1.0 / 61}}
	unknown := []portalTargetSearchOutcome{{ranks: map[portalRankDomain]map[string]portalRankCandidate{domain: {
		"not-an-authorized-candidate": {key: "not-an-authorized-candidate", score: 1, localRank: 1},
	}}}}
	require.Error(t, portalApplyCompatibleRanks(hits, unknown))
}

func TestPortalCompatibleRanksRetainFinalCredentialWithdrawal(t *testing.T) {
	f, coordinator, ctx, _ := globalPortalSearchFixture(t, 2)
	coordinator.globalRanks = true
	var chunks []types.Chunk
	require.NoError(t, f.DB.Where("chunk_type=? AND is_enabled=true", types.ChunkTypeText).Find(&chunks).Error)
	driver := &portalV1Driver{f: func(execution context.Context, params types.RetrieveParams) (
		[]*types.RetrieveResult, error,
	) {
		guarded, err := access.GuardPortalRetrieval(execution, types.PostgresRetrieverEngineType, params)
		if err != nil {
			return nil, err
		}
		require.Equal(t, 100, guarded.TopK)
		row := &types.RetrieveResult{RetrieverType: params.RetrieverType}
		for i, chunk := range chunks {
			for _, kb := range guarded.KnowledgeBaseIDs {
				if chunk.KnowledgeBaseID == kb {
					row.Results = append(row.Results, &types.IndexWithScore{
						ID: "1", KnowledgeID: chunk.KnowledgeID, KnowledgeBaseID: kb, ChunkID: chunk.ID,
						Content: chunk.Content, SourceType: types.ChunkSourceType, Score: float64(i + 1),
					})
				}
			}
		}
		return []*types.RetrieveResult{row}, nil
	}}
	coordinator.Engine = func(context.Context, access.PortalKBDescriptor) (access.DocumentRetriever, error) {
		return driver, nil
	}
	before, err := coordinator.Search(ctx, nil, "PUBLIC", "keyword", 30)
	require.NoError(t, err)
	require.NotEmpty(t, before.Results)
	coordinator.Consumers = []func(context.Context, []PortalMCPHit) error{
		func(context.Context, []PortalMCPHit) error {
			return f.DB.Exec("UPDATE tenant_api_keys SET revoked_at=CURRENT_TIMESTAMP WHERE id=1").Error
		},
	}
	_, err = coordinator.Search(ctx, nil, "PUBLIC", "keyword", 30)
	require.Error(t, err, "global ranking cannot turn a revoked credential into a serving grant")
}
