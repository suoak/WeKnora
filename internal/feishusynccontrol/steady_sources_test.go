package feishusynccontrol

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type steadyFixture struct {
	*recoveryFixture
	ids       []string
	facts     map[string]RecoveryFacts
	authority map[string]SourceManagementAuthority
}

func newSteadyFixture(t *testing.T) *steadyFixture {
	t.Helper()
	f := &steadyFixture{recoveryFixture: reviewedCanaryFixture(t), facts: map[string]RecoveryFacts{},
		authority: map[string]SourceManagementAuthority{}}
	for i := 0; i < 4; i++ {
		id := uuid.NewString()
		facts := f.recoveryFixture.facts
		facts.SourceID = id
		f.ids = append(f.ids, id)
		f.facts[id] = facts
		f.authority[id] = SourceManagementAuthority{TenantID: uint64(i%3 + 1), KBID: uuid.NewString(),
			SourceID: id, SourceType: []string{"feishu", "feishu_drive"}[i%2], ActorID: "operator",
			SourceStatus: "active", AccessState: "unmanaged",
			AuthorityHash: strings.Repeat("f", 64)}
	}
	f.m.facts = func(_ context.Context, id string) (RecoveryFacts, error) { return f.facts[id], nil }
	f.c.approvedFacts = f.m.facts
	f.m.ConfigureSteadyAuthority(func(_ context.Context, id, actor string) (SourceManagementAuthority, error) {
		a, ok := f.authority[id]
		if !ok || a.ActorID != actor {
			return a, ErrRecoveryRejected
		}
		return a, nil
	})
	return f
}

func (f *steadyFixture) sign(t *testing.T, p SteadyPlan) RecoveryApproval {
	t.Helper()
	hash, err := RecoveryDigest(p)
	require.NoError(t, err)
	a := RecoveryApproval{Version: 2, Algorithm: "Ed25519", KeyID: f.m.keyID, Incarnation: p.Incarnation,
		Operation: "steady_sources", PlanSHA256: hash, ActorID: "operator", Reference: "isolated-steady-review",
		EvidenceSHA256: strings.Repeat("d", 64), ExpiresMS: p.ExpiresMS}
	body, err := recoveryCanonical(a)
	require.NoError(t, err)
	a.Signature = hex.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(f.key), body))
	return a
}

func (f *steadyFixture) activate(t *testing.T) SteadyPlan {
	t.Helper()
	ctx := context.Background()
	p, err := f.m.PlanSteady(ctx, "operator", f.ids, 7)
	require.NoError(t, err)
	require.NoError(t, f.m.ActivateSteady(ctx, p, f.sign(t, p), "operator"))
	return p
}

func TestSteadySourcesAcrossThreeTenantsRetainQueueAndRenewOnlyHealth(t *testing.T) {
	f := newSteadyFixture(t)
	ctx := context.Background()
	before, err := f.m.queue(ctx)
	require.NoError(t, err)
	p := f.activate(t)
	require.Greater(t, p.AuthorizationUntilMS-p.CreatedMS, int64(time.Hour/time.Millisecond))
	raw, err := f.r.Get(ctx, f.c.prefix+"recovery").Result()
	require.NoError(t, err)
	require.NoError(t, f.r.PExpire(ctx, f.c.prefix+"steady_health", time.Second).Err())
	require.NoError(t, f.c.Step(ctx))
	require.Greater(t, f.r.PTTL(ctx, f.c.prefix+"steady_health").Val(), 10*time.Second)
	require.Equal(t, raw, f.r.Get(ctx, f.c.prefix+"recovery").Val(), "health renewal must not alter the signed grant")
	for _, id := range f.ids {
		work, approved, err := BindApprovedSource(ctx, id, f.facts[id].ConfigSHA256)
		require.NoError(t, err)
		require.True(t, approved)
		require.NoError(t, f.c.Guard.Check(feishusafety.WithScope(work, id)))
	}
	_, _, err = BindApprovedSource(ctx, uuid.NewString(), strings.Repeat("b", 64))
	require.Error(t, err)
	after, err := f.m.queue(ctx)
	require.NoError(t, err)
	require.Equal(t, before.digest, after.digest)
	require.Equal(t, before.ids, after.ids)
	require.Equal(t, before.fields, after.fields)
	require.True(t, f.r.Exists(ctx, "asynq:{sync}:paused").Val() == 1)
	require.Zero(t, f.r.Exists(ctx, "asynq:{approved_sync}:paused").Val())
}

