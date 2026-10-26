package wikilock

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestSharedWikiLeaseAndLostOwnership(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := With(ctx, client, "kb", func(locked context.Context) error {
		// A regeneration/finalize waiter must not overlap ingest.
		wait, stop := context.WithTimeout(context.Background(), 25*time.Millisecond)
		defer stop()
		require.Error(t, With(wait, client, "kb", func(context.Context) error {
			t.Fatal("generation lease overlapped")
			return nil
		}))
		cleanup, closeCleanup := Detached(locked, time.Second)
		defer closeCleanup()
		require.NoError(t, cleanup.Err())
		return nil
	})
	require.NoError(t, err)
	require.False(t, server.Exists("wiki:proof:generation:kb"))
}

func TestLiteWikiLeaseHonorsCanceledWaiter(t *testing.T) {
	err := With(context.Background(), nil, "lite-kb", func(context.Context) error {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := With(ctx, nil, "lite-kb", func(context.Context) error { return errors.New("must not run") })
		require.ErrorIs(t, err, context.Canceled)
		return nil
	})
	require.NoError(t, err)
}
