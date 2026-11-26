package feishusynccontrol

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// This is real Controller + Asynq + Redis + migrated PostgreSQL, not a full
// APP or a claim that a remote source/document publication was exercised.
func TestAutomaticServiceNativeSQLBootstrapClaimAndMissingPublication(t *testing.T) {
	f := newAutomaticAuthorityFixture(t)
	health, r, opt := nativeAutomaticService(t)
	startNativeAutomaticConsumer(t, opt)
	c := health.c
	c.Stop = func(ctx context.Context) error {
		_, err := repository.FenceClaimedLegacySyncExecutions(ctx, f.db)
		return err
	}
	m, err := NewAutomaticServiceManager(
		c,
		f.db,
		repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
	)
	require.NoError(t, err)
	prior := Recovery()
	InstallRecovery(m)
	t.Cleanup(func() { InstallRecovery(prior) })
	s := m.system
	nativeAutomaticStable(t, s)
	ctx := context.Background()
	var failAck atomic.Bool
	failAck.Store(true)
	const callback = "test:fail_exact_system_ack"
	require.NoError(
		t,
		f.db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
			if values, ok := tx.Statement.Dest.(map[string]interface{}); ok {
				if value, ok := values["value"].(types.JSON); ok &&
					strings.Contains(string(value), `"phase":"active"`) &&
					failAck.CompareAndSwap(true, false) {
					require.ErrorContains(
						t,
						tx.AddError(errors.New("controlled SQL acknowledgment failure")),
						"controlled SQL acknowledgment failure",
					)
				}
			}
		}),
	)
	t.Cleanup(func() { require.NoError(t, f.db.Callback().Update().Remove(callback)) })
	require.ErrorContains(
		t,
		c.Step(ctx),
		"controlled SQL acknowledgment failure",
		"reach the exact injected fault, not an unrelated closed precondition",
	)
	prepared, err := readSystemReceipt(ctx, f.db)
	require.NoError(t, err)
	require.NotNil(t, prepared)
	require.Equal(t, "prepared", prepared.Phase)
	require.Zero(t, r.Exists(ctx, c.Guard.PermitKey()).Val())
	require.ErrorIs(
		t,
		s.withdraw(ctx, "controlled_backoff"),
		ErrNotReady,
		"a later diagnostic must not hide the exact committed migration receipt",
	)
	require.NoError(t, c.Step(ctx), "healthy ordinary startup needs no approval key or signature")
	state, _, err := s.current(ctx)
	require.NoError(t, err)
	require.Equal(t, "canary", state.Phase)
	require.Empty(t, state.CanarySourceID)
	receipt, err := readSystemReceipt(ctx, f.db)
	require.NoError(t, err)
	require.Equal(t, "active", receipt.Phase)
	require.Equal(t, c.Guard.Fingerprint(), r.Get(ctx, c.Guard.PermitKey()).Val())
	handled, err := s.queueSource(f.userContext(f.viewer), f.source.ID, f.config)
	require.True(t, handled)
	require.NoError(t, err, "existing lawful user intent needs no internal approval")
	handled, err = QueueScheduledSource(ctx, f.source.ID, f.config)
	require.True(t, handled)
	require.NoError(
		t,
		err,
		"server scheduler accepts current delegated intent without a fabricated run identity",
	)
	_, err = QueueScheduledSource(f.userContext(f.owner), f.source.ID, f.config)
	require.ErrorIs(
		t,
		err,
		ErrRecoveryRejected,
		"an HTTP principal cannot choose the scheduler authority branch",
	)
	_, err = QueueNormalSource(ctx, f.source.ID, f.config)
	require.ErrorIs(
		t,
		err,
		ErrRecoveryRejected,
		"an absent session does not gain user import authority",
	)
	capabilities := types.StringArray{
		string(types.APIKeyCapabilityManageDataSources),
		string(types.APIKeyCapabilityIngest),
	}
	key := f.rawKey(t, types.APIKeyScopeUserMCP, false, capabilities, "dynamic_all")
	keyCtx := f.keyContext(key, types.APIKeyScopeUserMCP, false, capabilities, false)
	require.Equal(
		t,
		"sync_service_rechecking",
		AdmissionFailureReason(keyCtx, f.source.ID, f.config),
	)
	require.NoError(
		t,
		f.db.Table("tenant_api_keys").
			Where("id=?", key).
			Update("revoked_at", time.Now().UTC()).
			Error,
	)
	require.Equal(
		t,
		"sync_source_permission_denied",
		AdmissionFailureReason(keyCtx, f.source.ID, f.config),
	)
	log := types.SyncLog{
		ID:           uuid.NewString(),
		DataSourceID: f.source.ID,
		TenantID:     f.tenant.ID,
		Status:       types.SyncLogStatusRunning,
	}
	require.NoError(t, f.db.Create(&log).Error)
	work, handled, err := PrepareAutomaticSourceExecution(ctx, f.source.ID, log.ID, f.tenant.ID)
	require.True(t, handled)
	require.NoError(t, err)
	repo := repository.NewSyncLogRepository(f.db).(interfaces.SyncExecutionRepository)
	execution, claimed, err := repo.ClaimExecution(work, f.source.ID, log.ID, f.tenant.ID)
	require.NoError(t, err)
	require.True(
		t,
		claimed,
		"actual assigned generation passes the transactional epoch/source admission check",
	)
	work = types.WithSyncExecution(work, execution)
	work = feishusafety.WithRecoveryExecution(work, f.config, log.ID, execution.Generation)
	work, err = s.bindSource(work, f.source.ID, f.config)
	require.NoError(t, err)
	require.NoError(t, c.Guard.Check(feishusafety.WithScope(work, f.source.ID)))
	// A run marked successful with no actual atomic publication cannot open
	// other sources. This fixture deliberately creates no document or index.
	finished := time.Now().UTC()
	log.Status, log.FinishedAt = types.SyncLogStatusSuccess, &finished
	valid, err := repo.WriteExecution(work, execution, &f.source, &log, true)
	require.NoError(t, err)
	require.True(t, valid)
	state, raw, err := s.current(ctx)
	require.NoError(t, err)
	require.NoError(t, s.advanceCanary(ctx, state, raw))
	state, _, err = s.current(ctx)
	require.NoError(t, err)
	require.Equal(t, "canary", state.Phase)
	require.Empty(t, state.CanarySourceID, "unsuitable candidate is released, not promoted")
	require.NotEmpty(t, r.HGet(ctx, c.prefix+"system_source_backoff", f.source.ID).Val())
	require.Error(t, c.Guard.Check(feishusafety.WithScope(work, f.source.ID)))
	require.Zero(t, r.Exists(ctx, c.prefix+"trip").Val(), "source failure is not a global trip")
	require.NoError(t, c.Guard.Trip(ctx, "operator_emergency_stop"))
	require.ErrorIs(t, c.Step(ctx), ErrNotReady)
	require.Equal(t, "operator_emergency_stop", r.Get(ctx, c.prefix+"trip").Val())
	require.Zero(t, r.Exists(ctx, c.Guard.PermitKey()).Val())
}

