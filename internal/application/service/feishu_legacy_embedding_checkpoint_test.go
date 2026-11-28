package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type checkpointRenewBoundary struct {
	*checkpointMemoryStore
	err error
}

func (s *checkpointRenewBoundary) Renew(
	context.Context, *types.DataSource, *types.FeishuLegacyAttempt,
) error {
	if s.saves > 0 {
		return s.err
	}
	return nil
}

func TestLegacyEmbeddingCheckpointRenewalAfterDurableWork(t *testing.T) {
	for _, test := range []struct {
		name     string
		version  int
		cached   bool
		err      error
		progress bool
	}{
		{"v2-budget", 2, false, feishusafety.ErrBudgetBusy, true},
		{"v1-budget", 1, false, feishusafety.ErrBudgetBusy, false},
		{"cached-only", 2, true, feishusafety.ErrBudgetBusy, false},
		{"revoked", 2, false, types.ErrLegacySourceRestricted, false},
		{"canceled", 2, false, context.Canceled, false},
		{"provider-error", 2, false, errors.New("synthetic provider failure"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			memory := &checkpointMemoryStore{cache: map[string][]float32{}}
			store := &checkpointRenewBoundary{checkpointMemoryStore: memory, err: test.err}
			header := types.FeishuLegacyEmbeddingCheckpoint{Version: test.version}
			texts := make([]string, 41)
			for i := range texts {
				texts[i] = fmt.Sprintf("boundary-input-%d", i)
				header.Inputs = append(header.Inputs, types.FeishuLegacyEmbeddingInput{
					ChunkID: fmt.Sprintf("c%d", i), SHA256: repository.LegacyEmbeddingInputHash(texts[i]),
				})
				if test.cached && i < 20 {
					memory.cache[repository.LegacyEmbeddingInputHash(texts[i])] = []float32{1, 0, 0}
				}
			}
			model := &checkpointTestEmbedder{store: memory}
			owner := &types.FeishuLegacyAttempt{ID: "current-owner"}
			vectors, err := embedLegacyCheckpointed(context.Background(), store, &types.DataSource{},
				owner, model, header, texts)
			require.Nil(t, vectors)
			var progress *repository.FeishuEmbeddingProgress
			if test.progress {
				require.ErrorAs(t, err, &progress)
				require.Same(t, owner, progress.Owner)
				require.Equal(t, 20, progress.Completed)
				require.Equal(t, 41, progress.Total)
			} else {
				require.ErrorIs(t, err, test.err)
				require.False(t, errors.As(err, &progress))
			}
			require.Len(t, memory.cache, 20)
			if test.cached {
				require.Empty(t, model.calls, "replay alone cannot buy a progress window")
			} else {
				require.Len(t, model.calls, 1)
				// A fresh execution reuses the durably saved first batch.
				resume := &checkpointTestEmbedder{store: memory}
				all, err := embedLegacyCheckpointed(context.Background(), memory, &types.DataSource{},
					owner, resume, header, texts)
				require.NoError(t, err)
				require.Len(t, all, len(texts))
				count := 0
				for _, call := range resume.calls {
					count += len(call)
				}
				require.Equal(t, 21, count)
			}
		})
	}
}

type checkpointMemoryStore struct {
	cache   map[string][]float32
	saves   int
	revoked bool
}

func (s *checkpointMemoryStore) StartEmbeddingCheckpoint(
	context.Context,
	*types.DataSource,
	*types.FeishuLegacyAttempt,
	types.FeishuLegacyEmbeddingCheckpoint,
) error {
	return nil
}

func (s *checkpointMemoryStore) ReusableEmbeddingCheckpoint(
	context.Context, *types.DataSource, *types.FeishuLegacyAttempt,
) (map[string][]float32, error) {
	cloned := map[string][]float32{}
	for key, value := range s.cache {
		cloned[key] = value
	}
	return cloned, nil
}

func (s *checkpointMemoryStore) SaveEmbeddingCheckpoint(
	_ context.Context,
	_ *types.DataSource,
	_ *types.FeishuLegacyAttempt,
	inputs []string,
	vectors [][]float32,
) error {
	if s.revoked {
		return types.ErrLegacySourceRestricted
	}
	s.saves++
	for i, input := range inputs {
		s.cache[repository.LegacyEmbeddingInputHash(input)] = vectors[i]
	}
	return nil
}

func (s *checkpointMemoryStore) Renew(
	context.Context,
	*types.DataSource,
	*types.FeishuLegacyAttempt,
) error {
	if s.revoked {
		return types.ErrLegacySourceRestricted
	}
	return nil
}

type checkpointTestEmbedder struct {
	embedding.Embedder
	calls [][]string
	store *checkpointMemoryStore
	err   error
}

func (m *checkpointTestEmbedder) BatchEmbed(
	_ context.Context,
	texts []string,
) ([][]float32, error) {
	m.calls = append(m.calls, append([]string(nil), texts...))
	if m.err != nil {
		return nil, m.err
	}
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = []float32{1, 0, 0}
	}
	return out, nil
}

