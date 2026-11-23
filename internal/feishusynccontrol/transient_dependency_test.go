package feishusynccontrol

import (
	"context"
	"net"
	"testing"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type registryTimeoutHook struct{ fail bool }

func (h *registryTimeoutHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *registryTimeoutHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func (h *registryTimeoutHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if h.fail {
			h.fail = false
			return &net.OpError{Op: "read", Net: "tcp", Err: timeoutError{}}
		}
		return next(ctx, cmd)
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "injected dependency timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestNormalDedicatedRegistryTimeoutRecoversWithoutCancelingJobs(t *testing.T) {
	f := newNormalFixture(t)
	f.activateNormal(t)
	f.c.Config.DedicatedSyncLane = true
	ctx := t.Context()
	require.NoError(t, f.c.Step(ctx))
	source := f.addSource()
	work, _, err := BindApprovedSource(ctx, source.SourceID, source.ConfigSHA256)
	require.NoError(t, err)
	work = feishusafety.WithScope(work, source.SourceID)
	require.NoError(t, f.c.Guard.Check(work))
	stops := 0
	f.c.Stop = func(context.Context) error { stops++; return nil }
	hook := &registryTimeoutHook{fail: true}
	f.r.AddHook(hook)
	require.ErrorIs(t, f.c.Step(ctx), ErrNotReady)
	require.Zero(t, f.r.Exists(ctx, f.c.Guard.PermitKey()).Val())
	require.Error(t, f.c.Guard.Check(work), "outbound stays closed during the outage")
	require.Zero(t, f.r.Exists(ctx, f.c.prefix+"trip").Val(), "a transient outage must not become a permanent stop")
	require.Zero(t, stops, "a missed heartbeat must not cancel all running jobs")
	require.NoError(t, f.c.Step(ctx), "current consumer and authorization are checked again after recovery")
	require.NoError(t, f.c.Guard.Check(work))
	_, err = f.r.Get(ctx, "asynq:{approved_sync}:paused").Result()
	require.ErrorIs(t, err, redis.Nil)
}

func TestNormalDedicatedTransientFailurePreservesExistingEmergencyStop(t *testing.T) {
	f := newNormalFixture(t)
	f.activateNormal(t)
	f.c.Config.DedicatedSyncLane = true
	ctx := t.Context()
	require.ErrorIs(t, f.c.EmergencyStop(ctx), ErrNotReady)
	require.ErrorIs(t, f.c.freeze(ctx, "registry_redis_timeout", true), ErrNotReady)
	require.Equal(t, "operator_emergency_stop", f.r.Get(ctx, f.c.prefix+"trip").Val())
	require.Error(t, f.c.Step(ctx))
	require.Error(t, f.c.Guard.Check(ctx))
}

func TestNormalDedicatedDependencyTimeoutStagesRecover(t *testing.T) {
	for _, stage := range []string{
		"normal_service_authorization", "automatic_audit", "automatic_membership_probe",
		"automatic_redis_ping", "automatic_asynq_inspection", "automatic_permit", "normal_incident_health",
	} {
		t.Run(stage, func(t *testing.T) {
			f := newNormalFixture(t)
			f.activateNormal(t)
			f.c.Config.DedicatedSyncLane = true
			ctx := t.Context()
			require.NoError(t, f.c.Step(ctx))
			require.ErrorIs(t, f.c.freezeDependency(ctx, stage, context.DeadlineExceeded, "unavailable"), ErrNotReady)
			require.Zero(t, f.r.Exists(ctx, f.c.prefix+"trip").Val())
			require.Zero(t, f.r.Exists(ctx, f.c.Guard.PermitKey()).Val())
			require.NoError(t, f.c.Step(ctx))
			require.Positive(t, f.r.PTTL(ctx, f.c.Guard.PermitKey()).Val())
		})
	}
}

func TestTransientDependencySuspensionRequiresDedicatedSignedNormalService(t *testing.T) {
	for _, mode := range []string{"shared", "unsigned", "revoked", "scope", "consumer_loss", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			f := newNormalFixture(t)
			if mode != "unsigned" {
				f.activateNormal(t)
			}
			f.c.Config.DedicatedSyncLane = mode != "shared"
			if mode == "revoked" {
				require.NoError(t, f.m.DeactivateNormalService(t.Context()))
			}
			reason := "registry_redis_timeout"
			switch mode {
			case "scope":
				reason = "scope_none"
			case "consumer_loss":
				reason = "automatic_permit_consumer_lost"
			case "unknown":
				reason = "registry_redis_or_probe_error"
			}
			require.ErrorIs(t, f.c.freeze(t.Context(), reason, true), ErrNotReady)
			expected := reason
			if mode == "revoked" {
				expected = "normal_service_revoked"
			}
			require.Equal(t, expected, f.r.Get(t.Context(), f.c.prefix+"trip").Val())
		})
	}
}
