package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/stretchr/testify/require"
)

type leaseTestEmbedder struct {
	embedding.Embedder
	calls    int
	callback func(int)
}

func (m *leaseTestEmbedder) BatchEmbed(_ context.Context, texts []string) ([][]float32, error) {
	m.calls++
	if m.callback != nil {
		m.callback(m.calls)
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{float32(i)}
	}
	return out, nil
}

func TestFeishuLegacyLeaseBoundedEmbedding(t *testing.T) {
	for _, lostAt := range []int{0, 1, 2, 3} {
		t.Run(string(rune('0'+lostAt)), func(t *testing.T) {
			raw := &leaseTestEmbedder{}
			renewals := 0
			lost := errors.New("lease/claim no longer owned")
			model := &legacyLeaseEmbedder{Embedder: raw, renew: func(context.Context) error {
				renewals++
				if lostAt > 0 && renewals == lostAt {
					return lost
				}
				return nil
			}}
			vectors, err := embedding.BatchEmbedBounded(context.Background(), model, make([]string, 41), 20)
			if lostAt == 0 {
				require.NoError(t, err)
				require.Len(t, vectors, 41)
				require.Equal(t, 3, raw.calls)
				require.Equal(t, 6, renewals)
			} else {
				require.ErrorIs(t, err, lost)
				require.Nil(t, vectors)
				require.Equal(t, lostAt/2, raw.calls)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw := &leaseTestEmbedder{}
	model := &legacyLeaseEmbedder{Embedder: raw, renew: func(context.Context) error {
		t.Fatal("renew after cancel")
		return nil
	}}
	_, err := model.BatchEmbed(ctx, []string{"text"})
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, raw.calls)
}