func TestSteadySourcesAuthorizationRevocationIsSourceLocal(t *testing.T) {
	f := newSteadyFixture(t)
	ctx := context.Background()
	f.activate(t)
	delete(f.authority, f.ids[0])
	_, _, err := BindApprovedSource(ctx, f.ids[0], f.facts[f.ids[0]].ConfigSHA256)
	require.Error(t, err)
	require.NoError(t, f.c.Step(ctx), "one revoked manager must not stop other sources")
	work, _, err := BindApprovedSource(ctx, f.ids[1], f.facts[f.ids[1]].ConfigSHA256)
	require.NoError(t, err)
	require.NoError(t, f.c.Guard.Check(feishusafety.WithScope(work, f.ids[1])))
	require.Zero(t, f.r.Exists(ctx, f.c.prefix+"trip").Val())
	fact := f.facts[f.ids[1]]
	fact.Eligible = false
	f.facts[f.ids[1]] = fact
	_, _, err = BindApprovedSource(ctx, f.ids[1], fact.ConfigSHA256)
	require.Error(t, err)
	require.NoError(t, f.c.Step(ctx), "source hold is independently denied")
}

func TestSteadySourcesExpiredHealthCannotBeRecreated(t *testing.T) {
	f := newSteadyFixture(t)
	ctx := context.Background()
	f.activate(t)
	require.NoError(t, f.r.Del(ctx, f.c.prefix+"steady_health").Err())
	_, _, err := BindApprovedSource(ctx, f.ids[0], f.facts[f.ids[0]].ConfigSHA256)
	require.Error(t, err)
	require.Error(t, f.c.Step(ctx))
	require.Zero(t, f.r.Exists(ctx, f.c.prefix+"steady_health").Val())
	require.EqualValues(t, 1, f.r.Exists(ctx, f.c.prefix+"trip").Val())
	require.Zero(t, f.r.Exists(ctx, f.c.Guard.PermitKey()).Val())
}

func TestSteadySourcesRejectStaleSignatureStateAndAuthority(t *testing.T) {
	f := newSteadyFixture(t)
	ctx := context.Background()
	p, err := f.m.PlanSteady(ctx, "operator", f.ids, 7)
	require.NoError(t, err)
	a := f.sign(t, p)
	changed := p
	changed.AuthorizationUntilMS += 1000
	require.Error(t, f.m.ActivateSteady(ctx, changed, a, "operator"))
	delete(f.authority, f.ids[2])
	require.Error(t, f.m.ActivateSteady(ctx, p, a, "operator"))
	require.Equal(t, "approved_sync_frozen", f.r.Get(ctx, "asynq:{approved_sync}:paused").Val())
	require.Zero(t, f.r.Exists(ctx, f.c.prefix+"steady_health").Val())
}

func TestSteadySourcesNewErrorDeniedButApprovedTransientRetryAllowed(t *testing.T) {
	f := newSteadyFixture(t)
	ctx := context.Background()
	id := f.ids[0]
	facts := f.facts[id]
	facts.Eligible = false
	facts.RetryEligible = true
	f.facts[id] = facts
	_, err := f.m.PlanSteady(ctx, "operator", f.ids, 7)
	require.Error(t, err)
	facts.Eligible = true
	facts.RetryEligible = false
	f.facts[id] = facts
	f.activate(t)
	facts.Eligible = false
	facts.RetryEligible = true
	f.facts[id] = facts
	authority := f.authority[id]
	authority.SourceStatus = "error"
	f.authority[id] = authority
	_, _, err = BindApprovedSource(ctx, id, facts.ConfigSHA256)
	require.NoError(t, err)
	facts.RetryEligible = false
	f.facts[id] = facts
	_, _, err = BindApprovedSource(ctx, id, facts.ConfigSHA256)
	require.Error(t, err)
}

