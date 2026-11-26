package feishusynccontrol

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestAutomaticServiceIncidentRetainsIndependentStops(t *testing.T) {
	for _, fault := range []string{
		"none",
		"unrelated-old-history",
		"second-emergency",
		"later-identity-stop",
		"missing-control-coverage",
		"context-canceled",
	} {
		t.Run(fault, func(t *testing.T) {
			ctx := context.Background()
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { require.NoError(t, client.Close()) })
			guard, err := feishusafety.New(client, feishusafety.Config{
				Namespace: "incident-test", EndpointHost: "gateway.invalid", InFlight: 1,
				RPM: 30, RPS: 1, TPM: 30000, WaitBudgetDuration: time.Second, TokenBoundVerified: true,
			})
			require.NoError(t, err)
			prefix := strings.TrimSuffix(guard.PermitKey(), "permit")
			s := &automaticService{c: &Controller{Client: client, Guard: guard, prefix: prefix}}
			if fault != "missing-control-coverage" {
				require.NoError(
					t,
					client.XAdd(ctx, &redis.XAddArgs{
						Stream: prefix + "control_audit",
						ID:     "1-0",
						Values: map[string]any{"event": "bootstrap"},
					}).
						Err(),
				)
			}
			trip := "automatic_asynq_consumer_consumer_absent"
			if fault == "unrelated-old-history" {
				// Historical audit fixtures are not live trip/permit writes. A
				// previous incident had the same reason and a later operator stop.
				require.NoError(
					t,
					client.XAdd(ctx, &redis.XAddArgs{
						Stream: prefix + "audit",
						ID:     "2-0",
						Values: map[string]any{"event": "trip", "reason": trip},
					}).
						Err(),
				)
				require.NoError(
					t,
					client.XAdd(ctx, &redis.XAddArgs{
						Stream: prefix + "control_audit",
						ID:     "3-0",
						Values: map[string]any{"event": "feishu_safety_stop", "reason": "operator_emergency_stop"},
					}).
						Err(),
				)
				for i := 0; i < 1001; i++ {
					require.NoError(
						t,
						client.XAdd(ctx, &redis.XAddArgs{
							Stream: prefix + "control_audit",
							Values: map[string]any{"event": "historical_normal_source_registered"},
						}).
							Err(),
					)
				}
				// Keep the complete fixture history before the current incident's
				// common millisecond boundary, independent of stream sequence IDs.
				time.Sleep(2 * time.Millisecond)
			}
			require.NoError(t, guard.Trip(ctx, trip))
			switch fault {
			case "second-emergency":
				require.NoError(t, guard.Trip(ctx, "operator_emergency_stop"))
			case "later-identity-stop":
				require.NoError(
					t,
					client.XAdd(ctx, &redis.XAddArgs{
						Stream: prefix + "control_audit",
						Values: map[string]any{
							"event":  "feishu_safety_stop",
							"reason": "system_worker_identity_conflict",
						},
					}).
						Err(),
				)
			case "context-canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			evidence, guardID, controlID, originalID, err := s.incidentEvidence(ctx, trip)
			if fault == "none" || fault == "unrelated-old-history" {
				require.NoError(t, err)
				require.NotEmpty(t, evidence)
				require.Equal(t, guardID, originalID)
				if fault == "none" {
					require.Equal(t, "1-0", controlID)
				} else {
					require.NotEqual(t, "2-0", originalID, "bind the current incident, not the old matching reason")
					require.NotContains(t, evidence, "operator_emergency_stop")
					require.Greater(t, client.XLen(ctx, prefix+"control_audit").Val(), int64(1000))
				}
			} else {
				require.Error(t, err)
			}
			require.Equal(
				t,
				trip,
				client.Get(context.Background(), prefix+"trip").Val(),
				"read-only evidence gathering preserves the original trip",
			)
			require.Zero(t, client.Exists(context.Background(), prefix+"system_service").Val())
			require.Zero(t, client.Exists(context.Background(), guard.PermitKey()).Val())
		})
	}
}
