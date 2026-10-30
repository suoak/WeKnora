// Package feishucontinuity drives real recovery transitions with synthetic
// approvals and an owned in-memory Redis. It never installs production secrets.
package feishucontinuity

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// Activate establishes reset -> one canary -> separate signed source approval.
// The caller owns the authoritative facts function, including revocation cases.
func Activate(t *testing.T, id string, facts func(context.Context, string) (feishusynccontrol.RecoveryFacts, error)) {
	t.Helper()
	t.Setenv("FEISHU_SYNC_SAFETY_ENABLED", "true")
	t.Setenv("FEISHU_APPROVED_SYNC_LANE_ENABLED", "1")
	t.Setenv("FEISHU_LEGACY_SYNC_SUSPENDED", "false")
	t.Setenv("FEISHU_LEGACY_SYNC_ALLOWED_SOURCE_IDS", "all")
	ctx := context.Background()
	r := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() {
		feishusynccontrol.InstallRecovery(nil)
		feishusafety.InstallDefault(nil)
		_ = r.Close()
	})
	policy := feishusafety.DefaultConfig()
	policy.Namespace = "continuity-fixture-" + uuid.NewString()
	g, err := feishusafety.New(r, policy)
	require.NoError(t, err)
	c, err := feishusynccontrol.New(r, g, feishusynccontrol.Config{
		Automatic: true, WorkerID: "fixture:1", SourceSHA: strings.Repeat("a", 40),
		Scope: "all", Maintenance: 4,
	}, func(context.Context) error { return nil }, func(feishusynccontrol.Event) error { return nil })
	require.NoError(t, err)
	c.Consumers = func(context.Context) ([]string, error) { return []string{"fixture:1"}, nil }
	c.ApprovedConsumers = c.Consumers
	require.NoError(t, c.Step(ctx))
	feishusafety.InstallDefault(g)
	require.NoError(t, g.Trip(ctx, "synthetic_incident"))
	require.NoError(t, r.Set(ctx, "asynq:{sync}:paused", "synthetic_stop", 0).Err())
	require.NoError(t, r.Set(ctx, "asynq:{approved_sync}:paused", "synthetic_stop", 0).Err())
	key := ed25519.NewKeyFromSeed([]byte(strings.Repeat("k", 32)))
	m, err := feishusynccontrol.NewRecoveryManager(c, key.Public().(ed25519.PublicKey), facts)
	require.NoError(t, err)
	feishusynccontrol.InstallRecovery(m)
	p, err := m.Plan(ctx, id, "fixture-operator")
	require.NoError(t, err)
	sign := func(operation, incarnation, hash string, expires int64) feishusynccontrol.RecoveryApproval {
		a := feishusynccontrol.RecoveryApproval{
			Version: 2, Algorithm: "Ed25519", KeyID: p.ApprovalKeyID, Incarnation: incarnation,
			Operation: operation, PlanSHA256: hash, ActorID: "fixture-operator",
			Reference: "synthetic-unit-review", EvidenceSHA256: strings.Repeat("e", 64), ExpiresMS: expires,
		}
		raw, marshalErr := json.Marshal(a)
		require.NoError(t, marshalErr)
		var canonical any
		require.NoError(t, json.Unmarshal(raw, &canonical))
		raw, marshalErr = json.Marshal(canonical)
		require.NoError(t, marshalErr)
		a.Signature = hex.EncodeToString(ed25519.Sign(key, raw))
		return a
	}
	hash, err := feishusynccontrol.RecoveryDigest(p)
	require.NoError(t, err)
	require.NoError(t, m.Reset(ctx, p, sign("reset", p.Incarnation, hash, p.ExpiresMS), "fixture-operator"))
	require.NoError(t, c.Step(ctx))
	status, err := m.Status(ctx, id)
	require.NoError(t, err)
	raw, err := json.Marshal(status["state"])
	require.NoError(t, err)
	var state struct {
		Incarnation string `json:"incarnation"`
	}
	require.NoError(t, json.Unmarshal(raw, &state))
	require.NoError(t, m.Canary(ctx, p, sign("canary", state.Incarnation, hash, p.ExpiresMS),
		"fixture-operator", func(_ context.Context, bind func(string, int64) error) error {
			return bind(uuid.NewString(), 1)
		}))
	cp, err := m.PlanContinuity(ctx, "fixture-operator", []string{id})
	require.NoError(t, err)
	hash, err = feishusynccontrol.RecoveryDigest(cp)
	require.NoError(t, err)
	require.NoError(t, m.ApproveSources(ctx, cp,
		sign("approved_sources", cp.Incarnation, hash, time.Now().Add(5*time.Minute).UnixMilli()), "fixture-operator"))
}
