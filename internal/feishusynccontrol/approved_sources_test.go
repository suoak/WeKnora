package feishusynccontrol

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestApprovedSourcesEmptyRedisReplacementCannotOpenTraffic(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	// A separate, empty, owned Redis simulates lost/restored control state.
	// Keep the first fixture's six historical tasks and audit entirely intact.
	lost := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = lost.Close() })
	policy := feishusafety.DefaultConfig()
	policy.Namespace = "lost-control-" + uuid.NewString()
	fresh, err := feishusafety.New(lost, policy)
	require.NoError(t, err)
	c, err := New(lost, fresh, f.c.Config, func(context.Context) error { return nil }, func(Event) error { return nil })
	require.NoError(t, err)
	c.Consumers = f.c.Consumers
	_, err = NewRecoveryManager(c, ed25519.NewKeyFromSeed(f.key).Public().(ed25519.PublicKey),
		func(context.Context, string) (RecoveryFacts, error) { return f.facts, nil })
	require.NoError(t, err)
	require.Error(t, c.Step(ctx), "empty state must freeze a replacement process")
	require.Error(t, fresh.Check(feishusafety.WithScope(ctx, f.facts.SourceID)))
	require.Zero(t, lost.Exists(ctx, fresh.PermitKey()).Val())
	require.EqualValues(t, 1, lost.Exists(ctx, c.prefix+"trip").Val())
	require.EqualValues(t, 1, lost.Exists(ctx, "asynq:{sync}:paused").Val())
	require.EqualValues(t, 1, lost.Exists(ctx, "asynq:{approved_sync}:paused").Val())
	require.EqualValues(t, 6, f.r.LLen(ctx, "asynq:{sync}:pending").Val())
}

func TestApprovedSourcesActualConsumerInventory(t *testing.T) {
	for _, privateOnly := range []bool{false, true} {
		t.Run(fmt.Sprint("private_only_", privateOnly), func(t *testing.T) {
			f := newRecoveryFixture(t)
			ctx := context.Background()
			require.NoError(t, f.r.Set(ctx, "asynq:{approved_sync}:paused", "fixture_paused", 0).Err())
			queues := map[string]int{ApprovedSyncQueue: 1}
			if !privateOnly {
				queues["sync"] = 2
			}
			server := asynq.NewServer(asynq.RedisClientOpt{Addr: f.r.Options().Addr},
				asynq.Config{Concurrency: 1, Queues: queues})
			mux := asynq.NewServeMux()
			mux.HandleFunc("never-executed", func(context.Context, *asynq.Task) error { return nil })
			require.NoError(t, server.Start(mux))
			t.Cleanup(server.Shutdown)
			inspector := asynq.NewInspectorFromRedisClient(f.r)
			require.Eventually(t, func() bool {
				servers, err := inspector.Servers()
				return err == nil && len(servers) > 0
			}, 8*time.Second, 50*time.Millisecond)
			ids, err := ApprovedConsumers(f.r)(ctx)
			if privateOnly {
				require.Error(t, err, "private-only consumers must not hide outside the reviewed maintenance inventory")
			} else {
				require.NoError(t, err)
				host, e := os.Hostname()
				require.NoError(t, e)
				require.Equal(t, []string{fmt.Sprintf("%s:%d", host, os.Getpid())}, ids)
			}
			require.EqualValues(t, 6, f.r.LLen(ctx, "asynq:{sync}:pending").Val())
		})
	}
}

type continuityRoundTripper func(*http.Request) (*http.Response, error)

