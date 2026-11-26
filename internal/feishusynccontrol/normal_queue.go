package feishusynccontrol

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

var (
	// ErrNormalQueueStopped rejects admission while the platform stop is latched.
	ErrNormalQueueStopped = errors.New("normal_sync_queue_platform_stopped")
	// ErrNormalQueueSafetyUnavailable means the current stop state could not be read.
	ErrNormalQueueSafetyUnavailable = errors.New("normal_sync_queue_safety_unavailable")
)

// QueueScheduledSource is only for the server's normal scheduler. It reloads
// persisted source delegation rather than treating an absent HTTP principal as
// source authority. A task still needs a durable run and a fresh worker claim.
// Legacy protocols retain their original bind/guard behavior when not handled.
func QueueScheduledSource(ctx context.Context, source, config string) (bool, error) {
	m := Recovery()
	if m == nil || m.system == nil {
		return false, nil
	}
	return m.system.queueScheduledSource(ctx, source, config)
}

// QueueNormalSource verifies durable service authorization and current source
// access for a pending request. It does not issue an outbound permit or require
// a healthy consumer: the worker rebinds and revalidates before any source I/O.
func QueueNormalSource(ctx context.Context, source, config string) (bool, error) {
	m := Recovery()
	if m == nil {
		return false, nil
	}
	if m.system != nil {
		return m.system.queueSource(ctx, source, config)
	}
	cfg := m.normalService.Load()
	if cfg == nil || cfg.saved == nil {
		return false, nil
	}
	if m.verifyNormalAuthorization(cfg.saved.Authorization, false) != nil {
		return true, ErrRecoveryRejected
	}
	if !cfg.saved.Active {
		// No installation-wide admission. The caller must independently bind
		// the exact signed incident/steady-source policy before enqueuing.
		if observing, err := m.observationBarrier(ctx); err != nil || !observing {
			return true, ErrRecoveryRejected
		}
		return false, nil
	}
	if observing, err := m.observationBarrier(ctx); err != nil || observing {
		if err != nil {
			return true, fmt.Errorf("%w: %w", ErrNormalQueueSafetyUnavailable, err)
		}
		// The caller must bind the exact signed incident/source policy before
		// it can create a SyncLog or enqueue. This grants no normal admission.
		return false, nil
	}
	if _, err := uuid.Parse(source); err != nil || !recoverySHA.MatchString(config) {
		return true, ErrRecoveryRejected
	}
	authority, err := cfg.source(ctx, nil, source, config)
	if err != nil {
		return true, fmt.Errorf("normal_queue_source: %w", err)
	}
	if authority.SourceID != source || authority.ConfigSHA256 != config ||
		authority.TenantID == 0 || authority.KBID == "" || !recoverySHA.MatchString(authority.ExecutionSHA256) {
		return true, ErrRecoveryRejected
	}
	// A consumer may be temporarily paused without a permit during recovery.
	// A latched stop is different: the controller fences every running SyncLog,
	// so accepting a new one would immediately report a misleading cancellation.
	stopped, err := m.controller.Client.Exists(ctx, m.controller.prefix+"trip").Result()
	if err != nil {
		return true, fmt.Errorf("%w: %w", ErrNormalQueueSafetyUnavailable, err)
	}
	if stopped != 0 {
		return true, ErrNormalQueueStopped
	}
	return true, nil
}