func TestAutomaticServiceNativeSQLPreparedRestartCreatesNewEpoch(t *testing.T) {
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
	nativeAutomaticStable(t, m.system)
	ctx := context.Background()
	old := systemServiceState{
		Version: 1, Namespace: c.prefix, Epoch: uuid.NewString(), RuntimeSHA: c.Config.SourceSHA,
		WorkerID: c.Config.WorkerID, WorkerValue: c.fingerprint + ":" + uuid.NewString(), MembershipRevision: "1",
		GuardFingerprint: c.Guard.Fingerprint(), Phase: "canary",
	}
	raw, err := json.Marshal(old)
	require.NoError(t, err)
	prepared := &systemServiceReceipt{
		Version: 1, Namespace: c.prefix, Epoch: old.Epoch, RuntimeSHA: old.RuntimeSHA,
		WorkerValue: old.WorkerValue, Phase: "prepared", StateJSON: string(raw),
	}
	require.NoError(
		t,
		prepareSystemReceipt(ctx, f.db, prepared, nil),
		"simulate predecessor SQL commit before its Redis transition",
	)
	require.Zero(t, r.Exists(ctx, c.prefix+"system_service").Val())
	require.NoError(t, c.Step(ctx))
	current, _, err := m.system.current(ctx)
	require.NoError(t, err)
	require.NotEqual(t, old.Epoch, current.Epoch)
	require.NotEqual(t, old.WorkerValue, current.WorkerValue)
	var retained types.SystemSetting
	require.NoError(
		t,
		f.db.First(&retained, "key=?", systemServiceSetting+".epoch."+old.Epoch).Error,
	)
	var immutable systemServiceReceipt
	require.NoError(t, json.Unmarshal(retained.Value, &immutable))
	require.Equal(
		t,
		*prepared,
		immutable,
		"superseding a known preparation preserves its complete immutable evidence",
	)
}