func TestLegacyEmbeddingCheckpointRestartReusesCompletedInputs(t *testing.T) {
	store := &checkpointMemoryStore{cache: map[string][]float32{
		repository.LegacyEmbeddingInputHash("first"): {1, 0, 0},
	}}
	texts := []string{"first", "second", "first"}
	header := types.FeishuLegacyEmbeddingCheckpoint{
		ModelID: "model", ModelStamp: strings.Repeat("1", 32), KBConfigSHA256: strings.Repeat("1", 64), Dimension: 3,
	}
	for i, text := range texts {
		header.Inputs = append(header.Inputs, types.FeishuLegacyEmbeddingInput{
			ChunkID: fmt.Sprintf("c%d", i), SHA256: repository.LegacyEmbeddingInputHash(text),
		})
	}
	first := &checkpointTestEmbedder{store: store}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := embedLegacyCheckpointed(ctx, store, &types.DataSource{},
		&types.FeishuLegacyAttempt{}, first, header, texts)
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, first.calls)
	second := &checkpointTestEmbedder{store: store}
	vectors, err := embedLegacyCheckpointed(context.Background(), store, &types.DataSource{},
		&types.FeishuLegacyAttempt{}, second, header, texts)
	require.NoError(t, err)
	require.Len(t, vectors, 3)
	require.Equal(
		t,
		[][]string{{"second"}},
		second.calls,
		"completed and duplicate inputs must not be billed again",
	)
	store.revoked = true
	_, err = embedLegacyCheckpointed(context.Background(), store, &types.DataSource{},
		&types.FeishuLegacyAttempt{}, second, header, texts)
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
	require.Len(t, second.calls, 1, "revocation cannot be bypassed by the cache")
}

func TestLegacyLargeEmbeddingYieldsAndResumesWithoutRepeatingModelWork(t *testing.T) {
	store := &checkpointMemoryStore{cache: map[string][]float32{}}
	header := types.FeishuLegacyEmbeddingCheckpoint{Version: 2}
	texts := make([]string, 10001)
	for i := range texts {
		texts[i] = fmt.Sprintf("input-%d", i)
		header.Inputs = append(header.Inputs, types.FeishuLegacyEmbeddingInput{
			ChunkID: fmt.Sprintf("c%d", i), SHA256: repository.LegacyEmbeddingInputHash(texts[i]),
		})
	}
	model := &checkpointTestEmbedder{store: store}
	var complete [][]float32
	for window := 0; window < 11; window++ {
		vectors, err := embedLegacyCheckpointed(context.Background(), store, &types.DataSource{},
			&types.FeishuLegacyAttempt{ID: "owner"}, model, header, texts)
		if window < 10 {
			var progress *repository.FeishuEmbeddingProgress
			require.ErrorAs(t, err, &progress)
			require.Equal(t, (window+1)*1000, progress.Completed)
			require.Equal(t, len(texts), progress.Total)
			require.Nil(t, vectors)
		} else {
			require.NoError(t, err)
			complete = vectors
		}
	}
	require.Len(t, complete, len(texts))
	count := 0
	for _, call := range model.calls {
		count += len(call)
	}
	require.Equal(t, len(texts), count, "resume must not repeat billed inputs")
	require.Len(t, store.cache, len(texts))
}

func TestLegacyLargeEmbeddingFinishesWhenTheRemainingRowsRepeatCachedInputs(t *testing.T) {
	store := &checkpointMemoryStore{cache: map[string][]float32{}}
	model := &checkpointTestEmbedder{store: store}
	header := types.FeishuLegacyEmbeddingCheckpoint{Version: 2}
	texts := make([]string, 10001)
	for i := range texts {
		texts[i] = fmt.Sprintf("input-%d", i%1000)
		header.Inputs = append(header.Inputs, types.FeishuLegacyEmbeddingInput{
			ChunkID: fmt.Sprintf("c%d", i), SHA256: repository.LegacyEmbeddingInputHash(texts[i]),
		})
	}
	vectors, err := embedLegacyCheckpointed(
		context.Background(),
		store,
		&types.DataSource{},
		&types.FeishuLegacyAttempt{},
		model,
		header,
		texts,
	)
	require.NoError(
		t,
		err,
		"a complete checkpoint must proceed to publication instead of yielding forever",
	)
	require.Len(t, vectors, len(texts))
	require.Len(t, store.cache, 1000)
	count := 0
	for _, call := range model.calls {
		count += len(call)
	}
	require.Equal(t, 1000, count)
}

func TestLegacyEmbeddingCheckpointRetriesAreBoundedAndPermissionAware(t *testing.T) {
	for _, test := range []struct {
		name  string
		err   error
		calls int
	}{
		{"budget", feishusafety.ErrBudgetBusy, 3},
		{"permission", types.ErrLegacySourceRestricted, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &checkpointMemoryStore{cache: map[string][]float32{}}
			model := &checkpointTestEmbedder{store: store, err: test.err}
			_, err := embedLegacyCheckpointed(context.Background(), store,
				&types.DataSource{}, &types.FeishuLegacyAttempt{}, model,
				types.FeishuLegacyEmbeddingCheckpoint{Inputs: []types.FeishuLegacyEmbeddingInput{{
					ChunkID: "c", SHA256: repository.LegacyEmbeddingInputHash("x"),
				}}}, []string{"x"})
			require.ErrorIs(t, err, test.err)
			require.Len(t, model.calls, test.calls)
			require.Empty(t, store.cache)
		})
	}
}
