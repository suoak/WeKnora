package feishusynccontrol

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type recoveryFixture struct {
	m     *RecoveryManager
	c     *Controller
	r     *redis.Client
	p     RecoveryPlan
	facts RecoveryFacts
	key   []byte
	old   *feishusafety.Guard
}

func newRecoveryFixture(t *testing.T) *recoveryFixture {
	t.Helper()
	ctx := context.Background()
	addr := os.Getenv("FEISHU_RECOVERY_REAL_REDIS_ADDR")
	if addr != "" {
		require.Regexp(t, regexp.MustCompile(`^knowhub-recovery-redis-[a-f0-9]{12}:6379$`), addr)
	} else {
		addr = miniredis.RunT(t).Addr()
	}
	r := redis.NewClient(&redis.Options{Addr: addr, MaxRetries: -1})
	t.Cleanup(func() { _ = r.Close() })
	// Only a freshly created, no-volume, test-owned Redis container may be
	// configured above. Never fall back to DATABASE_URL/production Redis.
	if os.Getenv("FEISHU_RECOVERY_REAL_REDIS_ADDR") != "" {
		require.NoError(t, r.FlushDB(ctx).Err())
	}
	g, e := feishusafety.New(
		r,
		feishusafety.Config{
			Namespace:          "recovery-" + uuid.NewString(),
			EndpointHost:       "gateway.invalid",
			InFlight:           1,
			RPM:                30,
			RPS:                1,
			TPM:                30000,
			WaitBudgetDuration: time.Second,
			TokenBoundVerified: true,
		},
	)
	require.NoError(t, e)
	c, e := New(
		r,
		g,
		Config{
			Automatic:   true,
			WorkerID:    "worker:1",
			SourceSHA:   strings.Repeat("a", 40),
			Scope:       "all",
			Maintenance: 4,
		},
		func(context.Context) error { return nil },
		func(Event) error { return nil },
	)
	require.NoError(t, e)
	c.Consumers = func(context.Context) ([]string, error) { return []string{"worker:1"}, nil }
	require.NoError(t, c.Step(ctx))
	feishusafety.InstallDefault(g)
	t.Cleanup(func() { feishusafety.InstallDefault(nil) })
	require.NoError(t, g.Trip(ctx, "feishu_headers_io_failure"))
	require.NoError(t, r.Set(ctx, "asynq:{sync}:paused", "feishu_safety_stop", 0).Err())
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("historical-%d", i)
		require.NoError(t, r.RPush(ctx, "asynq:{sync}:pending", id).Err())
		require.NoError(
			t,
			r.HSet(ctx, "asynq:{sync}:t:"+id, "msg", []byte{byte(i), 0xff, 0, 1}, "state", "pending").
				Err(),
		)
	}
	f := &recoveryFixture{
		c:   c,
		r:   r,
		old: g,
		key: []byte(strings.Repeat("k", 32)),
		facts: RecoveryFacts{
			SourceID:              uuid.NewString(),
			ConfigSHA256:          strings.Repeat("b", 64),
			FactsSHA256:           strings.Repeat("c", 64),
			Eligible:              true,
			SelectedResourceCount: 1,
		},
	}
	f.m, e = NewRecoveryManager(
		c,
		ed25519.NewKeyFromSeed(f.key).Public().(ed25519.PublicKey),
		func(_ context.Context, _ string) (RecoveryFacts, error) { return f.facts, nil },
	)
	require.NoError(t, e)
	f.p, e = f.m.Plan(ctx, f.facts.SourceID, "operator")
	require.NoError(t, e)
	return f
}

func (f *recoveryFixture) approve(t *testing.T, op string) RecoveryApproval {
	t.Helper()
	h, e := RecoveryDigest(f.p)
	require.NoError(t, e)
	a := RecoveryApproval{
		Version:   2,
		Algorithm: "Ed25519", KeyID: f.m.keyID, Incarnation: f.c.incarnation,
		Operation:      op,
		PlanSHA256:     h,
		ActorID:        "operator",
		Reference:      "synthetic-isolation-approval",
		EvidenceSHA256: strings.Repeat("d", 64),
		ExpiresMS:      f.p.ExpiresMS,
	}
	b, e := recoveryCanonical(a)
	require.NoError(t, e)
	a.Signature = hex.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(f.key), b))
	return a
}

