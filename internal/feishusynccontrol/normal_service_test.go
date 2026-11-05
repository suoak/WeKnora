package feishusynccontrol

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type normalFixture struct {
	*steadyFixture
	sources map[string]NormalSourceAuthority
	saved   *NormalServiceRecord
}

func newNormalFixture(t *testing.T) *normalFixture {
	t.Helper()
	f := &normalFixture{steadyFixture: newSteadyFixture(t), sources: map[string]NormalSourceAuthority{}}
	require.NoError(t, f.m.ConfigureNormalService(f.load, f.store, nil))
	return f
}

func (f *normalFixture) load(ctx context.Context, _ *gorm.DB, source, config string) (NormalSourceAuthority, error) {
	a, ok := f.sources[source]
	if !ok || a.ConfigSHA256 != config {
		return a, ErrRecoveryRejected
	}
	if identity, outbound := feishusafety.RecoveryOutboundExecution(ctx); outbound {
		e, claimed := types.SyncExecutionFromContext(ctx)
		if !claimed || e.DataSourceID != source || e.TenantID != a.TenantID ||
			e.SyncLogID != identity.Run || e.Generation != identity.Generation || e.Generation < 1 {
			return a, ErrRecoveryRejected
		}
	}
	return a, nil
}

func (f *normalFixture) store(_ context.Context, record NormalServiceRecord, previous *NormalServiceRecord) error {
	if (f.saved == nil) != (previous == nil) || (previous != nil && *previous != *f.saved) {
		return ErrRecoveryRejected
	}
	f.saved = &record
	return nil
}

func (f *normalFixture) signNormal(t *testing.T, a NormalServiceAuthorization) NormalServiceAuthorization {
	t.Helper()
	a.Signature = ""
	canonical, err := recoveryCanonical(a)
	require.NoError(t, err)
	a.Signature = hex.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(f.key), canonical))
	return a
}

func (f *normalFixture) activateNormal(t *testing.T) NormalServiceAuthorization {
	t.Helper()
	a, err := f.m.PlanNormalService(context.Background(), "operator")
	require.NoError(t, err)
	a = f.signNormal(t, a)
	require.NoError(t, f.m.ActivateNormalService(context.Background(), a, "operator"))
	return a
}

func (f *normalFixture) addSource() NormalSourceAuthority {
	a := NormalSourceAuthority{
		SourceID: uuid.NewString(), TenantID: uint64(len(f.sources) + 1), KBID: uuid.NewString(),
		ConfigSHA256: strings.Repeat("b", 64), AuthoritySHA256: strings.Repeat("c", 64),
	}
	f.sources[a.SourceID] = a
	return a
}

func TestNormalServiceAutomaticallyRegistersFutureSourcesAndKeepsIncidentAudit(t *testing.T) {
	f := newNormalFixture(t)
	ctx := context.Background()
	before, err := f.m.queue(ctx)
	require.NoError(t, err)
	authorization := f.activateNormal(t)
	for range 4 {
		// These sources did not exist when the service was authorized and are
		// absent from every incident facts map and signed source list.
		source := f.addSource()
		work, approved, err := BindApprovedSource(ctx, source.SourceID, source.ConfigSHA256)
		require.NoError(t, err)
		require.True(t, approved)
		identity, _ := feishusafety.RecoveryOutboundExecution(work)
		require.Equal(t, authorization.ID, identity.Plan)
		require.NoError(t, f.c.Guard.Check(feishusafety.WithScope(work, source.SourceID)))
		require.True(t, f.r.HExists(ctx, f.c.prefix+"normal_sources", source.SourceID).Val())
		delete(f.sources, source.SourceID)
		require.Error(t, f.c.Guard.Check(feishusafety.WithScope(work, source.SourceID)), "live withdrawal denies old contexts")
		require.Zero(t, f.r.Exists(ctx, f.c.prefix+"trip").Val(), "one source does not stop other spaces")
	}
	after, err := f.m.queue(ctx)
	require.NoError(t, err)
	require.Equal(t, before.ids, after.ids)
	require.Equal(t, before.fields, after.fields)
	require.NoError(t, f.c.Step(ctx))
	require.NotNil(t, f.saved)
}

