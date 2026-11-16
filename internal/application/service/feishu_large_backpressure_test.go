package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type largeBudgetEmbedder struct {
	embedding.Embedder
	calls      int
	allowFirst bool
}

func (m *largeBudgetEmbedder) BatchEmbed(_ context.Context, texts []string) ([][]float32, error) {
	m.calls++
	if !m.allowFirst || m.calls > 1 {
		return nil, feishusafety.ErrBudgetBusy
	}
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = []float32{1, 0, 0}
	}
	return out, nil
}

func TestLegacyLargeCheckpointBackpressureYieldsOnlyWithNewPersistedResponses(t *testing.T) {
	texts := make([]string, 40)
	header := types.FeishuLegacyEmbeddingCheckpoint{Version: 2}
	for i := range texts {
		texts[i] = fmt.Sprintf("input-%d", i)
		header.Inputs = append(header.Inputs, types.FeishuLegacyEmbeddingInput{
			ChunkID: fmt.Sprintf("c%d", i), SHA256: repository.LegacyEmbeddingInputHash(texts[i]),
		})
	}
	store := &checkpointMemoryStore{cache: map[string][]float32{}}
	model := &largeBudgetEmbedder{allowFirst: true}
	_, err := embedLegacyCheckpointed(
		t.Context(),
		store,
		&types.DataSource{},
		&types.FeishuLegacyAttempt{},
		model,
		header,
		texts,
	)
	var progress *repository.FeishuEmbeddingProgress
	require.ErrorAs(t, err, &progress)
	require.Equal(t, 20, progress.Completed)
	require.Equal(t, 40, progress.Total)
	require.Len(t, store.cache, 20)
	busy := &largeBudgetEmbedder{}
	_, err = embedLegacyCheckpointed(
		t.Context(),
		store,
		&types.DataSource{},
		&types.FeishuLegacyAttempt{},
		busy,
		header,
		texts,
	)
	require.ErrorIs(t, err, feishusafety.ErrBudgetBusy)
	require.False(
		t,
		errors.As(err, &progress),
		"copying an old cache must not refresh the failed-attempt budget",
	)
}
