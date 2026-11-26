package feishusynccontrol

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Exploratory isolated test only: no model, remote source, production database
// or fabricated successful publication. A real claimed run retains its source
// row fence while the controller maintains the actual consumer/worker lease.
func TestAutomaticServiceNativeSQLPendingCanaryDoesNotWaitOnLiveSourceFence(t *testing.T) {
	f, s, r := renewableSQLService(t)
	ctx := context.Background()
	logs := repository.NewSyncLogRepository(f.db)
	log := &types.SyncLog{
		ID: uuid.NewString(), DataSourceID: f.source.ID,
		TenantID: f.tenant.ID, Status: types.SyncLogStatusRunning, StartedAt: time.Now().UTC(),
	}
	created, err := logs.CreateIfNoRunning(ctx, log)
	require.NoError(t, err)
	require.True(t, created)
	work, handled, err := PrepareAutomaticSourceExecution(ctx, f.source.ID, log.ID, f.tenant.ID)
	require.NoError(t, err)
	require.True(t, handled)
	owner, claimed, err := logs.(interfaces.SyncExecutionRepository).
		ClaimExecution(work, f.source.ID, log.ID, f.tenant.ID)
	require.NoError(t, err)
	require.True(t, claimed)
	locked := f.db.WithContext(ctx).Begin()
	require.NoError(t, locked.Error)
	t.Cleanup(func() { require.NoError(t, locked.Rollback().Error) })
	var source string
	require.NoError(t, locked.Raw("SELECT id FROM data_sources WHERE id=? FOR UPDATE", f.source.ID).Scan(&source).Error)
	require.Equal(t, f.source.ID, source)
	probe, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	err = s.c.Step(probe)
	require.NoError(t, err, "a still-claimed pending Canary must not wait behind its own source fence")
	require.Equal(t, s.c.Guard.Fingerprint(), r.Get(ctx, s.c.Guard.PermitKey()).Val())
	state, _, err := s.current(ctx)
	require.NoError(t, err)
	require.Equal(t, "canary", state.Phase, "pending is never a publication success")
	require.Empty(t, state.CanaryProofSHA256)
	var current types.SyncLog
	require.NoError(t, f.db.First(&current, "id=?", log.ID).Error)
	require.Equal(t, types.SyncLogStatusRunning, current.Status)
	require.True(t, current.ExecutionClaimed)
	require.Equal(t, owner.Generation, current.ExecutionGeneration)
}
