package feishusynccontrol

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type sourceRequestContextKey struct{}

func TestControlTransportPreservesSourceCallerCancellation(t *testing.T) {
	f := newRecoveryFixture(t)
	client, transport := NewControlClient(f.r)
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, f.r.Set(context.Background(), "source-read-fixture", "current", 0).Err())
	t.Run("live_request_with_expired_step", func(t *testing.T) {
		step, cancelStep := context.WithCancel(context.Background())
		cancelStep()
		transport.begin(step)
		defer transport.finish()
		request, cancelRequest := context.WithCancel(context.Background())
		defer cancelRequest()
		value, err := client.Get(request, "source-read-fixture").Result()
		require.NoError(t, err)
		require.Equal(t, "current", value)
	})
	t.Run("scoped_caller_with_expired_step", func(t *testing.T) {
		step, cancelStep := context.WithCancel(context.Background())
		cancelStep()
		transport.begin(step)
		defer transport.finish()
		scoped := context.WithValue(context.Background(), sourceRequestContextKey{}, "source")
		value, err := client.Get(scoped, "source-read-fixture").Result()
		require.NoError(t, err)
		require.Equal(t, "current", value)
	})
	t.Run("canceled_request_with_live_step", func(t *testing.T) {
		step, cancelStep := context.WithTimeout(context.Background(), time.Second)
		defer cancelStep()
		transport.begin(step)
		defer transport.finish()
		request, cancelRequest := context.WithCancel(context.Background())
		cancelRequest()
		require.ErrorIs(t, client.Get(request, "source-read-fixture").Err(), context.Canceled)
	})
}
