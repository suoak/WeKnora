package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/common/wikilock"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestWikiWorkerLeaseYieldsBusyKBWithoutBlockingOtherKB(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, wikilock.With(context.Background(), client, "busy", func(context.Context) error {
		start := time.Now()
		err := withWorkerWikiGenerationLease(context.Background(), client, "busy", func(context.Context) error {
			t.Fatal("contender entered protected work")
			return nil
		})
		require.ErrorIs(t, err, ErrWikiIngestConcurrent)
		require.Less(t, time.Since(start), time.Second)
		require.True(t, server.Exists("wiki:proof:generation:busy"), "contender must not release owner's lease")
		return withWorkerWikiGenerationLease(context.Background(), client, "other", func(ctx context.Context) error {
			// Acquisition budget must not cancel an acquired task or its model calls.
			time.Sleep(200 * time.Millisecond)
			require.NoError(t, ctx.Err())
			return nil
		})
	}))
	require.False(t, server.Exists("wiki:proof:generation:busy"))
	require.False(t, server.Exists("wiki:proof:generation:other"))
}

func TestWikiWorkerLeasePreservesParentCancellationAndCallbackError(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	err := withWorkerWikiGenerationLease(ctx, client, "cancel", func(locked context.Context) error {
		cancel()
		<-locked.Done()
		return locked.Err()
	})
	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, ErrWikiIngestConcurrent)
	want := errors.New("source validation failed")
	err = withWorkerWikiGenerationLease(context.Background(), client, "failure", func(context.Context) error {
		return want
	})
	require.ErrorIs(t, err, want)
	require.False(t, server.Exists("wiki:proof:generation:failure"))
}
