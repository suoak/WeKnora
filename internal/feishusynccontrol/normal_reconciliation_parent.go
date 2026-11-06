package feishusynccontrol

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

// A live normal-service root can sponsor a NEW independently signed incident
// plan after its old canary review expires or its worker is replaced. This
// issues no source/SQL execution authority, renews no old approval and never
// trips or pauses healthy normal synchronization.
func (m *RecoveryManager) normalReconciliationParent(ctx context.Context) (*NormalServiceRecord, string, error) {
	record, err := m.normalRecord(ctx)
	if err != nil || record == nil {
		return nil, "", ErrRecoveryRejected
	}
	raw, err := m.controller.Client.Get(ctx, m.controller.prefix+"normal_service").Result()
	var stored NormalServiceRecord
	if err != nil || json.Unmarshal([]byte(raw), &stored) != nil || stored != *record {
		return nil, "", ErrRecoveryRejected
	}
	mode, err := m.controller.Client.Get(ctx, m.controller.prefix+"recovery").Result()
	var state recoveryMode
	if err != nil || json.Unmarshal([]byte(mode), &state) != nil ||
		state.RunID != "" || state.Generation != 0 || !recoverySHA.MatchString(state.PlanSHA256) ||
		(state.Phase != "awaiting_review" && state.Phase != "approved_sources") ||
		(state.Version != 1 && state.Version != 2) ||
		(state.Version == 1 && (state.Steady != nil || state.Phase != "awaiting_review")) ||
		(state.Version == 2 && (state.Steady == nil || state.Steady.Plan.ReleaseSharedQueue)) {
		return nil, "", ErrRecoveryRejected
	}
	required, err := m.controller.Client.Get(ctx, m.controller.prefix+"recovery_required").Result()
	if err != nil || required != state.PlanSHA256 {
		return nil, "", ErrRecoveryRejected
	}
	return record, raw, nil
}

func validSteadyParentVersion(p SteadyPlan) bool {
	if p.Version == 1 {
		return p.NormalServiceAuthorizationID == ""
	}
	if p.Version != 2 || p.NormalServiceAuthorizationID == "" || p.ReleaseSharedQueue || p.RevocationOnly {
		return false
	}
	if _, err := uuid.Parse(p.NormalServiceAuthorizationID); err != nil {
		return false
	}
	for _, source := range p.ManagedSources {
		if !source.Reconciliation {
			return false
		}
	}
	return len(p.ManagedSources) > 0
}

func (m *RecoveryManager) checkSteadyParent(ctx context.Context, p SteadyPlan) (string, error) {
	if !validSteadyParentVersion(p) {
		return "", ErrRecoveryRejected
	}
	if p.Version == 1 {
		return "", m.controller.checkRecoveryPolicy(ctx)
	}
	record, raw, err := m.normalReconciliationParent(ctx)
	if err != nil || record.Authorization.ID != p.NormalServiceAuthorizationID {
		return "", ErrRecoveryRejected
	}
	return raw, nil
}