func TestAutomaticServiceNativeSQLCommittedRestartAcknowledgesThenRequalifies(t *testing.T) {
	f := newAutomaticAuthorityFixture(t)
	health, r, opt := nativeAutomaticService(t)
	consumer := startNativeAutomaticConsumer(t, opt)
	c := health.c
	m, err := NewAutomaticServiceManager(
		c,
		f.db,
		repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
	)
	require.NoError(t, err)
	nativeAutomaticStable(t, m.system)
	var fail atomic.Bool
	fail.Store(true)
	const callback = "test:committed_restart_ack"
	require.NoError(
		t,
		f.db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
			if values, ok := tx.Statement.Dest.(map[string]interface{}); ok {
				if value, ok := values["value"].(types.JSON); ok &&
					strings.Contains(string(value), `"phase":"active"`) &&
					fail.CompareAndSwap(true, false) {
					require.ErrorContains(
						t,
						tx.AddError(
							errors.New("controlled committed restart acknowledgment failure"),
						),
						"controlled committed restart acknowledgment failure",
					)
				}
			}
		}),
	)
	t.Cleanup(func() { require.NoError(t, f.db.Callback().Update().Remove(callback)) })
	ctx := context.Background()
	require.ErrorContains(t, c.Step(ctx), "controlled committed restart acknowledgment failure")
	prepared, err := readSystemReceipt(ctx, f.db)
	require.NoError(t, err)
	require.NotNil(t, prepared)
	require.Equal(t, "prepared", prepared.Phase)
	old, err := systemState(r.Get(ctx, c.prefix+"system_service").Val())
	require.NoError(t, err)
	consumer.Shutdown()
	require.NoError(t, c.Drain(ctx), "normal fenced departure retains the prepared receipt")
	guard, err := feishusafety.New(r, feishusafety.Config{
		Namespace:    strings.TrimSuffix(strings.TrimPrefix(c.prefix, "feishu:safety:{"), "}:"),
		EndpointHost: "gateway.invalid", InFlight: 1, RPM: 30, RPS: 1, TPM: 30000,
		WaitBudgetDuration: time.Second, TokenBoundVerified: true,
	})
	require.NoError(t, err)
	replacement, err := New(r, guard, c.Config, c.Stop, c.Audit)
	require.NoError(t, err)
	replacement.ApprovedConsumers = ApprovedConsumers(r)
	next, err := NewAutomaticServiceManager(
		replacement,
		f.db,
		repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
	)
	require.NoError(t, err)
	startNativeAutomaticConsumer(t, opt)
	nativeAutomaticStable(t, next.system)
	require.ErrorIs(t, replacement.Step(ctx), ErrNotReady,
		"acknowledging predecessor durability does not authorize its identity")
	acknowledged, err := readSystemReceipt(ctx, f.db)
	require.NoError(t, err)
	require.Equal(t, "active", acknowledged.Phase)
	require.Equal(t, old.Epoch, acknowledged.Epoch)
	require.Zero(t, r.Exists(ctx, guard.PermitKey()).Val())
	require.NoError(t, replacement.Step(ctx))
	state, _, err := next.system.current(ctx)
	require.NoError(t, err)
	require.NotEqual(t, old.Epoch, state.Epoch)
	require.NotEqual(t, old.WorkerValue, state.WorkerValue)
	require.NotEqual(t, old.ConsumerServerID, state.ConsumerServerID)
	require.Equal(t, "canary", state.Phase)
	require.Empty(t, state.CanarySourceID)
	require.Equal(t, guard.Fingerprint(), r.Get(ctx, guard.PermitKey()).Val())
}

func TestAutomaticServiceNativeSQLSameIncarnationConsumerConflictStaysClosed(t *testing.T) {
	f := newAutomaticAuthorityFixture(t)
	health, r, opt := nativeAutomaticService(t)
	first := startNativeAutomaticConsumer(t, opt)
	c := health.c
	m, err := NewAutomaticServiceManager(
		c,
		f.db,
		repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
	)
	require.NoError(t, err)
	nativeAutomaticStable(t, m.system)
	ctx := context.Background()
	require.NoError(t, c.Step(ctx))
	before, _, err := m.system.current(ctx)
	require.NoError(t, err)
	first.Shutdown()
	require.ErrorIs(t, c.Step(ctx), ErrNotReady)
	require.Zero(t, r.Exists(ctx, c.Guard.PermitKey()).Val())
	startNativeAutomaticConsumer(t, opt)
	replacement := nativeAutomaticStable(t, m.system)
	require.NotEqual(t, before.ConsumerServerID, replacement.ConsumerServerIDs[0])
	require.Equal(
		t,
		before.WorkerValue,
		replacement.WorkerValue,
		"this is a consumer conflict, not an orderly worker incarnation change",
	)
	require.ErrorIs(t, c.Step(ctx), ErrNotReady)
	require.Equal(t, "system_consumer_identity_conflict", r.Get(ctx, c.prefix+"trip").Val())
	require.Zero(t, r.Exists(ctx, c.Guard.PermitKey()).Val())
	receipt, err := readSystemReceipt(ctx, f.db)
	require.NoError(t, err)
	require.Equal(t, before.Epoch, receipt.Epoch)
	require.ErrorIs(t, c.Step(ctx), ErrNotReady)
	require.Equal(
		t,
		"system_consumer_identity_conflict",
		r.Get(ctx, c.prefix+"trip").Val(),
		"further healthy reads cannot erase a real identity conflict",
	)
}
