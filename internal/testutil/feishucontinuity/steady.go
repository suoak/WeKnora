package feishucontinuity

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"github.com/stretchr/testify/require"
)

// ActivateSteady uses synthetic signing and authority only. The actual recovery
// transitions, health gate and ordinary caller admission remain production code.
func ActivateSteady(t *testing.T, ids []string,
	facts func(context.Context, string) (feishusynccontrol.RecoveryFacts, error),
	authority func(context.Context, string, string) (feishusynccontrol.SourceManagementAuthority, error),
) {
	t.Helper()
	require.NotEmpty(t, ids)
	Activate(t, ids[0], facts)
	m := feishusynccontrol.Recovery()
	m.ConfigureSteadyAuthority(authority)
	plan, err := m.PlanSteady(context.Background(), "fixture-operator", ids, 7)
	require.NoError(t, err)
	hash, err := feishusynccontrol.RecoveryDigest(plan)
	require.NoError(t, err)
	a := feishusynccontrol.RecoveryApproval{
		Version: 2, Algorithm: "Ed25519", KeyID: plan.ApprovalKeyID,
		Incarnation: plan.Incarnation, Operation: "steady_sources", PlanSHA256: hash, ActorID: plan.ActorID,
		Reference: "synthetic-steady-test", EvidenceSHA256: strings.Repeat("e", 64), ExpiresMS: plan.ExpiresMS,
	}
	raw, err := json.Marshal(a)
	require.NoError(t, err)
	var value any
	require.NoError(t, json.Unmarshal(raw, &value))
	raw, err = json.Marshal(value)
	require.NoError(t, err)
	a.Signature = hex.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed([]byte(strings.Repeat("k", 32))), raw))
	require.NoError(t, m.ActivateSteady(context.Background(), plan, a, plan.ActorID))
}