func TestSteadySourcesSignedRevocationInvalidatesOldDeliveriesWithoutDeletingQueue(t *testing.T) {
	f := newSteadyFixture(t)
	ctx := context.Background()
	f.activate(t)
	work, _, err := BindApprovedSource(ctx, f.ids[0], f.facts[f.ids[0]].ConfigSHA256)
	require.NoError(t, err)
	require.NoError(t, f.r.RPush(ctx, "asynq:{approved_sync}:pending", "existing-approved-delivery").Err())
	p, err := f.m.PlanSteady(ctx, "operator", nil, 7)
	require.NoError(t, err)
	require.True(t, p.RevocationOnly)
	require.NoError(t, f.m.ActivateSteady(ctx, p, f.sign(t, p), "operator"))
	require.Error(t, f.c.Guard.Check(feishusafety.WithScope(work, f.ids[0])))
	require.Equal(t, []string{"existing-approved-delivery"},
		f.r.LRange(ctx, "asynq:{approved_sync}:pending", 0, -1).Val())
	require.NoError(t, f.c.Step(ctx))
	var state recoveryMode
	require.NoError(t, json.Unmarshal([]byte(f.r.Get(ctx, f.c.prefix+"recovery").Val()), &state))
	require.Empty(t, state.ApprovedSources)
	require.Equal(t, 2, state.Version, "old version-1 readers must fail closed")
}

func TestSteadySourcesSignedGrantOutlivesIssueWindowButNeverAuthorizationEnd(t *testing.T) {
	f := newSteadyFixture(t)
	f.activate(t)
	ctx := context.Background()
	var state recoveryMode
	require.NoError(t, json.Unmarshal([]byte(f.r.Get(ctx, f.c.prefix+"recovery").Val()), &state))
	tomorrow := state.Steady.Plan.CreatedMS + int64(24*time.Hour/time.Millisecond)
	require.NoError(t, f.m.verifySteady(*state.Steady, tomorrow, false))
	require.Error(t, f.m.verifySteady(*state.Steady, tomorrow, true),
		"the consumed short-lived approval cannot activate again")
	require.Error(t, f.m.verifySteady(*state.Steady, state.Steady.Plan.AuthorizationUntilMS, false))
}

func TestSteadySourcesAuditFailureDoesNotActivatePolicy(t *testing.T) {
	f := newSteadyFixture(t)
	ctx := context.Background()
	p, err := f.m.PlanSteady(ctx, "operator", f.ids, 7)
	require.NoError(t, err)
	before := f.r.Get(ctx, f.c.prefix+"recovery").Val()
	require.NoError(t, f.r.Del(ctx, f.c.prefix+"control_audit").Err())
	require.NoError(t, f.r.Set(ctx, f.c.prefix+"control_audit", "isolated_wrong_type", 0).Err())
	require.Error(t, f.m.ActivateSteady(ctx, p, f.sign(t, p), "operator"))
	require.Equal(t, before, f.r.Get(ctx, f.c.prefix+"recovery").Val())
	require.Zero(t, f.r.Exists(ctx, f.c.prefix+"steady_health").Val())
	require.EqualValues(t, 1, f.r.Exists(ctx, "asynq:{approved_sync}:paused").Val())
}

func TestSteadySourcesPartialRevocationDoesNotWaitForExistingWork(t *testing.T) {
	f := newSteadyFixture(t)
	ctx := context.Background()
	f.activate(t)
	require.NoError(t, f.r.RPush(ctx, "asynq:{approved_sync}:active", "running-delivery").Err())
	facts := f.facts[f.ids[1]]
	facts.LiveExecutions = 1
	f.facts[f.ids[1]] = facts
	p, err := f.m.PlanSteady(ctx, "operator", f.ids[1:], 7)
	require.NoError(t, err)
	require.True(t, p.RevocationOnly)
	require.NoError(t, f.m.ActivateSteady(ctx, p, f.sign(t, p), "operator"))
	_, _, err = BindApprovedSource(ctx, f.ids[0], f.facts[f.ids[0]].ConfigSHA256)
	require.Error(t, err)
	require.Equal(t, []string{"running-delivery"}, f.r.LRange(ctx, "asynq:{approved_sync}:active", 0, -1).Val())
}

