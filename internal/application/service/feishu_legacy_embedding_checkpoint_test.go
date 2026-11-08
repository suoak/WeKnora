package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type checkpointMemoryStore struct {
	cache   map[string][]float32
	saves   int
	revoked bool
}

func (s *checkpointMemoryStore) StartEmbeddingCheckpoint(
	context.Context, *types.DataSource, *types.FeishuLegacyAttempt, types.FeishuLegacyEmbeddingCheckpoint,
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
	_ context.Context, _ *types.DataSource, _ *types.FeishuLegacyAttempt, inputs []string, vectors [][]float32,
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

func (s *checkpointMemoryStore) Renew(context.Context, *types.DataSource, *types.FeishuLegacyAttempt) error {
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

func (m *checkpointTestEmbedder) BatchEmbed(_ context.Context, texts []string) ([][]float32, error) {
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
	require.Equal(t, [][]string{{"second"}}, second.calls, "completed and duplicate inputs must not be billed again")
	store.revoked = true
	_, err = embedLegacyCheckpointed(context.Background(), store, &types.DataSource{},
		&types.FeishuLegacyAttempt{}, second, header, texts)
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
	require.Len(t, second.calls, 1, "revocation cannot be bypassed by the cache")
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