func TestRecoveryResetAndOneSourceCanaryPreserveSharedQueue(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	before, e := f.m.queue(ctx)
	require.NoError(t, e)
	oldInc := f.c.incarnation
	require.NoError(t, f.m.Reset(ctx, f.p, f.approve(t, "reset"), "operator"))
	require.NotSame(t, f.old, f.c.Guard)
	require.NotEqual(t, oldInc, f.c.incarnation)
	require.ErrorIs(
		t,
		f.old.Check(ctx),
		feishusafety.ErrClosed,
		"the old object must remain closed",
	)
	require.NoError(t, f.c.Step(ctx))
	require.ErrorIs(
		t,
		f.c.Guard.Check(feishusafety.WithScope(ctx, f.facts.SourceID)),
		feishusafety.ErrClosed,
		"reset cannot open normal traffic",
	)
	run := uuid.NewString()
	require.NoError(
		t,
		f.m.Canary(
			ctx,
			f.p,
			f.approve(t, "canary"),
			"operator",
			func(work context.Context, bind func(string, int64) error) error {
				require.NoError(t, f.c.Guard.Check(feishusafety.WithScope(work, f.facts.SourceID)))
				require.ErrorIs(
					t,
					f.c.Guard.Check(feishusafety.WithScope(work, uuid.NewString())),
					feishusafety.ErrClosed,
				)
				require.ErrorIs(
					t,
					f.c.Guard.Check(feishusafety.WithScope(ctx, f.facts.SourceID)),
					feishusafety.ErrClosed,
					"ordinary producer cannot borrow the plan",
				)
				require.NoError(t, bind(run, 1))
				require.ErrorIs(
					t,
					f.c.Guard.Check(feishusafety.WithScope(work, f.facts.SourceID)),
					feishusafety.ErrClosed,
					"admission identity cannot execute",
				)
				valid := feishusafety.WithRecoveryExecution(work, f.facts.ConfigSHA256, run, 1)
				require.NoError(t, f.c.Guard.Check(feishusafety.WithScope(valid, f.facts.SourceID)))
				stale := feishusafety.WithRecoveryExecution(work, f.facts.ConfigSHA256, run, 2)
				require.ErrorIs(
					t,
					f.c.Guard.Check(feishusafety.WithScope(stale, f.facts.SourceID)),
					feishusafety.ErrClosed,
				)
				return nil
			},
		),
	)
	after, e := f.m.queue(ctx)
	require.NoError(t, e)
	require.Equal(t, before.digest, after.digest)
	require.Len(t, after.ids, 6)
	require.ErrorIs(
		t,
		f.c.Guard.Check(feishusafety.WithScope(ctx, f.facts.SourceID)),
		feishusafety.ErrClosed,
	)
	require.EqualValues(t, 1, f.r.Exists(ctx, "asynq:{sync}:paused").Val())
	logs, e := f.r.XRange(ctx, f.c.prefix+"control_audit", "-", "+").Result()
	require.NoError(t, e)
	require.GreaterOrEqual(t, len(logs), 4)
	for _, event := range logs {
		if !strings.HasPrefix(fmt.Sprint(event.Values["event"]), "recovery_") {
			continue
		}
		var signed RecoveryApproval
		require.NoError(
			t,
			json.Unmarshal([]byte(fmt.Sprint(event.Values["signed_approval"])), &signed),
		)
		require.Equal(t, "Ed25519", event.Values["approval_algorithm"])
		require.Equal(t, f.m.keyID, event.Values["approval_key_id"])
		signature, err := hex.DecodeString(signed.Signature)
		require.NoError(t, err)
		signed.Signature = ""
		body, err := recoveryCanonical(signed)
		require.NoError(t, err)
		require.True(t, ed25519.Verify(f.m.key, body, signature))
	}
}

func TestRecoveryRejectsChangedEvidenceWithoutClearingTrip(t *testing.T) {
	for _, change := range []string{
		"signature",
		"actor",
		"namespace",
		"incident",
		"queue",
		"task_bytes",
		"pause",
		"peer",
		"inflight",
		"facts",
		"audit_unavailable",
		"expired",
	} {
		t.Run(change, func(t *testing.T) {
			f := newRecoveryFixture(t)
			ctx := context.Background()
			a := f.approve(t, "reset")
			actor := "operator"
			switch change {
			case "signature":
				a.Signature = strings.Repeat("0", 64)
			case "actor":
				actor = "another"
			case "namespace":
				f.p.Namespace = "other"
				a = f.approve(t, "reset")
			case "incident":
				require.NoError(t, f.r.Set(ctx, f.c.prefix+"trip", "new_incident", 0).Err())
			case "queue":
				require.NoError(t, f.r.RPush(ctx, "asynq:{sync}:pending", "new-task").Err())
			case "task_bytes":
				require.NoError(
					t,
					f.r.HSet(ctx, "asynq:{sync}:t:historical-0", "msg", []byte{0, 0xfe, 0, 1}).
						Err(),
				)
			case "pause":
				require.NoError(
					t,
					f.r.Set(ctx, "asynq:{sync}:paused", "another_operator_stop", 0).Err(),
				)
			case "peer":
				require.NoError(
					t,
					f.r.HSet(ctx, f.c.prefix+"workers", "unexpected:2", "unregistered").Err(),
				)
			case "inflight":
				require.NoError(
					t,
					f.r.HSet(ctx, f.c.prefix+"inflight", "old-live-request", "1").Err(),
				)
			case "facts":
				f.facts.ConfigSHA256 = strings.Repeat("e", 64)
			case "audit_unavailable":
				require.NoError(t, f.r.Set(ctx, f.c.prefix+"control_audit", "wrong-type", 0).Err())
			case "expired":
				f.p.ExpiresMS = time.Now().Add(-time.Second).UnixMilli()
				a = f.approve(t, "reset")
			}
			require.Error(t, f.m.Reset(ctx, f.p, a, actor))
			require.EqualValues(t, 1, f.r.Exists(ctx, f.c.prefix+"trip").Val())
			require.EqualValues(t, 1, f.r.Exists(ctx, "asynq:{sync}:paused").Val())
			if change == "incident" {
				require.Equal(t, "new_incident", f.r.Get(ctx, f.c.prefix+"trip").Val())
			}
		})
	}
}

