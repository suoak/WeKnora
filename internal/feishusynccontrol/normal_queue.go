package feishusynccontrol

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// QueueNormalSource verifies durable service authorization and current source
// access for a pending request. It does not issue an outbound permit or require
// a healthy consumer: the worker rebinds and revalidates before any source I/O.
func QueueNormalSource(ctx context.Context, source, config string) (bool, error) {
	m := Recovery()
	if m == nil {
		return false, nil
	}
	cfg := m.normalService.Load()
	if cfg == nil || cfg.saved == nil {
		return false, nil
	}
	if !cfg.saved.Active || m.verifyNormalAuthorization(cfg.saved.Authorization, false) != nil {
		return true, ErrRecoveryRejected
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
	return true, nil
}
