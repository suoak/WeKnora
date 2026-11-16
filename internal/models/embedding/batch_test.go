package embedding

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/panjf2000/ants/v2"
	"github.com/stretchr/testify/require"
)

type batchPoolModel struct {
	*fakeEmbedder
	run func(context.Context, []string) ([][]float32, error)
}

func (m *batchPoolModel) BatchEmbed(ctx context.Context, inputs []string) ([][]float32, error) {
	return m.run(ctx, inputs)
}

func TestBatchPoolOrderedResults(t *testing.T) {
	t.Setenv("BATCH_EMBED_SIZE", "2")
	pool, err := ants.NewPool(3)
	require.NoError(t, err)
	defer pool.Release()
	model := &batchPoolModel{run: func(_ context.Context, texts []string) ([][]float32, error) {
		out := make([][]float32, len(texts))
		for i, s := range texts {
			n, _ := strconv.Atoi(s)
			out[i] = []float32{float32(n)}
		}
		return out, nil
	}}
	out, err := NewBatchEmbedder(
		pool,
	).BatchEmbedWithPool(context.Background(), model, []string{"0", "1", "2", "3", "4"})
	require.NoError(t, err)
	require.Equal(t, [][]float32{{0}, {1}, {2}, {3}, {4}}, out)
}

func TestBatchPoolFailureCancelsAndJoinsWorkers(t *testing.T) {
	t.Setenv("BATCH_EMBED_SIZE", "1")
	pool, err := ants.NewPool(2)
	require.NoError(t, err)
	defer pool.Release()
	entered := make(chan struct{})
	var active atomic.Int32
	sentinel := errors.New("provider failure")
	model := &batchPoolModel{run: func(ctx context.Context, texts []string) ([][]float32, error) {
		active.Add(1)
		defer active.Add(-1)
		if texts[0] == "0" {
			select {
			case <-entered:
				return nil, sentinel
			case <-time.After(time.Second):
				return nil, errors.New("worker never started")
			}
		}
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	out, err := NewBatchEmbedder(pool).BatchEmbedWithPool(context.Background(), model, []string{"0", "1", "2", "3"})
	require.ErrorIs(t, err, sentinel)
	require.Nil(t, out)
	require.Zero(t, active.Load())
}

func TestBatchPoolCancelledAndMalformedProvider(t *testing.T) {
	t.Setenv("BATCH_EMBED_SIZE", "1")
	pool, err := ants.NewPool(1)
	require.NoError(t, err)
	defer pool.Release()
	var called atomic.Int32
	model := &batchPoolModel{
		run: func(context.Context, []string) ([][]float32, error) { called.Add(1); return nil, nil },
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = NewBatchEmbedder(pool).BatchEmbedWithPool(ctx, model, []string{"a"})
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, called.Load())
	_, err = NewBatchEmbedder(pool).BatchEmbedWithPool(context.Background(), model, []string{"a"})
	require.ErrorContains(t, err, "returned 0 embeddings")
}

func TestBatchPoolRejectsInvalidAndReleasedPool(t *testing.T) {
	pool, err := ants.NewPool(1)
	require.NoError(t, err)
	pool.Release()
	for _, size := range []string{"0", "-1", "garbage"} {
		t.Setenv("BATCH_EMBED_SIZE", size)
		_, err := NewBatchEmbedder(pool).BatchEmbedWithPool(context.Background(), nil, []string{"a"})
		require.Error(t, err)
	}
	t.Setenv("BATCH_EMBED_SIZE", "1")
	_, err = NewBatchEmbedder(pool).BatchEmbedWithPool(context.Background(), nil, []string{"a"})
	require.ErrorIs(t, err, ants.ErrPoolClosed)
	_, err = NewBatchEmbedder(nil).BatchEmbedWithPool(context.Background(), nil, []string{"a"})
	require.Error(t, err)
}