func (f continuityRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestApprovedSourcesEmbeddingUsesBudgetsAndLiveAuthority(t *testing.T) {
	f := reviewedCanaryFixture(t)
	ctx := context.Background()
	p, err := f.m.PlanContinuity(ctx, "operator", []string{f.facts.SourceID})
	require.NoError(t, err)
	require.NoError(t, f.m.ApproveSources(ctx, p, f.approveContinuity(t, p), "operator"))
	work, _, err := BindApprovedSource(ctx, f.facts.SourceID, f.facts.ConfigSHA256)
	require.NoError(t, err)
	work = feishusafety.WithScope(work, f.facts.SourceID)
	run := uuid.NewString()
	work = feishusafety.WithRecoveryExecution(work, f.facts.ConfigSHA256, run, 1)
	work = types.WithSyncExecution(work, types.SyncExecution{
		DataSourceID: f.facts.SourceID, SyncLogID: run, TenantID: 1, Generation: 1,
	})
	calls := 0
	transport := feishusafety.Transport{Embedding: true, Base: continuityRoundTripper(
		func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{
				StatusCode: 200, Header: http.Header{},
				Body: io.NopCloser(strings.NewReader(`{"data":[{"embedding":[0.1,0.2]}]}`)),
			}, nil
		})}
	unclaimed, _, err := BindApprovedSource(ctx, f.facts.SourceID, f.facts.ConfigSHA256)
	require.NoError(t, err)
	unclaimed = feishusafety.WithScope(unclaimed, f.facts.SourceID)
	unclaimed = feishusafety.WithRecoveryExecution(unclaimed, f.facts.ConfigSHA256, run, 1)
	denied, err := http.NewRequestWithContext(unclaimed, "POST", "https://gateway.invalid/v1/embeddings",
		strings.NewReader(`{"input":["no SQL claim"]}`))
	require.NoError(t, err)
	_, err = transport.RoundTrip(denied)
	require.ErrorIs(t, err, feishusafety.ErrClosed)
	require.Zero(t, calls, "a source approval alone must not authorize an unclaimed model request")
	request, err := http.NewRequestWithContext(work, "POST", "https://gateway.invalid/v1/embeddings",
		strings.NewReader(`{"input":["synthetic approved input"]}`))
	require.NoError(t, err)
	response, err := transport.RoundTrip(request)
	require.NoError(t, err)
	_, err = io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, 1, calls)
	f.facts.Eligible = false
	request, err = http.NewRequestWithContext(work, "POST", "https://gateway.invalid/v1/embeddings",
		strings.NewReader(`{"input":["must not leave process"]}`))
	require.NoError(t, err)
	_, err = transport.RoundTrip(request)
	require.ErrorIs(t, err, feishusafety.ErrClosed)
	require.Equal(t, 1, calls, "revoked source must not reach the transport")
}

func reviewedCanaryFixture(t *testing.T) *recoveryFixture {
	t.Helper()
	f := newRecoveryFixture(t)
	ctx := context.Background()
	require.NoError(t, f.m.Reset(ctx, f.p, f.approve(t, "reset"), "operator"))
	require.NoError(t, f.c.Step(ctx))
	require.NoError(
		t,
		f.m.Canary(
			ctx,
			f.p,
			f.approve(t, "canary"),
			"operator",
			func(_ context.Context, bind func(string, int64) error) error { return bind(uuid.NewString(), 1) },
		),
	)
	require.NoError(
		t,
		f.r.Set(ctx, "asynq:{approved_sync}:paused", "approved_sync_frozen", 0).Err(),
	)
	f.c.ApprovedConsumers = func(context.Context) ([]string, error) { return []string{f.c.Config.WorkerID}, nil }
	InstallRecovery(f.m)
	t.Cleanup(func() { InstallRecovery(nil) })
	return f
}

func (f *recoveryFixture) approveContinuity(t *testing.T, p ContinuityPlan) RecoveryApproval {
	t.Helper()
	h, err := RecoveryDigest(p)
	require.NoError(t, err)
	a := RecoveryApproval{
		Version:        2,
		Algorithm:      "Ed25519",
		KeyID:          f.m.keyID,
		Incarnation:    p.Incarnation,
		Operation:      "approved_sources",
		PlanSHA256:     h,
		ActorID:        "operator",
		Reference:      "synthetic-continuity-review",
		EvidenceSHA256: strings.Repeat("e", 64),
		ExpiresMS:      p.ExpiresMS,
	}
	body, err := recoveryCanonical(a)
	require.NoError(t, err)
	a.Signature = hex.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(f.key), body))
	return a
}

