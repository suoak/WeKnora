package feishusynccontrol

import (
	"context"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type automaticMigrationRace struct {
	once atomic.Bool
	fire func() error
}

func (h *automaticMigrationRace) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *automaticMigrationRace) ProcessPipelineHook(
	next redis.ProcessPipelineHook,
) redis.ProcessPipelineHook {
	return next
}

func (h *automaticMigrationRace) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "evalsha" && len(cmd.Args()) > 1 &&
			cmd.Args()[1] == migrateSystemService.Hash() &&
			h.once.CompareAndSwap(false, true) {
			if err := h.fire(); err != nil {
				return err
			}
		}
		return next(ctx, cmd)
	}
}

func TestAutomaticServiceNativeSQLHistoricalIncidentAndCASRaces(t *testing.T) {
	for _, fault := range []string{"none", "independent-emergency", "trim-coverage"} {
		t.Run(fault, func(t *testing.T) {
			f := newAutomaticAuthorityFixture(t)
			health, r, opt := nativeAutomaticService(t)
			startNativeAutomaticConsumer(t, opt)
			c := health.c
			m, err := NewAutomaticServiceManager(
				c,
				f.db,
				repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
			)
			require.NoError(t, err)
			ctx := context.Background()
			// Synthetic historical audit rows, not a live authorization or stop
			// rewrite. The current trip below is created by the real Guard API.
			reason := "automatic_asynq_consumer_consumer_absent"
			require.NoError(
				t,
				r.XAdd(ctx, &redis.XAddArgs{
					Stream: c.prefix + "audit",
					ID:     "1-0",
					Values: map[string]any{"event": "trip", "reason": reason},
				}).
					Err(),
			)
			for i := 0; i < 1001; i++ {
				require.NoError(
					t,
					r.XAdd(ctx, &redis.XAddArgs{
						Stream: c.prefix + "control_audit",
						ID:     strconv.Itoa(i+2) + "-0",
						Values: map[string]any{"event": "historical_record"},
					}).
						Err(),
				)
			}
			writer := redis.NewClient(r.Options())
			t.Cleanup(func() { require.NoError(t, writer.Close()) })
			legacy, err := feishusafety.New(writer, feishusafety.Config{
				Namespace: strings.TrimSuffix(
					strings.TrimPrefix(c.prefix, "feishu:safety:{"),
					"}:",
				),
				EndpointHost: "gateway.invalid", InFlight: 1, RPM: 30, RPS: 1, TPM: 30000,
				WaitBudgetDuration: time.Second, TokenBoundVerified: true,
			})
			require.NoError(t, err)
			require.Equal(t, c.Guard.Fingerprint(), legacy.Fingerprint())
			require.NoError(t, legacy.Trip(ctx, reason))
			require.NoError(
				t,
				writer.XAdd(ctx, &redis.XAddArgs{
					Stream: c.prefix + "control_audit",
					Values: map[string]any{"event": "feishu_safety_stop", "reason": reason},
				}).
					Err(),
			)
			nativeAutomaticStable(t, m.system)
			var hook *automaticMigrationRace
			if fault != "none" {
				hook = &automaticMigrationRace{fire: func() error {
					if fault == "independent-emergency" {
						return legacy.Trip(ctx, "operator_emergency_stop")
					}
					return writer.XDel(ctx, c.prefix+"control_audit", "1002-0").Err()
				}}
				r.AddHook(hook)
			}
			err = c.Step(ctx)
			if fault != "none" {
				require.Error(t, err)
				if fault == "trim-coverage" {
					require.ErrorContains(t, err, "SYSTEM_INCIDENT_COVERAGE_CHANGED")
				} else {
					require.ErrorContains(t, err, "SYSTEM_INCIDENT_EVIDENCE_CHANGED")
				}
				require.True(
					t,
					hook.once.Load(),
					"inject between evidence collection and the actual migration CAS",
				)
				require.Equal(t, reason, r.Get(ctx, c.prefix+"trip").Val())
				require.Zero(t, r.Exists(ctx, c.prefix+"system_service").Val())
				require.Zero(t, r.Exists(ctx, c.Guard.PermitKey()).Val())
				receipt, err := readSystemReceipt(ctx, f.db)
				require.NoError(t, err)
				require.NotNil(t, receipt)
				require.Equal(t, "prepared", receipt.Phase)
				return
			}
			require.NoError(t, err)
			state, _, err := m.system.current(ctx)
			require.NoError(t, err)
			require.Equal(t, "canary", state.Phase)
			require.Empty(t, state.CanarySourceID)
			require.Zero(t, r.Exists(ctx, c.prefix+"trip").Val())
			receipt, err := readSystemReceipt(ctx, f.db)
			require.NoError(t, err)
			require.Equal(t, reason, receipt.OriginalTrip)
			require.NotEqual(t, "1-0", receipt.OriginalTripAuditID)
			require.NotEmpty(t, receipt.OriginalTripAuditID)
			require.NotContains(t, receipt.EvidenceJSON, "historical_record")
			require.Greater(
				t,
				r.XLen(ctx, c.prefix+"control_audit").Val(),
				int64(1000),
				"migration preserves older incident history",
			)
		})
	}
}
