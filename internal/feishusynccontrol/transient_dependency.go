package feishusynccontrol

import (
	"context"
	"errors"
	"time"
)

// A monitoring timeout is not evidence of a lost consumer or a policy breach.
// Signed normal service on the dedicated lane can retry observation while its
// short-lived outbound permit stays closed. Incident rollouts remain latched.
func (c *Controller) transientNormalDependency(reason string) bool {
	if !c.Config.Automatic || !c.Config.DedicatedSyncLane || c.steadyManager == nil {
		return false
	}
	cfg := c.steadyManager.normalService.Load()
	if cfg == nil || cfg.saved == nil || !cfg.saved.Active ||
		c.steadyManager.verifyNormalAuthorization(cfg.saved.Authorization, false) != nil {
		return false
	}
	for _, stage := range []string{
		"registry", "automatic_membership_probe", "automatic_consumer_probe",
		"automatic_redis_ping", "automatic_asynq_inspection", "automatic_permit",
		"normal_service_authorization", "normal_incident_health", "automatic_audit",
	} {
		if reason == stage+"_redis_timeout" || reason == stage+"_context_deadline" {
			return true
		}
	}
	return false
}

func (c *Controller) freezeDependency(ctx context.Context, stage string, cause error, fallback string) error {
	reason := stage + "_" + failureClass(cause)
	if c.transientNormalDependency(reason) {
		return c.suspendNormalDependency(ctx, reason)
	}
	return c.freeze(ctx, fallback, true)
}

func (c *Controller) suspendNormalDependency(ctx context.Context, reason string) error {
	c.Guard.SuspendDependency(true)
	// The failed probe may already have exhausted its deadline. Withdraw through
	// the independent control pool; Redis loss also bounds permission by its TTL.
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := c.Client.Del(stopCtx, c.Guard.PermitKey()).Err()
	if c.lastEvent != "waiting:"+reason || err != nil {
		auditErr := c.audit(stopCtx, Event{
			Kind: "feishu_dependency_wait", Reason: reason, WorkerID: c.Config.WorkerID,
			ProbeStage: c.probeStage,
		})
		if err == nil && auditErr == nil {
			c.lastEvent = "waiting:" + reason
		}
		err = errors.Join(err, auditErr)
	}
	// Do not clear a trip, pause marker, execution claim, or local emergency
	// closure. A later Step must validate authorization and actual membership
	// before Lua may issue a new permit. Running jobs retain their checkpoints.
	return errors.Join(ErrNotReady, err)
}