func TestApprovedSourcesRetainHistoricalQueueAndCheckLiveAuthority(t *testing.T) {
	f := reviewedCanaryFixture(t)
	ctx := context.Background()
	before, err := f.m.queue(ctx)
	require.NoError(t, err)
	p, err := f.m.PlanContinuity(ctx, "operator", []string{f.facts.SourceID})
	require.NoError(t, err)
	a := f.approveContinuity(t, p)
	require.NoError(t, f.m.ApproveSources(ctx, p, a, "operator"))
	work, approved, err := BindApprovedSource(ctx, f.facts.SourceID, f.facts.ConfigSHA256)
	require.NoError(t, err)
	require.True(t, approved)
	work = feishusafety.WithScope(work, f.facts.SourceID)
	require.NoError(t, f.c.Guard.Check(work))
	require.Error(
		t,
		f.c.Guard.Check(feishusafety.WithScope(ctx, f.facts.SourceID)),
		"policy identity cannot be omitted",
	)
	require.Error(
		t,
		f.c.Guard.Check(feishusafety.WithScope(work, uuid.NewString())),
		"source scope cannot be replaced",
	)
	f.facts.LiveExecutions = 1
	require.NoError(
		t,
		f.c.Guard.Check(
			feishusafety.WithRecoveryExecution(work, f.facts.ConfigSHA256, uuid.NewString(), 1),
		),
		"normal live claims are not themselves a revocation",
	)
	f.facts.Eligible = false
	f.facts.RetryEligible = true
	require.NoError(t, f.c.Guard.Check(work), "a normal retryable source error is not revocation")
	f.facts.RetryEligible = false
	require.ErrorIs(
		t,
		f.c.Guard.Check(work),
		feishusafety.ErrClosed,
		"hold/revoked must reject before outbound",
	)
	f.facts.Eligible = true
	old := f.facts.ConfigSHA256
	f.facts.ConfigSHA256 = strings.Repeat("f", 64)
	require.ErrorIs(
		t,
		f.c.Guard.Check(work),
		feishusafety.ErrClosed,
		"a configuration change revokes approval",
	)
	f.facts.ConfigSHA256 = old
	f.c.Guard.SetApprovedSourceValidator(nil)
	require.Error(
		t,
		f.c.Guard.Check(work),
		"Redis policy cannot substitute for live authority validation",
	)
	f.c.Guard.SetApprovedSourceValidator(f.m.checkApprovedSource)
	after, err := f.m.queue(ctx)
	require.NoError(t, err)
	require.Equal(t, before.digest, after.digest)
	require.Len(t, after.ids, 6)
	require.EqualValues(t, 1, f.r.Exists(ctx, "asynq:{sync}:paused").Val())
	require.Zero(t, f.r.Exists(ctx, "asynq:{approved_sync}:paused").Val())
	require.Error(
		t,
		f.m.ApproveSources(ctx, p, a, "operator"),
		"approval replay must not reopen a later lifecycle",
	)
}

func TestApprovedSourcesRejectChangedPreconditionsWithoutResuming(t *testing.T) {
	for _, change := range []string{
		"signature", "incident", "task_bytes", "worker", "membership", "hold", "private_pending",
		"audit_failure", "duplicate", "consumer_lost",
	} {
		t.Run(change, func(t *testing.T) {
			f := reviewedCanaryFixture(t)
			ctx := context.Background()
			p, err := f.m.PlanContinuity(ctx, "operator", []string{f.facts.SourceID})
			require.NoError(t, err)
			a := f.approveContinuity(t, p)
			before := f.r.Get(ctx, f.c.prefix+"recovery").Val()
			switch change {
			case "consumer_lost":
				f.c.ApprovedConsumers = func(context.Context) ([]string, error) { return nil, nil }
			case "signature":
				a.Signature = strings.Repeat("0", 128)
			case "incident":
				require.NoError(t, f.c.Guard.Trip(ctx, "new_trip"))
			case "task_bytes":
				require.NoError(
					t,
					f.r.HSet(ctx, "asynq:{sync}:t:historical-0", "msg", []byte{9, 0, 255}).Err(),
				)
			case "worker":
				f.c.incarnation = uuid.NewString()
			case "membership":
				require.NoError(t, f.r.Incr(ctx, f.c.prefix+"membership_revision").Err())
			case "hold":
				f.facts.Eligible = false
			case "private_pending":
				require.NoError(
					t,
					f.r.RPush(ctx, "asynq:{approved_sync}:pending", "not-approved").Err(),
				)
			case "audit_failure":
				require.NoError(t, f.r.Set(ctx, f.c.prefix+"control_audit", "wrong-type", 0).Err())
			case "duplicate":
				p.Sources = append(p.Sources, p.Sources[0])
				a = f.approveContinuity(t, p)
			}
			require.Error(t, f.m.ApproveSources(ctx, p, a, "operator"))
			require.Equal(t, before, f.r.Get(ctx, f.c.prefix+"recovery").Val())
			require.EqualValues(t, 1, f.r.Exists(ctx, "asynq:{approved_sync}:paused").Val())
			require.EqualValues(t, 1, f.r.Exists(ctx, "asynq:{sync}:paused").Val())
			require.EqualValues(t, 6, f.r.LLen(ctx, "asynq:{sync}:pending").Val())
		})
	}
}

