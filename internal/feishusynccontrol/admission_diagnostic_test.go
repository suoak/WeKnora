package feishusynccontrol

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSteadyAdmissionDiagnosticsDoNotAuthorizeOrMutateState(t *testing.T) {
	f := newSteadyFixture(t)
	ctx := context.Background()
	f.activate(t)
	raw := f.r.Get(ctx, f.c.prefix+"recovery").Val()
	require.Equal(t, "sync_source_not_approved", AdmissionFailureReason(ctx, uuid.NewString(), ""))
	require.Equal(t, "sync_source_config_changed", AdmissionFailureReason(ctx, f.ids[0], "changed"))
	require.Equal(t, raw, f.r.Get(ctx, f.c.prefix+"recovery").Val())
	require.NoError(t, f.r.Del(ctx, f.c.Guard.PermitKey()).Err())
	require.Equal(t, "sync_permit_unavailable", AdmissionFailureReason(ctx, f.ids[0], f.facts[f.ids[0]].ConfigSHA256))
	require.NoError(t, f.r.Set(ctx, f.c.prefix+"trip", "operator_emergency_stop", 0).Err())
	require.Equal(t, "sync_platform_stopped", AdmissionFailureReason(ctx, f.ids[0], f.facts[f.ids[0]].ConfigSHA256))
}
