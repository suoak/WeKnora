package feishusynccontrol

import (
	"context"
	"encoding/json"
)

// RecoveryPrepareRequest pins a restrictive queue freeze to an observed incident.
// It neither clears the stop nor replaces the separate signed reset/canary.
type RecoveryPrepareRequest struct {
	HoldForReview bool   `json:"hold_for_review,omitempty"`
	IncidentID    string `json:"incident_id"`
	RuntimeSHA    string `json:"runtime_sha"`
	Trip          string `json:"trip"`
}

// Prepare freezes the observed, drained dedicated lane without issuing authority.
func (m *RecoveryManager) Prepare(
	ctx context.Context, source, actor string, expected RecoveryPrepareRequest,
) (RecoveryPlan, error) {
	var empty RecoveryPlan
	if !m.operation.TryLock() {
		return empty, ErrRecoveryRejected
	}
	defer m.operation.Unlock()
	c := m.controller
	if !c.Config.DedicatedSyncLane || actor == "" || expected.RuntimeSHA != c.Config.SourceSHA ||
		expected.IncidentID == "" || expected.Trip == "" {
		return empty, ErrRecoveryRejected
	}
	if expected.HoldForReview {
		if _, err := m.inactiveNormalRecord(ctx); err != nil {
			return empty, ErrRecoveryRejected
		}
	}
	facts, err := m.facts(ctx, source)
	if err != nil || !facts.Eligible || facts.SourceID != source || facts.SelectedResourceCount < 1 ||
		facts.SelectedResourceCount > 64 || facts.LiveExecutions != 0 || facts.LivePublications != 0 {
		return empty, ErrRecoveryRejected
	}
	record, err := m.normalRecord(ctx)
	if err != nil {
		return empty, ErrRecoveryRejected
	}
	var raw []byte
	if record == nil {
		inactive, readErr := m.inactiveNormalRecord(ctx)
		if readErr != nil {
			return empty, readErr
		}
		raw = []byte(inactive)
	} else {
		raw, err = json.Marshal(record)
	}
	if err != nil {
		return empty, err
	}
	err = c.Client.Eval(ctx, `
if redis.call('GET',KEYS[1])~=ARGV[2] or redis.call('EXISTS',KEYS[2])~=0 or
 redis.call('HLEN',KEYS[3])~=0 or redis.call('GET',KEYS[4])~=ARGV[1] then return
redis.error_reply('RECOVERY_PREPARE_CHANGED') end
local marker=redis.call('GET',KEYS[5])
if marker and marker~='feishu_safety_stop' and marker~=ARGV[8] then return
redis.error_reply('RECOVERY_PAUSE_NOT_OWNED') end
if redis.call('LLEN',KEYS[6])~=0 or redis.call('ZCARD',KEYS[7])~=0 or
 redis.call('ZCARD',KEYS[8])~=0 then return redis.error_reply('RECOVERY_LANE_NOT_DRAINED') end
local event=redis.call('XREVRANGE',KEYS[9],'+','-','COUNT',1)
if #event~=1 or event[1][1]~=ARGV[3] or redis.call('HLEN',KEYS[11])~=1 or
 redis.call('HGET',KEYS[11],ARGV[4])~=ARGV[5] then return
redis.error_reply('RECOVERY_PREPARE_CHANGED') end
redis.call('XADD',KEYS[10],'MAXLEN','~',10000,'*','event','reviewed_recovery_prepared',
 'incident_id',ARGV[3],'actor_id',ARGV[6],'source_id',ARGV[7],'queue','approved_sync')
redis.call('SET',KEYS[5],ARGV[8]);return 1`, []string{
		c.prefix + "trip", c.Guard.PermitKey(), c.prefix + "inflight", c.prefix + "normal_service",
		"asynq:{approved_sync}:paused", "asynq:{approved_sync}:active", "asynq:{approved_sync}:retry",
		"asynq:{approved_sync}:scheduled", c.prefix + "audit", c.prefix + "control_audit", c.prefix + "workers",
	}, string(raw), expected.Trip, expected.IncidentID, c.Config.WorkerID,
		c.fingerprint+":"+c.incarnation, actor, source, dedicatedRecoveryPause).Err()
	if err != nil {
		return empty, ErrRecoveryRejected
	}
	// Bind the exact queue bytes, current source facts and live worker topology
	// after freezing. Ordinary plan/status remain read-only.
	return m.Plan(ctx, source, actor)
}
