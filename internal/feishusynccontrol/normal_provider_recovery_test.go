package feishusynccontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func providerStopFixture(t *testing.T) *normalFixture {
	f := newNormalFixture(t)
	f.activateNormal(t)
	f.c.Config.DedicatedSyncLane = true
	ctx := context.Background()
	require.NoError(t, f.c.Step(ctx))
	digest := func(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
	evidence, err := json.Marshal(map[string]any{
		"Family": "embedding", "Operation": "embedding", "Status": 403,
		"SourceHash": digest("other-source"), "EndpointHash": digest("gateway.invalid"),
	})
	require.NoError(t, err)
	require.NoError(t, f.r.Set(ctx, f.c.prefix+"trip", "embedding_http_403", 0).Err())
	require.NoError(t, f.r.Del(ctx, f.c.Guard.PermitKey()).Err())
	require.NoError(t, f.r.XAdd(ctx, &redis.XAddArgs{
		Stream: f.c.prefix + "audit",
		Values: map[string]any{"event": "trip", "reason": "embedding_http_403", "http_evidence": string(evidence)},
	}).Err())
	return f
}

// This regression uses only pre-existing public APIs, so it must also fail
// against the deployed baseline rather than merely fail to compile there.
func TestNormalServiceRecoversEvidencedProviderStopAfterFullHealth(t *testing.T) {
	f := providerStopFixture(t)
	ctx := context.Background()
	incident := f.r.Get(ctx, f.c.prefix+"recovery").Val()
	grant := *f.saved
	snapshot := func() map[string]any {
		out := map[string]any{}
		for _, q := range []string{"sync", "approved_sync"} {
			ids, err := f.r.LRange(ctx, "asynq:{"+q+"}:pending", 0, -1).Result()
			require.NoError(t, err)
			out[q] = ids
			for _, id := range ids {
				fields, err := f.r.HGetAll(ctx, "asynq:{"+q+"}:t:"+id).Result()
				require.NoError(t, err)
				out[q+":"+id] = fields
			}
		}
		return out
	}
	before := snapshot()
	require.NoError(t, f.c.Step(ctx))
	require.Zero(t, f.r.Exists(ctx, f.c.prefix+"trip").Val())
	require.Equal(t, f.c.Guard.Fingerprint(), f.r.Get(ctx, f.c.Guard.PermitKey()).Val())
	require.Equal(t, grant, *f.saved)
	require.Equal(t, incident, f.r.Get(ctx, f.c.prefix+"recovery").Val())
	require.Equal(t, before, snapshot())
	events, err := f.r.XRange(ctx, f.c.prefix+"control_audit", "-", "+").Result()
	require.NoError(t, err)
	count := 0
	for _, event := range events {
		if event.Values["event"] == "normal_provider_stop_recovered" {
			count++
		}
	}
	require.Equal(t, 1, count)
	require.NoError(t, f.c.Step(ctx))
}

func TestNormalProviderRecoveryRetainsManualUnknownAndUnhealthyStops(t *testing.T) {
	for _, failure := range []string{
		"emergency_after_provider", "operator_pause", "revoked", "changed_record", "missing_evidence",
		"wrong_endpoint", "missing_consumer", "locally_closed", "not_dedicated", "audit_failure",
		"legacy_control_operator_stop", "legacy_control_unknown_stop", "truncated_control_history",
		"cross_stream_same_millisecond_stop",
	} {
		t.Run(failure, func(t *testing.T) {
			f := providerStopFixture(t)
			ctx := context.Background()
			switch failure {
			case "cross_stream_same_millisecond_stop":
				original, err := f.r.XRevRangeN(ctx, f.c.prefix+"audit", "+", "-", 1).Result()
				require.NoError(t, err)
				require.Len(t, original, 1)
				ms, _, ok := strings.Cut(original[0].ID, "-")
				require.True(t, ok)
				require.NoError(t, f.r.XAdd(ctx, &redis.XAddArgs{
					Stream: f.c.prefix + "audit", ID: ms + "-100000", Values: original[0].Values,
				}).Err())
				require.NoError(t, f.r.XAdd(ctx, &redis.XAddArgs{
					Stream: f.c.prefix + "control_audit", ID: ms + "-99999",
					Values: map[string]any{"event": "feishu_safety_stop", "reason": "operator_emergency_stop"},
				}).Err())
			case "legacy_control_operator_stop", "legacy_control_unknown_stop":
				reason := "operator_emergency_stop"
				if failure == "legacy_control_unknown_stop" {
					reason = "unconfirmed_worker_loss"
				}
				require.NoError(t, f.r.XAdd(ctx, &redis.XAddArgs{
					Stream: f.c.prefix + "control_audit",
					Values: map[string]any{"event": "feishu_safety_stop", "reason": reason},
				}).Err())
			case "truncated_control_history":
				require.NoError(t, f.r.Del(ctx, f.c.prefix+"control_audit").Err())
				require.NoError(t, f.r.XAdd(ctx, &redis.XAddArgs{
					Stream: f.c.prefix + "control_audit", Values: map[string]any{"event": "later_event"},
				}).Err())
			case "emergency_after_provider":
				require.NoError(t, f.r.XAdd(ctx, &redis.XAddArgs{
					Stream: f.c.prefix + "audit", Values: map[string]any{"event": "emergency_stop"},
				}).Err())
			case "operator_pause":
				require.NoError(t, f.r.Set(ctx, "asynq:{approved_sync}:paused", "operator_pause", 0).Err())
			case "revoked":
				require.NoError(t, f.m.DeactivateNormalService(ctx))
			case "changed_record":
				require.NoError(t, f.r.Set(ctx, f.c.prefix+"normal_service", "{}", 0).Err())
			case "missing_evidence":
				require.NoError(t, f.r.Del(ctx, f.c.prefix+"audit").Err())
			case "wrong_endpoint":
				badEvidence, err := json.Marshal(map[string]any{
					"Family": "embedding", "Operation": "embedding", "Status": 403,
					"SourceHash": "source", "EndpointHash": "other",
				})
				require.NoError(t, err)
				require.NoError(t, f.r.XAdd(ctx, &redis.XAddArgs{
					Stream: f.c.prefix + "audit",
					Values: map[string]any{
						"event": "trip", "reason": "embedding_http_403", "http_evidence": string(badEvidence),
					},
				}).Err())
			case "missing_consumer":
				f.c.Consumers = func(context.Context) ([]string, error) { return nil, nil }
			case "locally_closed":
				f.c.Guard.CloseLocal()
			case "not_dedicated":
				f.c.Config.DedicatedSyncLane = false
			case "audit_failure":
				require.NoError(t, f.r.Del(ctx, f.c.prefix+"control_audit").Err())
				require.NoError(t, f.r.Set(ctx, f.c.prefix+"control_audit", "invalid-type", 0).Err())
			}
			require.Error(t, f.c.Step(ctx))
			require.NotZero(t, f.r.Exists(ctx, f.c.prefix+"trip").Val())
			require.Zero(t, f.r.Exists(ctx, f.c.Guard.PermitKey()).Val())
		})
	}
}
