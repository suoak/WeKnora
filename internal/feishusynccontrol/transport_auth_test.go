package feishusynccontrol

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestControlTransportAuthenticatesEachPhysicalPool(t *testing.T) {
	server := miniredis.RunT(t)
	server.RequireAuth("test-control-password")
	base := redis.NewClient(&redis.Options{Addr: server.Addr(), Password: "test-control-password", DB: 3})
	t.Cleanup(func() { _ = base.Close() })
	client, transport := NewControlClient(base)
	t.Cleanup(func() { _ = client.Close() })
	step, cancel := context.WithTimeout(context.WithValue(t.Context(), controllerProbeKey{}, true), time.Second)
	defer cancel()
	transport.begin(step)
	// Inspector drops the caller context. Its first command initializes the
	// source pool while borrowing the controller's marked deadline.
	require.NoError(t, client.Set(context.Background(), "pool-auth-proof", "source", 0).Err())
	transport.finish()
	require.Equal(t, "source", client.Get(t.Context(), "pool-auth-proof").Val())
	require.NoError(t, client.Set(step, "probe-auth-proof", "probe", 0).Err())
	require.Equal(t, "probe", client.Get(t.Context(), "probe-auth-proof").Val())
	require.NoError(t, base.Ping(t.Context()).Err())
	stored, err := server.DB(3).Get("pool-auth-proof")
	require.NoError(t, err)
	require.Equal(t, "source", stored)
	require.False(t, server.DB(0).Exists("pool-auth-proof"), "SELECT must initialize the same connection as AUTH")
}