func TestApprovedSourcesConsumerLossStopsBothLanes(t *testing.T) {
	f := reviewedCanaryFixture(t)
	ctx := context.Background()
	p, err := f.m.PlanContinuity(ctx, "operator", []string{f.facts.SourceID})
	require.NoError(t, err)
	require.NoError(t, f.m.ApproveSources(ctx, p, f.approveContinuity(t, p), "operator"))
	f.c.ApprovedConsumers = func(context.Context) ([]string, error) { return nil, nil }
	require.Error(t, f.c.checkRecoveryPolicy(ctx))
	require.ErrorIs(t, f.c.EmergencyStop(ctx), ErrNotReady)
	require.EqualValues(t, 1, f.r.Exists(ctx, "asynq:{sync}:paused").Val())
	require.EqualValues(t, 1, f.r.Exists(ctx, "asynq:{approved_sync}:paused").Val())
	require.EqualValues(t, 6, f.r.LLen(ctx, "asynq:{sync}:pending").Val())
	require.Zero(t, f.r.Exists(ctx, f.c.Guard.PermitKey()).Val())
}

func TestApprovedSourcesExpiryClosesRequests(t *testing.T) {
	f := reviewedCanaryFixture(t)
	ctx := context.Background()
	p, err := f.m.PlanContinuity(ctx, "operator", []string{f.facts.SourceID})
	require.NoError(t, err)
	require.NoError(t, f.m.ApproveSources(ctx, p, f.approveContinuity(t, p), "operator"))
	work, _, err := BindApprovedSource(ctx, f.facts.SourceID, f.facts.ConfigSHA256)
	require.NoError(t, err)
	var state recoveryMode
	require.NoError(t, json.Unmarshal([]byte(f.r.Get(ctx, f.c.prefix+"recovery").Val()), &state))
	state.ExpiresMS = time.Now().Add(-time.Second).UnixMilli()
	raw, _ := json.Marshal(state)
	require.NoError(t, f.r.Set(ctx, f.c.prefix+"recovery", raw, 0).Err())
	require.Error(t, f.c.Guard.Check(feishusafety.WithScope(work, f.facts.SourceID)))
	require.Error(t, f.c.checkRecoveryPolicy(ctx))
}

func TestApprovedSourcesHistoricalQueueMutationClosesPolicy(t *testing.T) {
	f := reviewedCanaryFixture(t)
	ctx := context.Background()
	p, err := f.m.PlanContinuity(ctx, "operator", []string{f.facts.SourceID})
	require.NoError(t, err)
	require.NoError(t, f.m.ApproveSources(ctx, p, f.approveContinuity(t, p), "operator"))
	require.NoError(t, f.c.checkRecoveryPolicy(ctx))
	require.NoError(t, f.r.HSet(ctx, "asynq:{sync}:t:historical-0", "msg", "isolated-fault-injection").Err())
	require.Error(t, f.c.checkRecoveryPolicy(ctx), "historical task bytes remain bound after activation")
	require.ErrorIs(t, f.c.EmergencyStop(ctx), ErrNotReady)
	require.Zero(t, f.r.Exists(ctx, f.c.Guard.PermitKey()).Val())
	require.EqualValues(t, 1, f.r.Exists(ctx, "asynq:{sync}:paused").Val())
}

func TestApprovedSourcesDigestPreservesBinaryHashFieldIdentity(t *testing.T) {
	f := reviewedCanaryFixture(t)
	ctx := context.Background()
	key := "asynq:{sync}:t:historical-0"
	require.NoError(t, f.r.HSet(ctx, key, string([]byte{0xff}), "unchanged-value").Err())
	p, err := f.m.PlanContinuity(ctx, "operator", []string{f.facts.SourceID})
	require.NoError(t, err)
	a := f.approveContinuity(t, p)
	require.NoError(t, f.r.HDel(ctx, key, string([]byte{0xff})).Err())
	require.NoError(t, f.r.HSet(ctx, key, string([]byte{0xfe}), "unchanged-value").Err())
	require.Error(t, f.m.ApproveSources(ctx, p, a, "operator"), "distinct binary field names must not share a digest")
	require.EqualValues(t, 1, f.r.Exists(ctx, "asynq:{sync}:paused").Val())
}