func TestSteadySourcesSlowAuthorityDoesNotBlockControllerPermitRenewal(t *testing.T) {
	for _, operation := range []string{"plan", "activate"} {
		t.Run(operation, func(t *testing.T) {
			f := newSteadyFixture(t)
			ctx := context.Background()
			plan, err := f.m.PlanSteady(ctx, "operator", f.ids, 7)
			require.NoError(t, err)
			approval := f.sign(t, plan)
			original := f.m.steadyAuthority
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			f.m.ConfigureSteadyAuthority(func(
				ctx context.Context, id, actor string,
			) (SourceManagementAuthority, error) {
				once.Do(func() { close(entered); <-release })
				return original(ctx, id, actor)
			})
			done := make(chan error, 1)
			go func() {
				if operation == "plan" {
					_, err := f.m.PlanSteady(ctx, "operator", f.ids, 7)
					done <- err
				} else {
					done <- f.m.ActivateSteady(ctx, plan, approval, "operator")
				}
			}()
			<-entered
			step := make(chan error, 1)
			go func() { step <- f.c.Step(ctx) }()
			var stepErr error
			select {
			case stepErr = <-step:
			case <-time.After(time.Second):
				close(release)
				<-done
				t.Fatal("authority I/O blocked the control permit loop")
			}
			close(release)
			require.NoError(t, stepErr)
			require.NoError(t, <-done)
		})
	}
}

func TestSteadySourcesOutboundRequiresExactTenantAndCurrentClaim(t *testing.T) {
	f := newSteadyFixture(t)
	ctx := context.Background()
	f.activate(t)
	id := f.ids[1]
	run := uuid.NewString()
	claimLive := true
	f.m.facts = func(ctx context.Context, source string) (RecoveryFacts, error) {
		facts := f.facts[source]
		if execution, ok := types.SyncExecutionFromContext(ctx); ok {
			facts.Eligible = claimLive && execution.SyncLogID == run && execution.Generation == 7
		}
		return facts, nil
	}
	bound, _, err := BindApprovedSource(ctx, id, f.facts[id].ConfigSHA256)
	require.NoError(t, err)
	bound = feishusafety.WithScope(bound, id)
	bound = feishusafety.WithRecoveryExecution(bound, f.facts[id].ConfigSHA256, run, 7)
	calls := 0
	transport := feishusafety.Transport{Embedding: true,
		Base: continuityRoundTripper(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 200, Header: http.Header{},
				Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
		})}
	call := func(work context.Context) error {
		r, err := http.NewRequestWithContext(work, http.MethodPost, "https://gateway.invalid/v1/embeddings",
			strings.NewReader(`{"input":["approved synthetic input"]}`))
		require.NoError(t, err)
		response, err := transport.RoundTrip(r)
		if response != nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
		return err
	}
	require.Error(t, call(bound))
	require.Zero(t, calls)
	wrong := types.WithSyncExecution(bound, types.SyncExecution{
		DataSourceID: id, SyncLogID: run, TenantID: 999, Generation: 7})
	require.Error(t, call(wrong))
	require.Zero(t, calls)
	valid := types.WithSyncExecution(bound, types.SyncExecution{
		DataSourceID: id, SyncLogID: run, TenantID: f.authority[id].TenantID, Generation: 7})
	require.NoError(t, call(valid))
	require.Equal(t, 1, calls)
	claimLive = false
	require.Error(t, call(valid))
	require.Equal(t, 1, calls, "released claims never reach the transport")
	status, err := f.m.Status(ctx, f.recoveryFixture.facts.SourceID)
	require.NoError(t, err)
	stateJSON, err := json.Marshal(status)
	require.NoError(t, err)
	require.NotContains(t, string(stateJSON), "managed_sources", "anchor-only status must not disclose other tenants")
	require.NotContains(t, string(stateJSON), f.authority[id].KBID)
}