func TestRecoveryNewTripCannotBeClearedByPriorApproval(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	require.NoError(t, f.m.Reset(ctx, f.p, f.approve(t, "reset"), "operator"))
	require.NoError(t, f.c.Step(ctx))
	require.NoError(t, f.c.Guard.Trip(ctx, "audit_unavailable"))
	require.Error(
		t,
		f.m.Canary(
			ctx,
			f.p,
			f.approve(t, "canary"),
			"operator",
			func(context.Context, func(string, int64) error) error { t.Fatal("must not execute"); return nil },
		),
	)
	require.Error(t, f.m.Reset(ctx, f.p, f.approve(t, "reset"), "operator"))
	require.Equal(t, "audit_unavailable", f.r.Get(ctx, f.c.prefix+"trip").Val())
}

func TestRecoveryMissingPersistentStateFailsClosed(t *testing.T) {
	for _, key := range []string{"recovery", "recovery_required"} {
		t.Run(key, func(t *testing.T) {
			f := newRecoveryFixture(t)
			ctx := context.Background()
			require.NoError(t, f.m.Reset(ctx, f.p, f.approve(t, "reset"), "operator"))
			require.NoError(t, f.c.Step(ctx))
			require.NoError(t, f.r.Del(ctx, f.c.prefix+key).Err())
			require.Error(t, f.c.checkRecoveryPolicy(ctx))
			require.ErrorIs(
				t,
				f.c.Guard.Check(feishusafety.WithScope(ctx, f.facts.SourceID)),
				feishusafety.ErrClosed,
			)
		})
	}
}

func TestRecoveryCanaryFaultsStopAndKeepHistoricalQueue(t *testing.T) {
	for _, fault := range []string{
		"execution_error",
		"unbound",
		"panic",
		"queue_changed",
		"authorization_changed",
		"audit_failed",
	} {
		t.Run(fault, func(t *testing.T) {
			f := newRecoveryFixture(t)
			ctx := context.Background()
			require.NoError(t, f.m.Reset(ctx, f.p, f.approve(t, "reset"), "operator"))
			require.NoError(t, f.c.Step(ctx))
			var revoked atomic.Bool
			run := func() {
				err := f.m.Canary(
					ctx,
					f.p,
					f.approve(t, "canary"),
					"operator",
					func(work context.Context, bind func(string, int64) error) error {
						if fault == "unbound" {
							return nil
						}
						require.NoError(t, bind(uuid.NewString(), 1))
						switch fault {
						case "execution_error":
							return fmt.Errorf("synthetic failure")
						case "panic":
							panic("synthetic panic")
						case "queue_changed":
							require.NoError(
								t,
								f.r.HSet(ctx, "asynq:{sync}:t:historical-0", "state", "changed").
									Err(),
							)
						case "authorization_changed":
							// Revoke the authority without racing the watcher callback.
							revoked.Store(true)
						case "audit_failed":
							require.NoError(t, f.r.Del(ctx, f.c.prefix+"control_audit").Err())
							require.NoError(
								t,
								f.r.Set(ctx, f.c.prefix+"control_audit", "wrong-type", 0).Err(),
							)
							return nil
						}
						select {
						case <-work.Done():
							return work.Err()
						case <-time.After(2 * time.Second):
							return fmt.Errorf("watchdog did not stop")
						}
					},
				)
				require.Error(t, err)
			}
			revoked.Store(false)
			original := f.m.facts
			f.m.facts = func(ctx context.Context, id string) (RecoveryFacts, error) {
				v, e := original(ctx, id)
				if revoked.Load() {
					v.Eligible = false
				}
				return v, e
			}
			if fault == "panic" {
				require.Panics(t, run)
			} else {
				run()
			}
			require.EqualValues(t, 1, f.r.Exists(ctx, f.c.prefix+"trip").Val())
			require.EqualValues(t, 1, f.r.Exists(ctx, "asynq:{sync}:paused").Val())
			require.EqualValues(t, 6, f.r.LLen(ctx, "asynq:{sync}:pending").Val())
		})
	}
}

