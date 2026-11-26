package feishusynccontrol

import (
	"context"
	"errors"
	"strings"

	"github.com/redis/go-redis/v9"
)

// Only the exact health Lua reply qualifies, never an unknown error with a
// similar message. All reads, audit writes and the single revalidation share
// the caller's original deadline. There is no sleep or retry of a final CAS.
func isSystemConsumerReadConflict(err error) bool {
	var reply redis.Error
	return errors.As(err, &reply) && strings.TrimPrefix(reply.Error(), "ERR ") == "SYSTEM_CONSUMER_READ_CONFLICT"
}

func (s *automaticService) healthWithReadBoundaryReprobe(
	ctx context.Context, available *bool,
) (systemHealthProof, error) {
	first, err := s.health(ctx)
	if contextErr := ctx.Err(); contextErr != nil {
		return systemHealthProof{}, contextErr
	}
	if err == nil || !isSystemConsumerReadConflict(err) ||
		available == nil || !*available || first.WorkerID == "" ||
		len(first.Metadata) != 1 || len(first.ConsumerServerIDs) != 1 {
		return first, err
	}
	c := s.c
	if c.consumerDiagnostic == nil {
		return systemHealthProof{}, ErrNotReady
	}
	*available = false
	diagnostic := c.consumerDiagnostic.snapshot(ctx, c, "consumer_read_conflict_reprobe")
	if err := c.Client.XAdd(ctx, &redis.XAddArgs{
		Stream: c.prefix + "system_journal", Values: map[string]any{
			"event": "automatic_consumer_read_reprobe", "worker_id": c.Config.WorkerID,
			"incarnation": c.incarnation, "consumer_diagnostic": diagnostic,
		},
	}).Err(); err != nil {
		c.consumerDiagnostic.ProbeClass = "health_reprobe_journal_unavailable"
		return systemHealthProof{}, err
	}
	if err := c.audit(ctx, Event{
		Kind: "feishu_automatic_read_boundary", Reason: "consumer_read_conflict_reprobe",
		WorkerID: c.Config.WorkerID, ProbeStage: c.probeStage, ConsumerDiagnostic: diagnostic,
	}); err != nil {
		c.consumerDiagnostic.ProbeClass = "health_reprobe_audit_unavailable"
		return systemHealthProof{}, err
	}
	// Re-run the complete decode, unique server/subscription/active checks,
	// registry/lease/worker/membership validation and exact metadata comparison.
	// The first failed proof cannot qualify the stable window or a permit.
	second, err := s.health(ctx)
	if err != nil {
		return systemHealthProof{}, err
	}
	if err := ctx.Err(); err != nil {
		return systemHealthProof{}, err
	}
	if first.stabilityIdentity() != second.stabilityIdentity() {
		if c.consumerDiagnostic != nil {
			c.consumerDiagnostic.ProbeClass = "health_identity_changed_during_reprobe"
		}
		return systemHealthProof{}, ErrNotReady
	}
	return second, nil
}
