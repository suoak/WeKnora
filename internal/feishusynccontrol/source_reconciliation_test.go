package feishusynccontrol

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func reconciliationOperator() context.Context {
	ctx := context.WithValue(context.Background(), types.UserIDContextKey, "operator")
	return types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: "operator"})
}

func reconciliationFixture(t *testing.T) (*steadyFixture, SteadyPlan, string) {
	t.Helper()
	f := newSteadyFixture(t)
	id := f.ids[0]
	a := f.authority[id]
	a.AccessState = "hold"
	a.SourceStatus = "error"
	f.authority[id] = a
	facts := f.facts[id]
	facts.Eligible = false
	facts.ReconciliationEligible = true
	f.facts[id] = facts
	_, err := f.m.PlanSteady(context.Background(), "operator", f.ids, 7)
	require.Error(t, err, "ordinary approval must never admit hold")
	p, err := f.m.PlanSteadyWithReconciliation(context.Background(), "operator", f.ids[1:], []string{id}, 7)
	require.NoError(t, err)
	require.NoError(t, f.m.ActivateSteady(context.Background(), p, f.sign(t, p), "operator"))
	return f, p, id
}

func TestSourceReconciliationExplicitBoundedWindow(t *testing.T) {
	f, previous, id := reconciliationFixture(t)
	ctx := reconciliationOperator()
	for _, entry := range previous.ManagedSources {
		if entry.Reconciliation {
			require.Equal(t, time.Hour.Milliseconds(), entry.ReconcileUntilMS-previous.CreatedMS)
		}
	}
	for _, window := range []time.Duration{0, 30 * time.Minute, 13 * time.Hour} {
		_, err := f.m.PlanSteadyWithReconciliation(ctx, "operator", f.ids[1:], []string{id}, 7, window)
		require.Error(t, err)
	}
	_, err := f.m.PlanSteadyWithReconciliation(ctx, "operator", f.ids[1:], []string{id}, 7, time.Hour, time.Hour)
	require.Error(t, err)
	p, err := f.m.PlanSteadyWithReconciliation(ctx, "operator", f.ids[1:], []string{id}, 7, 12*time.Hour)
	require.NoError(t, err)
	for _, entry := range p.ManagedSources {
		if entry.Reconciliation {
			require.Equal(t, (12 * time.Hour).Milliseconds(), entry.ReconcileUntilMS-p.CreatedMS)
		} else {
			require.Zero(t, entry.ReconcileUntilMS)
		}
	}
	// A caller cannot extend the signed window, even inside the allowed maximum.
	approval := f.sign(t, p)
	tampered := p
	tampered.ManagedSources = append([]SteadySource(nil), p.ManagedSources...)
	for i := range tampered.ManagedSources {
		if tampered.ManagedSources[i].Reconciliation {
			tampered.ManagedSources[i].ReconcileUntilMS--
		}
	}
	require.Error(t, f.m.ActivateSteady(ctx, tampered, approval, "operator"))
	require.NoError(t, f.m.ActivateSteady(ctx, p, approval, "operator"))
	_, _, err = BindApprovedSource(ctx, id, f.facts[id].ConfigSHA256)
	require.Error(t, err, "longer purpose never grants ordinary sync to a held source")
	_, err = f.m.BeginSourceReconciliation(ctx, id, p.ID)
	require.NoError(t, err)
	_, err = f.m.BeginSourceReconciliation(ctx, id, p.ID)
	require.Error(t, err, "one shot remains one shot")
}

func TestSourceReconciliationOneShotRequiresActorTicketAndActualClaim(t *testing.T) {
	f, p, id := reconciliationFixture(t)
	ctx := reconciliationOperator()
	before, err := f.m.queue(ctx)
	require.NoError(t, err)
	_, _, err = BindApprovedSource(ctx, id, f.facts[id].ConfigSHA256)
	require.Error(t, err, "normal manual/cron context cannot consume reconciliation purpose")
	_, err = f.m.BeginSourceReconciliation(context.Background(), id, p.ID)
	require.Error(t, err, "a machine/background context is not the managing actor")
	work, err := f.m.BeginSourceReconciliation(ctx, id, p.ID)
	require.NoError(t, err)
	require.Equal(t, p.ID, ReconciliationPolicy(work, id))
	require.Empty(t, ReconciliationPolicy(work, f.ids[1]))
	_, err = f.m.BeginSourceReconciliation(ctx, id, p.ID)
	require.Error(t, err, "one signed source ticket only")
	run := uuid.NewString()
	require.NoError(t, f.m.BindSourceReconciliationRun(work, id, run))
	require.Error(t, f.m.BindSourceReconciliationRun(work, id, run))
	_, err = RestoreSourceReconciliation(work, id, p.ID)
	require.Error(t, err)
	e := types.SyncExecution{DataSourceID: id, SyncLogID: run, TenantID: f.authority[id].TenantID, Generation: 1}
	wrong := e
	wrong.TenantID++
	_, err = RestoreSourceReconciliation(types.WithSyncExecution(context.Background(), wrong), id, p.ID)
	require.Error(t, err)
	claimed, err := RestoreSourceReconciliation(types.WithSyncExecution(context.Background(), e), id, p.ID)
	require.NoError(t, err)
	require.True(t, types.LegacyReconciliationRead(claimed, id))
	require.False(t, types.LegacyFullReconciliation(claimed), "read purpose must not complete a partial reconciliation")
	require.False(t, types.LegacyReconciliationRead(claimed, f.ids[1]))
	e.Generation++
	require.False(t, types.LegacyReconciliationRead(types.WithSyncExecution(claimed, e), id))
	after, err := f.m.queue(ctx)
	require.NoError(t, err)
	require.Equal(t, before.digest, after.digest)
	require.Equal(t, before.fields, after.fields)
	require.True(t, f.r.Exists(ctx, "asynq:{sync}:paused").Val() == 1)
}

