package embedding

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/stretchr/testify/require"
)

func TestPhysicalEmbeddingResponseCheckpointSurvivesLaterFailure(t *testing.T) {
	model, _, _, _, calls := reservationEmbedder(t, 2, time.Second)
	ctx := feishusafety.WithScope(context.Background(), "source")
	var saved []string
	ctx = WithBatchCheckpoint(ctx, func(_ context.Context, inputs []string, values [][]float32) error {
		require.Len(t, inputs, len(values))
		saved = append(saved, inputs...)
		return nil
	})
	// Each input fits; together they require two physical requests.
	inputs := []string{"1:" + strings.Repeat("a", 16000), "2:" + strings.Repeat("b", 16000)}
	_, err := model.BatchEmbed(ctx, inputs)
	require.Error(t, err)
	require.EqualValues(t, 2, calls.Load())
	require.Equal(t, inputs[:1], saved, "the successful first response must survive the second response failure")
}

func TestEmbeddingCheckpointFailureStopsWithoutProviderTrip(t *testing.T) {
	model, _, client, guard, calls := reservationEmbedder(t, 0, time.Second)
	want := errors.New("private checkpoint storage unavailable")
	ctx := WithBatchCheckpoint(feishusafety.WithScope(context.Background(), "source"),
		func(context.Context, []string, [][]float32) error { return want })
	_, err := model.BatchEmbed(ctx, []string{"1:body"})
	require.ErrorIs(t, err, want)
	require.EqualValues(t, 1, calls.Load())
	require.NoError(t, guard.Check(ctx))
	require.EqualValues(t, 0, client.HLen(ctx, "feishu:safety:{embedding-reservation}:inflight").Val())
}
