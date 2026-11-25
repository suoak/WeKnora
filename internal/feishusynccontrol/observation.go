package feishusynccontrol

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

// observationBarrier selects the signed incident/source policy instead of the
// independently activated installation-wide normal-service authorization. The
// non-expiring marker survives a missing/tampered recovery record; reads are
// atomic and have no Inspector/cleanup side effects.
var readObservation = redis.NewScript(`return {
 redis.call('GET',KEYS[1]) or false,
 redis.call('GET',KEYS[2]) or false,
 redis.call('GET',KEYS[3]) or false}`)

func (m *RecoveryManager) observationBarrier(ctx context.Context) (bool, error) {
	values, err := readObservation.Run(ctx, m.controller.Client, []string{
		m.controller.prefix + "observation_required",
		m.controller.prefix + "recovery",
		m.controller.prefix + "recovery_required",
	}).Slice()
	if err != nil {
		return true, err
	}
	marker, _ := values[0].(string)
	raw, _ := values[1].(string)
	if raw == "" {
		if marker != "" || values[2] != nil {
			return true, ErrRecoveryRejected
		}
		return false, nil
	}
	var state recoveryMode
	if len(raw) > 1<<20 || json.Unmarshal([]byte(raw), &state) != nil {
		return true, ErrRecoveryRejected
	}
	if values[0] == nil && state.ObservationPlanSHA256 == "" {
		return false, nil // Older ordinary-service lifecycle, not a new recovery.
	}
	if !recoverySHA.MatchString(marker) || state.ObservationPlanSHA256 != marker {
		return true, ErrRecoveryRejected
	}
	validPhase := state.Version == 1 && state.Steady == nil &&
		(state.Phase == "frozen" || state.Phase == "admit" ||
			state.Phase == "running" || state.Phase == "awaiting_review")
	validPhase = validPhase || (state.Version == 2 && state.Steady != nil && state.Phase == "approved_sources")
	if !validPhase || !recoverySHA.MatchString(state.ObservationRootSHA256) {
		return true, ErrRecoveryRejected
	}
	root, err := m.inactiveNormalRecord(ctx)
	if err != nil || digestBytes(root) != state.ObservationRootSHA256 {
		return true, ErrRecoveryRejected
	}
	return true, nil
}

// Require an existing, durably revoked grant, not a Redis-only override of an
// active installation-wide grant. Older binaries already reject Active=false;
// rolling back cannot silently turn single-source observation into all-source
// admission. Revocation uses the existing administrator workflow separately.
func (m *RecoveryManager) inactiveNormalRecord(ctx context.Context) (string, error) {
	if _, err := m.normalRecord(ctx); err != nil {
		return "", err
	}
	cfg := m.normalService.Load()
	if cfg == nil || cfg.saved == nil || cfg.saved.Active {
		return "", ErrRecoveryRejected
	}
	raw, err := m.controller.Client.Get(ctx, m.controller.prefix+"normal_service").Result()
	var saved NormalServiceRecord
	if err != nil || json.Unmarshal([]byte(raw), &saved) != nil || saved != *cfg.saved {
		return "", ErrRecoveryRejected
	}
	return raw, nil
}

func (m *RecoveryManager) requireObservationPlan(ctx context.Context, p RecoveryPlan) error {
	observing, err := m.observationBarrier(ctx)
	if err != nil {
		return err
	}
	if observing && !p.HoldForReview {
		return ErrRecoveryRejected
	}
	if cfg := m.normalService.Load(); cfg != nil && cfg.saved != nil &&
		!cfg.saved.Active && m.controller.Config.DedicatedSyncLane && !p.HoldForReview {
		return ErrRecoveryRejected
	}
	if p.HoldForReview {
		root, err := m.inactiveNormalRecord(ctx)
		q, queueErr := m.incidentQueue(ctx)
		if err != nil || p.QueueName != ApprovedSyncQueue || p.QueuePendingCount != 0 ||
			digestBytes(root) != p.NormalServiceSHA256 || queueErr != nil ||
			q.pause != dedicatedRecoveryPause || !recoveryQueueMatches(q, p) {
			return ErrRecoveryRejected
		}
	}
	return nil
}

func (m *RecoveryManager) bindObservationPlan(ctx context.Context, p *RecoveryPlan) error {
	cfg := m.normalService.Load()
	if p.QueueName != ApprovedSyncQueue || cfg == nil || cfg.saved == nil || cfg.saved.Active {
		return nil // Preserve the separately authorized legacy service lifecycle.
	}
	root, err := m.inactiveNormalRecord(ctx)
	q, queueErr := m.incidentQueue(ctx)
	if err != nil || p.QueuePendingCount != 0 || queueErr != nil || q.pause != dedicatedRecoveryPause {
		return ErrRecoveryRejected
	}
	p.HoldForReview, p.NormalServiceSHA256 = true, digestBytes(root)
	return nil
}

// Only an empty dedicated lane may enter observation. Once the second signed
// steady policy releases it, retain that signed EMPTY checkpoint rather than
// interpreting new live jobs as historical work. No shared queue is resumed or
// checkpointed by this path, and archived tasks remain untouched.
func (m *RecoveryManager) observationQueue(ctx context.Context) (recoveryQueueSnapshot, bool, error) {
	var empty recoveryQueueSnapshot
	observing, err := m.observationBarrier(ctx)
	if err != nil || !observing {
		return empty, observing, err
	}
	raw, err := m.controller.Client.Get(ctx, m.controller.prefix+"recovery").Result()
	var state recoveryMode
	if err != nil || json.Unmarshal([]byte(raw), &state) != nil ||
		state.QueueName != ApprovedSyncQueue || state.ObservationPlanSHA256 == "" {
		return empty, true, ErrRecoveryRejected
	}
	if state.Steady == nil {
		q, err := m.incidentQueue(ctx)
		if err != nil || len(q.ids) != 0 {
			return empty, true, ErrRecoveryRejected
		}
		return q, true, nil
	}
	q := recoveryQueueSnapshot{name: ApprovedSyncQueue, pause: dedicatedRecoveryPause, ids: []string{}}
	q.digest, err = RecoveryDigest([]any{q.name, digestBytes(q.pause), q.ids, []map[string]string(nil)})
	p := state.Steady.Plan
	if err != nil || m.verifySteady(*state.Steady, time.Now().UnixMilli(), false) != nil ||
		p.ReleaseSharedQueue || p.ObservationPlanSHA256 != state.ObservationPlanSHA256 ||
		p.RuntimeSHA != m.controller.Config.SourceSHA || p.WorkerID != m.controller.Config.WorkerID ||
		p.Incarnation != m.controller.incarnation || p.RecoveryPlanSHA256 != state.PlanSHA256 ||
		p.HistoricalPending != 0 || p.HistoricalQueueSHA256 != q.digest ||
		state.HistoricalQueueSHA256 != q.digest || state.HistoricalPending != 0 {
		return empty, true, ErrRecoveryRejected
	}
	return q, true, nil
}
