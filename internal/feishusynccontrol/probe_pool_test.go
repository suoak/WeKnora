package feishusynccontrol

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestControllerProbeSurvivesConcurrentSourcePoolExhaustion(t *testing.T) {
	f := newControlFixture(t)
	client, transport := NewControlClient(f.client)
	t.Cleanup(func() { _ = client.Close() })
	c := f.automaticController(t, "app:1")
	c.Client, c.Transport = client, transport
	c.Consumers = func(ctx context.Context) ([]string, error) {
		if err := client.Ping(ctx).Err(); err != nil {
			return nil, err
		}
		return []string{"app:1"}, nil
	}
	require.NoError(t, c.Step(context.Background()))
	// Hold every source-admission connection. Controller attestations must not
	// queue behind these independently scoped requests or borrow their deadline.
	held := make(chan error, 2)
	sourceCtx, cancelSource := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelSource()
	for i := 0; i < 2; i++ {
		go func() { held <- client.BLPop(sourceCtx, time.Second, "source-pool-held").Err() }()
	}
	require.Eventually(t, func() bool {
		stats := client.PoolStats()
		return stats.TotalConns == 2 && stats.IdleConns == 0
	}, time.Second, time.Millisecond)
	defer func() {
		require.NoError(t, f.client.RPush(context.Background(), "source-pool-held", "a", "b").Err())
		require.NoError(t, <-held)
		require.NoError(t, <-held)
	}()
	step, cancelStep := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelStep()
	require.NoError(t, c.Step(step))
	require.NoError(t, f.guard.Check(context.Background()))
	require.Zero(t, f.client.Exists(context.Background(), c.prefix+"trip").Val())
	// Pool isolation does not change endpoint, scope, or the 750ms permit lease.
	require.Equal(t, client.Options().Addr, client.probe.Options().Addr)
	require.Equal(t, client.Options().DB, client.probe.Options().DB)
	require.Equal(t, 2, client.probe.Options().MaxActiveConns)
	require.LessOrEqual(t, f.client.PTTL(context.Background(), c.Guard.PermitKey()).Val(), 750*time.Millisecond)
}

func TestControllerProbePipelineKeepsCallerCancellationAndResults(t *testing.T) {
	f := newControlFixture(t)
	client, _ := NewControlClient(f.client)
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.WithValue(context.Background(), controllerProbeKey{}, true)
	var value *redis.StringCmd
	_, err := client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Set(ctx, "probe-pipeline", "value", 0)
		value = pipe.Get(ctx, "probe-pipeline")
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, "value", value.Val())
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	require.ErrorIs(t, client.Ping(canceled).Err(), context.Canceled)
}