func TestSourceReconciliationConcurrentReservationHasOneWinner(t *testing.T) {
	f, p, id := reconciliationFixture(t)
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.m.BeginSourceReconciliation(reconciliationOperator(), id, p.ID)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		}
	}
	require.Equal(t, 1, winners)
}

func TestSourceReconciliationPromotionIsFreshAdmissionEvenWithSmallerScope(t *testing.T) {
	f, _, id := reconciliationFixture(t)
	authority := f.authority[id]
	authority.AccessState = "active"
	authority.SourceStatus = "active"
	f.authority[id] = authority
	facts := f.facts[id]
	facts.Eligible = true
	facts.ReconciliationEligible = false
	f.facts[id] = facts
	p, err := f.m.PlanSteady(context.Background(), "operator", []string{id}, 7)
	require.NoError(t, err)
	require.False(t, p.RevocationOnly, "conversion to ongoing service is a fresh authorization")
	require.False(t, p.ManagedSources[0].Reconciliation)
	require.Zero(t, p.ManagedSources[0].ReconcileUntilMS)
	require.NoError(t, f.m.ActivateSteady(context.Background(), p, f.sign(t, p), "operator"))
}

func TestSourceReconciliationRevocationExpiryAndTripStopOnlyAuthorizedWork(t *testing.T) {
	for _, scenario := range []string{"revoked", "authority", "expiry", "trip", "ticket_invention"} {
		t.Run(scenario, func(t *testing.T) {
			f, p, id := reconciliationFixture(t)
			work, err := f.m.BeginSourceReconciliation(reconciliationOperator(), id, p.ID)
			require.NoError(t, err)
			run := uuid.NewString()
			require.NoError(t, f.m.BindSourceReconciliationRun(work, id, run))
			e := types.SyncExecution{DataSourceID: id, SyncLogID: run, TenantID: 1, Generation: 1}
			claimed, err := RestoreSourceReconciliation(types.WithSyncExecution(context.Background(), e), id, p.ID)
			require.NoError(t, err)
			transaction := &gorm.DB{Statement: &gorm.Statement{Context: claimed}}
			require.NoError(t, types.CheckReconciliationPublication(transaction))
			switch scenario {
			case "revoked":
				v := f.facts[id]
				v.ReconciliationEligible = false
				f.facts[id] = v
			case "authority":
				delete(f.authority, id)
			case "expiry":
				// The normal grant survives; only the signed one-shot deadline expires.
				var old recoveryMode
				rawState := f.r.Get(context.Background(), f.c.prefix+"recovery").Val()
				require.NoError(t, json.Unmarshal([]byte(rawState), &old))
				past := old.Steady.Plan
				delta := (2 * time.Hour).Milliseconds()
				past.CreatedMS -= delta
				past.ExpiresMS -= delta
				for i := range past.ManagedSources {
					if past.ManagedSources[i].Reconciliation {
						past.ManagedSources[i].ReconcileUntilMS -= delta
					}
				}
				old.Steady = &SteadyCertificate{Plan: past, Approval: f.sign(t, past), ActivatedMS: past.CreatedMS + 1}
				raw, err := json.Marshal(old)
				require.NoError(t, err)
				require.NoError(t, f.r.Set(context.Background(), f.c.prefix+"recovery", raw, 0).Err())
			case "trip":
				require.NoError(t, f.r.Set(context.Background(), f.c.prefix+"trip", "new_incident", 0).Err())
			case "ticket_invention":
				run = uuid.NewString()
			}
			e.SyncLogID = run
			_, err = RestoreSourceReconciliation(types.WithSyncExecution(context.Background(), e), id, p.ID)
			require.Error(t, err)
			commit := types.WithSyncExecution(claimed, e)
			transaction.Statement.Context = commit
			require.Error(t, types.CheckReconciliationPublication(transaction),
				"authority must still hold at publication, not only at Restore")
			var state recoveryMode
			rawState := f.r.Get(context.Background(), f.c.prefix+"recovery").Val()
			require.NoError(t, json.Unmarshal([]byte(rawState), &state))
			require.Equal(t, p.ID, state.ContinuityID)
		})
	}
}