func TestRecoveryApprovalCannotUseRuntimePublicKeyAsSigningSeed(t *testing.T) {
	f := newRecoveryFixture(t)
	a := f.approve(t, "reset")
	a.Signature = ""
	body, err := recoveryCanonical(a)
	require.NoError(t, err)
	forged := ed25519.NewKeyFromSeed(f.m.key)
	a.Signature = hex.EncodeToString(ed25519.Sign(forged, body))
	require.Error(t, f.m.Reset(context.Background(), f.p, a, "operator"))
	require.EqualValues(t, 1, f.r.Exists(context.Background(), f.c.prefix+"trip").Val())
}

func TestRecoveryRejectsLegacyAlgorithmAndApprovalVersion(t *testing.T) {
	for _, kind := range []string{"legacy_version", "wrong_algorithm", "wrong_key_id"} {
		t.Run(kind, func(t *testing.T) {
			f := newRecoveryFixture(t)
			a := f.approve(t, "reset")
			switch kind {
			case "legacy_version":
				a.Version = 1
			case "wrong_algorithm":
				a.Algorithm = "HMAC-SHA256"
			case "wrong_key_id":
				a.KeyID = strings.Repeat("0", 64)
			}
			require.Error(t, f.m.Reset(context.Background(), f.p, a, "operator"))
			require.EqualValues(t, 1, f.r.Exists(context.Background(), f.c.prefix+"trip").Val())
		})
	}
}

func TestRecoveryCanaryBindsReplacementProcessAndBuild(t *testing.T) {
	for _, change := range []string{"runtime", "incarnation", "registry", "revision", "pre_reset_incarnation"} {
		t.Run(change, func(t *testing.T) {
			f := newRecoveryFixture(t)
			ctx := context.Background()
			require.NoError(t, f.m.Reset(ctx, f.p, f.approve(t, "reset"), "operator"))
			require.NoError(t, f.c.Step(ctx))
			a := f.approve(t, "canary")
			switch change {
			case "runtime":
				f.c.Config.SourceSHA = strings.Repeat("e", 40)
			case "incarnation":
				f.c.incarnation = uuid.NewString()
			case "registry":
				require.NoError(
					t,
					f.r.HSet(ctx, f.c.prefix+"workers", f.p.WorkerID, "other-incarnation").Err(),
				)
			case "revision":
				require.NoError(t, f.r.Incr(ctx, f.c.prefix+"membership_revision").Err())
			case "pre_reset_incarnation":
				a.Incarnation = f.p.Incarnation
				a.Signature = ""
				body, e := recoveryCanonical(a)
				require.NoError(t, e)
				a.Signature = hex.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(f.key), body))
			}
			require.Error(
				t,
				f.m.Canary(
					ctx,
					f.p,
					a,
					"operator",
					func(context.Context, func(string, int64) error) error {
						t.Fatal("changed execution must not run")
						return nil
					},
				),
			)
			require.EqualValues(t, 6, f.r.LLen(ctx, "asynq:{sync}:pending").Val())
			require.EqualValues(t, 1, f.r.Exists(ctx, "asynq:{sync}:paused").Val())
		})
	}
}

func TestRecoveryPythonSignedApprovalFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/recovery-ed25519-v2.json")
	require.NoError(t, err)
	var fixture struct {
		PublicKey string           `json:"public_key_hex"`
		Approval  RecoveryApproval `json:"approval"`
	}
	require.NoError(t, json.Unmarshal(raw, &fixture))
	public, err := hex.DecodeString(fixture.PublicKey)
	require.NoError(t, err)
	signature, err := hex.DecodeString(fixture.Approval.Signature)
	require.NoError(t, err)
	fixture.Approval.Signature = ""
	body, err := recoveryCanonical(fixture.Approval)
	require.NoError(t, err)
	require.True(
		t,
		ed25519.Verify(public, body, signature),
		"Python signer and Go verifier must use identical canonical bytes",
	)
	require.Equal(t, RecoveryVerificationKeyID(public), fixture.Approval.KeyID)
	fixture.Approval.Operation = "canary"
	body, err = recoveryCanonical(fixture.Approval)
	require.NoError(t, err)
	require.False(t, ed25519.Verify(public, body, signature))
}
