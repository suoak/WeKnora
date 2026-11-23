package feishusynccontrol

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestControllerMetricsSnapshotRefreshRevalidated(t *testing.T) {
	for _, kind := range []string{"healthy", "unhealthy", "missing", "stale", "unbounded_ttl"} {
		t.Run(kind, func(t *testing.T) {
			f := newControlFixture(t)
			a, _ := f.admit(t)
			before := f.stops
			a.lastAlertAck = time.Time{}
			a.Audit = func(event Event) error {
				if event.Kind != "feishu_safety_heartbeat" {
					return nil
				}
				switch kind {
				case "missing":
					return f.client.Del(context.Background(), a.MetricsKey()).Err()
				case "unbounded_ttl":
					return f.client.Persist(context.Background(), a.MetricsKey()).Err()
				default:
					f.metrics(t, a, func(m *Metrics) {
						m.OnlineP95Ratio = 1.1
						if kind == "unhealthy" {
							yes := true
							m.QuotaExceeded = &yes
						}
						if kind == "stale" {
							m.ObservedUnixMS -= 3000
						}
					})
				}
				return nil
			}
			err := a.Step(context.Background())
			if kind == "healthy" {
				require.NoError(t, err)
				require.NoError(t, f.guard.Check(context.Background()))
				require.Equal(t, before, f.stops, "a fresh healthy replacement must not fence workers")
			} else {
				require.Error(t, err)
				require.Error(t, f.guard.Check(context.Background()))
				require.Greater(t, f.stops, before)
				require.Zero(t, f.client.Exists(context.Background(), f.guard.PermitKey()).Val())
			}
		})
	}
}

type metricsRefreshHook struct {
	mutate       func() error
	calls        int
	metricsKey   string
	checkRevoked func()
}

func (*metricsRefreshHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (*metricsRefreshHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func (h *metricsRefreshHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if h.calls > 0 && cmd.Name() == "get" && cmd.Args()[1] == h.metricsKey && h.checkRevoked != nil {
			h.checkRevoked()
		}
		if cmd.Name() == "evalsha" && len(cmd.Args()) > 1 && cmd.Args()[1] == permit.Hash() {
			h.calls++
			if err := h.mutate(); err != nil {
				return err
			}
		}
		return next(ctx, cmd)
	}
}

func TestControllerMetricsSnapshotRefreshBudgetCloses(t *testing.T) {
	f := newControlFixture(t)
	a, _ := f.admit(t)
	hook := &metricsRefreshHook{metricsKey: a.MetricsKey(), checkRevoked: func() {
		require.False(t, f.server.Exists(f.guard.PermitKey()),
			"replacement samples revoke the previous permit atomically")
	}}
	hook.mutate = func() error {
		f.metrics(t, a, func(m *Metrics) {
			m.OnlineP95Ratio = 1.0
			if hook.calls%2 == 1 {
				m.OnlineP95Ratio = 1.1
			}
		})
		return nil
	}
	f.client.AddHook(hook)
	err := a.Step(context.Background())
	require.Error(t, err)
	require.Equal(t, 2, hook.calls, "continuous refresh cannot cause an unbounded permit loop")
	require.Error(t, f.guard.Check(context.Background()))
	require.Zero(t, f.client.Exists(context.Background(), f.guard.PermitKey()).Val())
}

func TestControllerMetricsSnapshotUnknownFailureNotRetried(t *testing.T) {
	f := newControlFixture(t)
	a, _ := f.admit(t)
	hook := &metricsRefreshHook{mutate: func() error { return errors.New("metrics backend unavailable") }}
	f.client.AddHook(hook)
	require.Error(t, a.Step(context.Background()))
	require.Equal(t, 1, hook.calls)
	require.Error(t, f.guard.Check(context.Background()))
}
