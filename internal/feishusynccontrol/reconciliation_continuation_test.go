package feishusynccontrol

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/stretchr/testify/require"
)

func TestSignedReconciliationContinuesOnlyAfterVerifiedStateTransition(t *testing.T) {
	f, _, id := reconciliationFixture(t)
	facts := f.facts[id]
	facts.NormalConfigSHA256 = strings.Repeat("a", 64)
	facts.NormalFactsSHA256 = strings.Repeat("b", 64)
	f.facts[id] = facts
	authority := f.authority[id]
	authority.NormalAuthorityHash = strings.Repeat("c", 64)
	f.authority[id] = authority
	p, err := f.m.PlanSteadyWithReconciliation(context.Background(), "operator", f.ids[1:], []string{id}, 7)
	require.NoError(t, err)
	require.NoError(t, f.m.ActivateSteady(context.Background(), p, f.sign(t, p), "operator"))
	_, _, err = BindApprovedSource(context.Background(), id, facts.ConfigSHA256)
	require.Error(t, err, "signed recovery still cannot grant normal sync while held")

	// The database's fenced full traversal is the sole writer of this state.
	facts.ConfigSHA256, facts.FactsSHA256 = facts.NormalConfigSHA256, facts.NormalFactsSHA256
	facts.Eligible, facts.ReconciliationEligible = true, false
	f.facts[id] = facts
	authority.AccessState, authority.SourceStatus = "active", "active"
	authority.AuthorityHash = authority.NormalAuthorityHash
	f.authority[id] = authority
	work, bound, err := BindApprovedSource(context.Background(), id, facts.ConfigSHA256)
	require.NoError(t, err)
	require.True(t, bound, "the same signed exact source automatically continues normal scheduling")
	work = feishusafety.WithScope(work, id)
	require.NoError(t, f.c.Guard.Check(work), "the atomic Redis gate must accept the signed normal fingerprint")
	_, release, err := feishusafety.ReserveEmbedding(work, []string{"verified resumed source"})
	require.NoError(t, err, "the same exact normal identity must pass the atomic model budget gate")
	release()

	for _, mutation := range []string{"paused", "revoked", "hold", "membership", "config"} {
		t.Run(mutation, func(t *testing.T) {
			changedFacts, changedAuthority := facts, authority
			switch mutation {
			case "paused":
				changedFacts.Eligible, changedFacts.RetryEligible = false, false
				changedAuthority.SourceStatus = "paused"
			case "revoked", "hold":
				changedFacts.Eligible, changedFacts.RetryEligible = false, false
				changedAuthority.AccessState = mutation
			case "membership":
				changedAuthority.AuthorityHash = strings.Repeat("d", 64)
			case "config":
				changedFacts.ConfigSHA256 = strings.Repeat("e", 64)
			}
			f.facts[id], f.authority[id] = changedFacts, changedAuthority
			_, _, err := BindApprovedSource(context.Background(), id, changedFacts.ConfigSHA256)
			require.Error(t, err)
			f.facts[id], f.authority[id] = facts, authority
		})
	}
}
