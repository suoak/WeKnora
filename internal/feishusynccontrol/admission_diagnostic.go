package feishusynccontrol

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/redis/go-redis/v9"
)

// AdmissionFailureReason is read-only diagnostics AFTER ordinary admission has
// failed. It cannot issue a permit, alter recovery state or authorize a source.
// Only a caller already authorized for the source should receive this category.
func AdmissionFailureReason(ctx context.Context, source, config string) string {
	m := Recovery()
	if m == nil {
		return "sync_safety_unavailable"
	}
	c := m.controller
	trip, err := c.Client.Get(ctx, c.prefix+"trip").Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return "sync_safety_unavailable"
	}
	raw, stateErr := c.Client.Get(ctx, c.prefix+"recovery").Result()
	var state recoveryMode
	validState := stateErr == nil && json.Unmarshal([]byte(raw), &state) == nil
	now, timeErr := c.Client.Time(ctx).Result()
	if timeErr != nil {
		return "sync_safety_unavailable"
	}
	if trip != "" {
		if validState && state.ExpiresMS <= now.UnixMilli() && strings.Contains(trip, "recovery_policy") {
			return "sync_policy_expired"
		}
		return "sync_platform_stopped"
	}
	if !validState {
		return "sync_policy_unavailable"
	}
	if state.ExpiresMS <= now.UnixMilli() {
		return "sync_policy_expired"
	}
	if state.Phase != "approved_sources" {
		return "sync_recovery_review_pending"
	}
	entry, ok := state.ApprovedSources[source]
	if !ok {
		return "sync_source_not_approved"
	}
	if entry.ConfigSHA256 != config {
		return "sync_source_config_changed"
	}
	permit, err := c.Client.Get(ctx, c.Guard.PermitKey()).Result()
	if err != nil || permit != c.Guard.Fingerprint() {
		return "sync_permit_unavailable"
	}
	if state.Steady != nil {
		health, err := c.Client.Get(ctx, c.prefix+"steady_health").Result()
		if err != nil || health != state.ContinuityID {
			return "sync_health_unavailable"
		}
	}
	return "sync_source_authority_changed"
}
