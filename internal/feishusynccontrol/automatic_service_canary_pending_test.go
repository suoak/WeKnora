package feishusynccontrol

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// No model, remote source, production database or fabricated successful
// publication. A real claimed run retains its source
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
	previousPoll := s.lastCanaryRead
	err = s.c.Step(probe)
	require.NoError(t, err, "a still-claimed pending Canary must not wait behind its own source fence")
	require.True(t, s.lastCanaryRead.After(previousPoll), "must execute the actual Canary poll")
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

func TestAutomaticServiceNativeSQLPendingCanaryQueryFailureStaysClosed(t *testing.T) {
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
	_, handled, err := PrepareAutomaticSourceExecution(ctx, f.source.ID, log.ID, f.tenant.ID)
	require.NoError(t, err)
	require.True(t, handled)
	var injected atomic.Bool
	callback := "test:pending_query_failure_" + uuid.NewString()
	require.NoError(t, f.db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if strings.Contains(tx.Statement.SQL.String(), "s.execution_claimed") &&
			strings.Contains(tx.Statement.SQL.String(), "JOIN data_sources") {
			injected.Store(true)
			require.ErrorContains(t, tx.AddError(errors.New("controlled SQL failure with private_token_do_not_log")),
				"controlled SQL failure")
		}
	}))
	t.Cleanup(func() { require.NoError(t, f.db.Callback().Query().Remove(callback)) })
	require.Error(t, s.c.Step(ctx))
	require.True(t, injected.Load(), "must exercise the new non-locking observation")
	require.True(t, s.c.Guard.DependencySuspended())
	require.Zero(t, r.Exists(ctx, s.c.Guard.PermitKey()).Val())
	journal, err := r.XRange(ctx, s.c.prefix+"system_journal", "-", "+").Result()
	require.NoError(t, err)
	last := journal[len(journal)-1].Values["consumer_diagnostic"].(string)
	require.Contains(t, last, "canary_internal_error")
	require.NotContains(t, last, "private_token_do_not_log")
	state, _, err := s.current(ctx)
	require.NoError(t, err)
	require.Equal(t, "canary", state.Phase)
	require.Empty(t, state.CanaryProofSHA256)
}

func TestAutomaticServiceNativeSQLDeletedPendingSourceStillRejected(t *testing.T) {
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
	require.NoError(t, f.db.Model(&types.DataSource{}).Where("id=?", f.source.ID).
		Update("deleted_at", time.Now().UTC()).Error)
	require.NoError(t, s.c.Step(ctx), "missing join must fall through to the original failed-source decision")
	state, _, err := s.current(ctx)
	require.NoError(t, err)
	require.Equal(t, "canary", state.Phase)
	require.Empty(t, state.CanarySourceID)
	require.Empty(t, state.CanaryProofSHA256)
	require.Error(t, s.c.Guard.Check(feishusafety.WithScope(work, f.source.ID)))
	var retained types.SyncLog
	require.NoError(t, f.db.First(&retained, "id=?", log.ID).Error)
	require.Equal(t, types.SyncLogStatusRunning, retained.Status, "observation does not sign off or delete the run")
	require.Zero(t, r.Exists(ctx, s.c.prefix+"trip").Val(), "one deleted source is not a namespace-wide failure")
}