func TestNormalServiceGrantCannotBeInventedOrBroadened(t *testing.T) {
	for _, mutation := range []string{"signature", "namespace", "budget", "purpose", "policy", "actor", "expiry"} {
		t.Run(mutation, func(t *testing.T) {
			f := newNormalFixture(t)
			a, err := f.m.PlanNormalService(context.Background(), "operator")
			require.NoError(t, err)
			a = f.signNormal(t, a)
			switch mutation {
			case "signature":
				a.Signature = strings.Repeat("0", 128)
			case "namespace":
				a.Namespace = "another-installation"
			case "budget":
				a.GuardFingerprint = strings.Repeat("f", 64)
			case "purpose":
				a.Purpose = "source_reconciliation"
			case "policy":
				a.SourcePolicy = "skip_resource_permissions"
			case "actor":
				a.ActorID = "another-operator"
			case "expiry":
				a.CreatedMS, a.ActivationExpiresMS = time.Now().Add(-time.Hour).UnixMilli(), time.Now().Add(-time.Minute).UnixMilli()
				a = f.signNormal(t, a)
			}
			require.Error(t, f.m.ActivateNormalService(context.Background(), a, "operator"))
			require.Nil(t, f.saved)
			require.Zero(t, f.r.Exists(context.Background(), f.c.prefix+"normal_service").Val())
		})
	}
}

func TestNormalServiceGracefulDeploymentDoesNotNeedNewSourceApprovals(t *testing.T) {
	f := newNormalFixture(t)
	ctx := context.Background()
	grant := f.activateNormal(t)
	require.NoError(t, f.c.Drain(ctx))
	guard, err := f.c.Guard.Fresh()
	require.NoError(t, err)
	cfg := f.c.Config
	cfg.SourceSHA, cfg.WorkerID = strings.Repeat("d", 40), "replacement:2"
	c, err := New(f.r, guard, cfg, func(context.Context) error { return nil }, func(Event) error { return nil })
	require.NoError(t, err)
	c.Consumers = func(context.Context) ([]string, error) { return []string{cfg.WorkerID}, nil }
	c.ApprovedConsumers = c.Consumers
	m, err := NewRecoveryManager(c, ed25519.NewKeyFromSeed(f.key).Public().(ed25519.PublicKey), f.m.facts)
	require.NoError(t, err)
	require.NoError(t, m.ConfigureNormalService(f.load, f.store, f.saved))
	feishusafety.InstallDefault(guard)
	InstallRecovery(m)
	require.NoError(t, c.Step(ctx))
	source := f.addSource()
	work, approved, err := BindApprovedSource(ctx, source.SourceID, source.ConfigSHA256)
	require.NoError(t, err)
	require.True(t, approved)
	identity, _ := feishusafety.RecoveryOutboundExecution(work)
	require.Equal(t, grant.ID, identity.Plan)
	require.NoError(t, guard.Check(feishusafety.WithScope(work, source.SourceID)))
	_, _, err = feishusafety.ReserveEmbedding(feishusafety.WithScope(work, source.SourceID), []string{"synthetic"})
	require.Error(t, err, "producer admission is not a SQL execution Claim")
	execution := types.SyncExecution{
		DataSourceID: source.SourceID, SyncLogID: uuid.NewString(), TenantID: source.TenantID, Generation: 1,
	}
	claimed := types.WithSyncExecution(work, execution)
	claimed = feishusafety.WithRecoveryExecution(claimed, source.ConfigSHA256, execution.SyncLogID, execution.Generation)
	_, release, err := feishusafety.ReserveEmbedding(feishusafety.WithScope(claimed, source.SourceID), []string{"synthetic"})
	require.NoError(t, err)
	release()
	require.True(t, f.r.Exists(ctx, c.prefix+"recovery").Val() == 1, "old incident evidence remains intact")
	// The independent budget gate still applies to every automatically
	// registered source, not only the producer's Check.
	forged := feishusafety.WithRecoveryExecution(work, strings.Repeat("e", 64), uuid.NewString(), 1)
	_, _, err = feishusafety.ReserveEmbedding(feishusafety.WithScope(forged, source.SourceID), []string{"synthetic"})
	require.Error(t, err)
}

func TestNormalServiceLostStateAndExplicitRevocationStayClosed(t *testing.T) {
	for _, action := range []string{"redis_lost", "tampered", "revoked"} {
		t.Run(action, func(t *testing.T) {
			f := newNormalFixture(t)
			ctx := context.Background()
			grant := f.activateNormal(t)
			source := f.addSource()
			work, _, err := BindApprovedSource(ctx, source.SourceID, source.ConfigSHA256)
			require.NoError(t, err)
			switch action {
			case "redis_lost":
				require.NoError(t, f.r.Del(ctx, f.c.prefix+"normal_service").Err())
			case "tampered":
				record := *f.saved
				record.Authorization.SourcePolicy = "another-policy"
				raw, err := json.Marshal(record)
				require.NoError(t, err)
				require.NoError(t, f.r.Set(ctx, f.c.prefix+"normal_service", string(raw), 0).Err())
			case "revoked":
				require.NoError(t, f.m.DeactivateNormalService(ctx))
				require.Error(t, f.m.ActivateNormalService(ctx, grant, "operator"), "old grant cannot undo revocation")
			}
			require.Error(t, f.c.Guard.Check(feishusafety.WithScope(work, source.SourceID)))
			require.Error(t, f.c.Step(ctx))
		})
	}
}
