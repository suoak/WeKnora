package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type bulkReplayStore struct {
	*checkpointMemoryStore
	imports     int
	largest     int
	renews      int
	revokeAfter int
}

func (s *bulkReplayStore) SaveReusedEmbeddingCheckpoint(ctx context.Context, ds *types.DataSource,
	owner *types.FeishuLegacyAttempt, texts []string, values [][]float32,
) error {
	s.imports++
	s.largest = max(s.largest, len(texts))
	if s.revokeAfter > 0 && s.imports >= s.revokeAfter {
		s.revoked = true
	}
	return s.SaveEmbeddingCheckpoint(ctx, ds, owner, texts, values)
}

func (s *bulkReplayStore) Renew(ctx context.Context, ds *types.DataSource, owner *types.FeishuLegacyAttempt) error {
	s.renews++
	return s.checkpointMemoryStore.Renew(ctx, ds, owner)
}

func replayFixture(count int) (*bulkReplayStore, types.FeishuLegacyEmbeddingCheckpoint, []string) {
	store := &bulkReplayStore{checkpointMemoryStore: &checkpointMemoryStore{cache: map[string][]float32{}}}
	header := types.FeishuLegacyEmbeddingCheckpoint{Version: 2}
	texts := make([]string, count)
	for i := range texts {
		texts[i] = fmt.Sprintf("replay-%d", i)
		hash := repository.LegacyEmbeddingInputHash(texts[i])
		store.cache[hash] = []float32{1, 0, 0}
		header.Inputs = append(
			header.Inputs,
			types.FeishuLegacyEmbeddingInput{ChunkID: fmt.Sprintf("c-%d", i), SHA256: hash},
		)
	}
	return store, header, texts
}

func TestLegacyLargeCheckpointReplayUsesBoundedGroupsWithoutBilling(t *testing.T) {
	store, header, texts := replayFixture(10001)
	model := &checkpointTestEmbedder{}
	vectors, err := embedLegacyCheckpointed(
		t.Context(),
		store,
		&types.DataSource{},
		&types.FeishuLegacyAttempt{},
		model,
		header,
		texts,
	)
	require.NoError(t, err)
	require.Len(t, vectors, len(texts))
	require.Empty(t, model.calls)
	require.Equal(t, 40, store.imports)
	require.Equal(t, 256, store.largest)
	require.LessOrEqual(t, store.renews, 42, "replayed groups must not renew once per twenty cached inputs")
}

func TestLegacyLargeCheckpointReplayHonorsRevocation(t *testing.T) {
	store, header, texts := replayFixture(10001)
	store.revokeAfter = 2
	model := &checkpointTestEmbedder{}
	vectors, err := embedLegacyCheckpointed(
		t.Context(),
		store,
		&types.DataSource{},
		&types.FeishuLegacyAttempt{},
		model,
		header,
		texts,
	)
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
	require.Nil(t, vectors)
	require.Equal(t, 2, store.imports)
	require.Empty(t, model.calls)
}

func TestLegacyLargeCheckpointReplayDoesNotCreditBackpressure(t *testing.T) {
	store, header, texts := replayFixture(300)
	missing := "missing input"
	texts = append(texts, missing)
	header.Inputs = append(
		header.Inputs,
		types.FeishuLegacyEmbeddingInput{ChunkID: "new", SHA256: repository.LegacyEmbeddingInputHash(missing)},
	)
	model := &largeBudgetEmbedder{}
	_, err := embedLegacyCheckpointed(
		t.Context(),
		store,
		&types.DataSource{},
		&types.FeishuLegacyAttempt{},
		model,
		header,
		texts,
	)
	require.ErrorIs(t, err, feishusafety.ErrBudgetBusy)
	var progress *repository.FeishuEmbeddingProgress
	require.False(t, errors.As(err, &progress), "replaying previous work must not refresh failure budgets")
	require.Equal(t, 2, store.imports)
}
