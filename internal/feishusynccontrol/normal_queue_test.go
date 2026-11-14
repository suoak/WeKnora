package feishusynccontrol

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalSourceQueueWaitsWithoutGrantingExecution(t *testing.T) {
	f := newNormalFixture(t)
	f.activateNormal(t)
	source := f.addSource()
	ctx := context.Background()
	require.NoError(t, f.r.Del(ctx, f.c.Guard.PermitKey()).Err())
	require.NoError(t, f.r.Set(ctx, "asynq:{approved_sync}:paused", "feishu_safety_stop", 0).Err())
	queued, err := QueueNormalSource(ctx, source.SourceID, source.ConfigSHA256)
	require.NoError(t, err)
	require.True(t, queued)
	require.Zero(t, f.r.Exists(ctx, f.c.prefix+"trip").Val())
	require.Zero(t, f.r.Exists(ctx, f.c.Guard.PermitKey()).Val())
	require.Equal(t, "feishu_safety_stop", f.r.Get(ctx, "asynq:{approved_sync}:paused").Val())
	_, _, err = BindApprovedSource(ctx, source.SourceID, source.ConfigSHA256)
	require.Error(t, err, "accepting a request must not authorize execution")

	delete(f.sources, source.SourceID)
	_, err = QueueNormalSource(ctx, source.SourceID, source.ConfigSHA256)
	require.Error(t, err, "current source permissions still apply while waiting")
}

func TestNormalSourceQueueRejectsLatchedStopWithoutChangingControlState(t *testing.T) {
	f := newNormalFixture(t)
	f.activateNormal(t)
	source := f.addSource()
	ctx := t.Context()
	require.NoError(t, f.r.Set(ctx, f.c.prefix+"trip", "feishu_response_io_failure", 0).Err())
	require.NoError(t, f.r.Del(ctx, f.c.Guard.PermitKey()).Err())
	require.NoError(t, f.r.Set(ctx, "asynq:{approved_sync}:paused", "feishu_safety_stop", 0).Err())
	queued, err := QueueNormalSource(ctx, source.SourceID, source.ConfigSHA256)
	require.True(t, queued, "the normal service owns this admission decision")
	require.ErrorIs(t, err, ErrNormalQueueStopped)
	require.Equal(t, "feishu_response_io_failure", f.r.Get(ctx, f.c.prefix+"trip").Val())
	require.Equal(t, "feishu_safety_stop", f.r.Get(ctx, "asynq:{approved_sync}:paused").Val())
	require.Zero(t, f.r.Exists(ctx, f.c.Guard.PermitKey()).Val())
	delete(f.sources, source.SourceID)
	_, err = QueueNormalSource(ctx, source.SourceID, source.ConfigSHA256)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrNormalQueueStopped, "source authority must be checked before exposing platform state")
}

func TestNormalSourceQueueRejectsUnavailableStopState(t *testing.T) {
	f := newNormalFixture(t)
	f.activateNormal(t)
	source := f.addSource()
	require.NoError(t, f.r.Close())
	queued, err := QueueNormalSource(t.Context(), source.SourceID, source.ConfigSHA256)
	require.True(t, queued)
	require.ErrorIs(t, err, ErrNormalQueueSafetyUnavailable)
}

func TestNormalSourceQueueRejectsRevokedService(t *testing.T) {
	f := newNormalFixture(t)
	f.activateNormal(t)
	source := f.addSource()
	require.NoError(t, f.m.DeactivateNormalService(t.Context()))
	_, err := QueueNormalSource(t.Context(), source.SourceID, source.ConfigSHA256)
	require.Error(t, err)
}
