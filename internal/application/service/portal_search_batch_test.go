package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPortalBatchConsumesReadyMembersBeforeUnrelatedWork(t *testing.T) {
	_, s, ctx, count := globalPortalSearchFixture(t, 24)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	release, follower := make(chan struct{}), make(chan struct{})
	var releaseOnce, followerOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	shared := &portalV1Driver{f: func(execution context.Context, params types.RetrieveParams) ([]*types.RetrieveResult, error) {
		for _, id := range params.KnowledgeBaseIDs {
			if id == "parallel-00" {
				return nil, nil
			}
		}
		select {
		case <-release:
			return nil, nil
		case <-execution.Done():
			return nil, execution.Err()
		}
	}}
	s.Engine = func(context.Context, access.PortalKBDescriptor) (access.DocumentRetriever, error) { return shared, nil }
	original := s.SearchCapabilities
	var calls atomic.Int32
	s.SearchCapabilities = func(execution context.Context, d access.PortalKBDescriptor) ([]string, error) {
		if d.ID == "parallel-01" && calls.Add(1) == 2 {
			followerOnce.Do(func() { close(follower) })
		}
		if original != nil {
			return original(execution, d)
		}
		return d.SearchCapabilities(), nil
	}
	done := make(chan struct{})
	var result *PortalMCPSearchResult
	var searchErr error
	go func() {
		defer close(done)
		result, searchErr = s.Search(ctx, nil, "PUBLIC", "hybrid", 30)
	}()
	defer func() { unblock(); cancel(); <-done }()
	select {
	case <-follower:
	case <-ctx.Done():
		t.Fatal("a completed batch member waited behind unrelated blocked database work")
	}
	unblock()
	<-done
	require.NoError(t, searchErr)
	require.Equal(t, "complete", result.Status)
	require.Equal(t, count, result.Completed)
}

func TestPortalBatchPreservesPerKBRanksAndFailureIsolation(t *testing.T) {
	f, s, ctx, count := globalPortalSearchFixture(t, 6)
	var chunks []types.Chunk
	require.NoError(t, f.DB.Where("chunk_type=? AND is_enabled=true", types.ChunkTypeText).Find(&chunks).Error)
	var calls, grouped atomic.Int32
	var fail atomic.Bool
	backend := func(execution context.Context, params types.RetrieveParams) ([]*types.RetrieveResult, error) {
		calls.Add(1)
		require.Equal(t, 100, params.TopK)
		guarded, err := access.GuardPortalRetrieval(execution, types.PostgresRetrieverEngineType, params)
		require.NoError(t, err)
		if len(guarded.KnowledgeBaseIDs) > 1 {
			grouped.Add(1)
			require.True(t, access.PortalRetrievalGrouped(execution))
		}
		row := &types.RetrieveResult{
			RetrieverType: params.RetrieverType, RetrieverEngineType: types.PostgresRetrieverEngineType,
		}
		for _, kb := range guarded.KnowledgeBaseIDs {
			if fail.Load() && kb == "parallel-00" {
				return nil, errors.New("one unavailable index")
			}
			for _, chunk := range chunks {
				if chunk.KnowledgeBaseID == kb {
					row.Results = append(row.Results, &types.IndexWithScore{
						KnowledgeID: chunk.KnowledgeID, KnowledgeBaseID: kb, ChunkID: chunk.ID,
						Content: chunk.Content, SourceType: types.ChunkSourceType,
					})
				}
			}
		}
		return []*types.RetrieveResult{row}, nil
	}
	s.Engine = func(context.Context, access.PortalKBDescriptor) (access.DocumentRetriever, error) {
		return portalV1Driver{f: backend}, nil // non-comparable adapters retain the independent path
	}
	baseline, err := s.Search(ctx, nil, "PUBLIC", "hybrid", 30)
	require.NoError(t, err)
	require.Equal(t, count, baseline.Searched)
	calls.Store(0)
	shared := &portalV1Driver{f: backend}
	s.Engine = func(context.Context, access.PortalKBDescriptor) (access.DocumentRetriever, error) { return shared, nil }
	actual, err := s.Search(ctx, nil, "PUBLIC", "hybrid", 30)
	require.NoError(t, err)
	require.Equal(t, baseline, actual, "batching must preserve per-KB/per-mode RRF ranks and canonical output")
	require.Positive(t, grouped.Load())
	require.Less(t, int(calls.Load()), count*2)
	fail.Store(true)
	partial, err := s.Search(ctx, nil, "PUBLIC", "hybrid", 30)
	require.NoError(t, err)
	require.Equal(t, "partial", partial.Status)
	require.Equal(t, 1, partial.Failed)
	require.Equal(t, count-1, partial.Searched)
	require.NotEmpty(t, partial.Results)
}

func TestPortalBatchCancellationIsNotAuthorizationDenial(t *testing.T) {
	_, s, ctx, _ := globalPortalSearchFixture(t, 2)
	targets, err := s.Resolve(ctx, []string{"parallel-00", "parallel-01"})
	require.NoError(t, err)
	execution, cancel := context.WithCancel(ctx)
	defer cancel()
	batch := &portalBatchEngine{targets: targets, calls: make(map[string]*portalBatchReply)}
	batch.engine = &portalV1Driver{f: func(context.Context, types.RetrieveParams) ([]*types.RetrieveResult, error) {
		cancel()
		return nil, context.Canceled
	}}
	_, err = batch.Retrieve(execution, types.RetrieveParams{
		KnowledgeBaseIDs: []string{targets[0].Descriptor().ID}, RetrieverType: types.KeywordsRetrieverType, TopK: 100,
	})
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, portalBatchAuthorizationError(err))
	_, err = batch.Retrieve(ctx, types.RetrieveParams{KnowledgeBaseIDs: []string{"outside-issued-batch"}})
	require.ErrorIs(t, err, access.ErrForbidden)
}
