package feishusynccontrol

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// step is called only by the controller's serialized, bounded Step. Ordinary
// service does not call any signed recovery operation. It does not reinterpret
// an operator/integrity stop as a temporary consumer-read problem.
func (s *automaticService) step(ctx context.Context) error {
	c := s.c
	if err := ctx.Err(); err != nil {
		s.stable = stableServiceHealth{}
		return s.withdraw(ctx, "controller_context_unavailable")
	}
	_, err := register.Run(ctx, c.Client, []string{
		c.prefix + "workers", c.prefix + "leases", c.prefix + "joining",
		c.prefix + "admitted", c.prefix + "membership_revision",
	}, c.Config.WorkerID, c.fingerprint+":"+c.incarnation).Result()
	if err != nil {
		s.stable = stableServiceHealth{}
		if strings.Contains(err.Error(), "CONFLICT") {
			return c.freeze(ctx, "system_worker_identity_conflict", true)
		}
		return s.withdraw(ctx, "registry_unavailable")
	}
	proof, err := s.health(ctx)
	stable := s.stable.observe(time.Now(), proof, err)
	if err != nil {
		if s.missingSince.IsZero() {
			s.missingSince = time.Now()
		}
		// Absence alone is NOT evidence that a process exited. Stay outbound
		// closed for as long as health cannot be established, rather than
		// inventing a confirmed exit after an arbitrary elapsed timeout.
		return s.withdraw(ctx, "consumer_unavailable")
	}
	if !s.missingSince.IsZero() && time.Since(s.missingSince) >= time.Second {
		s.needsCanary = true
	}
	s.missingSince = time.Time{}
	if !stable {
		return s.withdraw(ctx, "waiting_for_stable_identity")
	}
	trip, err := systemRead(ctx, c.Client, c.prefix+"trip")
	if err != nil {
		s.stable = stableServiceHealth{}
		return s.withdraw(ctx, "stop_state_unavailable")
	}
	if trip != "" && !automaticServiceRecoverableTrip(trip) {
		return s.withdraw(ctx, "hard_stop_preserved")
	}
	raw, err := systemRead(ctx, c.Client, c.prefix+"system_service")
	if err != nil {
		return s.withdraw(ctx, "system_state_unavailable")
	}
	var state systemServiceState
	if raw != "" {
		state, err = systemState(raw)
		if err != nil {
			return s.withdraw(ctx, "system_state_invalid")
		}
		if state.WorkerValue == proof.WorkerValue && state.ConsumerServerID != proof.ConsumerServerIDs[0] {
			// A replacement consumer in the same worker incarnation is a real
			// identity drift, not a recovered heartbeat of the original server.
			return c.freeze(ctx, "system_consumer_identity_conflict", true)
		}
	}
	// A Redis commit followed by a failed SQL acknowledgment has a valid root
	// but no executable authority. Reconcile its immutable receipt explicitly;
	// otherwise current() would reject prepared forever without revisiting CAS.
	receipt, err := readSystemReceipt(ctx, s.db)
	if err != nil {
		return s.withdraw(ctx, "durable_epoch_unavailable")
	}
	prepared := receipt != nil && receipt.Phase == "prepared"
	if raw == "" || state.WorkerValue != proof.WorkerValue || state.RuntimeSHA != c.Config.SourceSHA ||
		state.MembershipRevision != proof.MembershipRevision ||
		trip != "" || s.needsCanary || prepared {
		c.Guard.SuspendDependency(true)
		if err := c.Client.Del(ctx, c.Guard.PermitKey()).Err(); err != nil {
			return s.withdraw(ctx, "permit_withdrawal_unavailable")
		}
		if err := s.migrate(ctx, proof, raw, trip); err != nil {
			return errors.Join(ErrNotReady, err)
		}
		s.needsCanary = false
	}
	state, raw, err = s.current(ctx)
	if err != nil {
		return s.withdraw(ctx, "durable_epoch_unavailable")
	}
	if time.Since(c.lastAlertAck) >= time.Second {
		if err := c.audit(ctx, Event{Kind: "feishu_safety_heartbeat", Reason: "automatic_service_alive", WorkerID: c.Config.WorkerID}); err != nil {
			return s.withdraw(ctx, "audit_unavailable")
		}
		c.lastAlertAck = time.Now()
	}
	if state.Phase == "canary" && state.CanaryRunID != "" && time.Since(s.lastCanaryRead) >= time.Second {
		s.lastCanaryRead = time.Now()
		if err := s.advanceCanary(ctx, state, raw); err != nil {
			return s.withdraw(ctx, "canary_receipt_unavailable")
		}
		state, raw, err = s.current(ctx)
		if err != nil {
			return s.withdraw(ctx, "canary_state_unavailable")
		}
	}
	metadataKey := "asynq:servers:{" + proof.WorkerID + ":" + proof.ConsumerServerIDs[0] + "}"
	state.ConsumerMetadataSHA1 = proof.Metadata[metadataKey]
	updated, err := json.Marshal(state)
	if err != nil {
		return err
	}
	args, err := systemProofArgs(proof)
	if err != nil {
		return err
	}
	args = append(args, raw, string(updated), c.Guard.Fingerprint())
	keys := append(s.healthKeys(), c.prefix+"system_service", c.prefix+"trip", c.Guard.PermitKey(),
		c.prefix+"system_journal", "asynq:{approved_sync}:paused", "asynq:{sync}:paused")
	err = renewSystemService.Run(ctx, c.Client, keys, args...).Err()
	if err != nil {
		return s.withdraw(ctx, "runtime_or_queue_changed")
	}
	c.admitted = true
	c.Guard.SetProviderFailureIsolation(true)
	c.lastEvent = ""
	return nil
}

var renewSystemService = redis.NewScript(systemHealthLua + `
if redis.call('GET',KEYS[6])~=ARGV[5] or redis.call('EXISTS',KEYS[7])~=0 then
 return redis.error_reply('SYSTEM_STATE_CHANGED') end
local s=cjson.decode(ARGV[6])
if s.worker_id~=ARGV[1] or s.worker_value~=ARGV[2] or s.membership_revision~=ARGV[3] or
 (s.phase~='canary' and s.phase~='ramp' and s.phase~='normal') then return redis.error_reply('SYSTEM_PHASE_CLOSED') end
local owned={feishu_safety_stop=true,invalid_feishu_safety_configuration=true,
 approved_sync_frozen=true,feishu_recovery_canary=true}
local approved=redis.call('GET',KEYS[10])
if approved and not owned[approved] then return redis.error_reply('SYSTEM_OPERATOR_QUEUE_PAUSE') end
for _,key in ipairs({KEYS[10],KEYS[11]}) do
 local marker=redis.call('GET',key)
 if marker and owned[marker] then
  redis.call('XADD',KEYS[9],'*','event','automatic_service_queue_resumed','epoch',s.epoch,'marker',marker)
  redis.call('DEL',key)
 end
end
if s.phase=='ramp' and type(s.ramp_until_ms)=='number' and now>=s.ramp_until_ms then
 s.phase='normal'
 redis.call('XADD',KEYS[9],'*','event','automatic_service_normal','epoch',s.epoch)
end
redis.call('SET',KEYS[6],cjson.encode(s))
redis.call('SET',KEYS[8],ARGV[7],'PX',750)
return 1`)
