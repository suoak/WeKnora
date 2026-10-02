package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSyncExecutionSQLite(t *testing.T)   { syncExecutionSuite(t, "sqlite") }
func TestSyncExecutionPostgres(t *testing.T) { syncExecutionSuite(t, "postgres") }

func syncExecutionSuite(t *testing.T, dialect string) {
	db := syncdb.Open(t, dialect)
	r := &SyncLogRepository{db: db}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	makeDS := func() *types.DataSource {
		ds := &types.DataSource{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb", Name: "sync", Type: types.ConnectorTypeFeishu, Status: types.DataSourceStatusActive}
		require.NoError(t, db.Create(ds).Error)
		return ds
	}
	admit := func(ds *types.DataSource) *types.SyncLog {
		log := &types.SyncLog{DataSourceID: ds.ID, TenantID: ds.TenantID, Status: types.SyncLogStatusRunning}
		ok, err := r.CreateIfNoRunning(ctx, log)
		require.NoError(t, err)
		require.True(t, ok)
		return log
	}
	claim := func(ds *types.DataSource, log *types.SyncLog) types.SyncExecution {
		e, ok, err := r.ClaimExecution(ctx, ds.ID, log.ID, ds.TenantID)
		require.NoError(t, err)
		require.True(t, ok)
		return e
	}
	assertStale := func(e types.SyncExecution, ds *types.DataSource, log *types.SyncLog) {
		for _, release := range []bool{false, true} {
			log.Status = types.SyncLogStatusRunning
			if release {
				log.Status = types.SyncLogStatusSuccess
			}
			ds.LastSyncCursor = types.JSON(`{"stale":true}`)
			ds.LastSyncResult = types.JSON(`{"stale":true}`)
			ok, err := r.WriteExecution(ctx, e, ds, log, release)
			require.NoError(t, err)
			require.False(t, ok)
		}
	}
	t.Run("admission_20", func(t *testing.T) {
		ds := makeDS()
		results := make(chan error, 20)
		winners := make(chan bool, 20)
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ok, err := r.CreateIfNoRunning(ctx, &types.SyncLog{DataSourceID: ds.ID, TenantID: 1, Status: types.SyncLogStatusRunning})
				results <- err
				winners <- ok
			}()
		}
		wg.Wait()
		close(results)
		close(winners)
		for err := range results {
			require.NoError(t, err)
		}
		count := 0
		for won := range winners {
			if won {
				count++
			}
		}
		require.Equal(t, 1, count)
	})
	t.Run("different_datasources", func(t *testing.T) {
		ds1, ds2 := makeDS(), makeDS()
		log1, log2 := admit(ds1), admit(ds2)
		first, second := claim(ds1, log1), claim(ds2, log2)
		// Both can own an execution at once; no app-wide datasource mutex.
		for _, e := range []types.SyncExecution{first, second} {
			ok, err := r.HeartbeatExecution(ctx, e)
			require.NoError(t, err)
			require.True(t, ok)
		}
	})
	t.Run("duplicate_delivery_20", func(t *testing.T) {
		ds := makeDS()
		log := admit(ds)
		results := make(chan error, 20)
		wins := make(chan bool, 20)
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, ok, err := r.ClaimExecution(ctx, ds.ID, log.ID, 1)
				results <- err
				wins <- ok
			}()
		}
		wg.Wait()
		close(results)
		close(wins)
		for err := range results {
			require.NoError(t, err)
		}
		count := 0
		for won := range wins {
			if won {
				count++
			}
		}
		require.Equal(t, 1, count)
	})
	t.Run("retry_fences_checkpoint_and_finalization", func(t *testing.T) {
		ds := makeDS()
		log := admit(ds)
		old := claim(ds, log)
		ok, err := r.WriteExecution(ctx, old, ds, log, true)
		require.NoError(t, err)
		require.True(t, ok)
		busy, err := r.CreateIfNoRunning(ctx, &types.SyncLog{DataSourceID: ds.ID, TenantID: 1, Status: types.SyncLogStatusRunning})
		require.NoError(t, err)
		require.False(t, busy, "retry wait is still one active logical run")
		current := claim(ds, log)
		require.Greater(t, current.Generation, old.Generation)
		ds.LastSyncCursor = types.JSON(`{"authoritative":true}`)
		ds.LastSyncResult = ds.LastSyncCursor
		ok, err = r.WriteExecution(ctx, current, ds, log, false)
		require.NoError(t, err)
		require.True(t, ok)
		assertStale(old, ds, log)
		var stored types.DataSource
		require.NoError(t, db.First(&stored, "id = ?", ds.ID).Error)
		require.JSONEq(t, `{"authoritative":true}`, stored.LastSyncCursor.ToString())
		require.JSONEq(t, `{"authoritative":true}`, stored.LastSyncResult.ToString())
	})
	t.Run("recovery_fences_old_run", func(t *testing.T) {
		ds := makeDS()
		oldLog := admit(ds)
		old := claim(ds, oldLog)
		// Exact ownership invalidation performed by startup recovery.
		require.NoError(t, db.Model(&types.SyncLog{}).Where("id = ?", oldLog.ID).Updates(map[string]interface{}{
			"status": types.SyncLogStatusFailed, "execution_claimed": false, "execution_generation": gorm.Expr("execution_generation + 1"),
		}).Error)
		newLog := admit(ds)
		current := claim(ds, newLog)
		ds.LastSyncCursor = types.JSON(`{"new_run":true}`)
		ds.LastSyncResult = ds.LastSyncCursor
		newLog.Status = types.SyncLogStatusSuccess
		ok, err := r.WriteExecution(ctx, current, ds, newLog, true)
		require.NoError(t, err)
		require.True(t, ok)
		assertStale(old, ds, oldLog)
		var stored types.DataSource
		require.NoError(t, db.First(&stored, "id = ?", ds.ID).Error)
		require.JSONEq(t, `{"new_run":true}`, stored.LastSyncCursor.ToString())
		_, ok, err = r.ClaimExecution(ctx, ds.ID, oldLog.ID, 1)
		require.NoError(t, err)
		require.False(t, ok)
	})
	t.Run("expired_claim_new_attempt_same_log", func(t *testing.T) {
		ds := makeDS()
		log := admit(ds)
		old := claim(ds, log)
		// Legacy/local-time and UTC representations must compare as instants.
		require.NoError(t, db.Model(&types.SyncLog{}).Where("id = ?", log.ID).UpdateColumn("updated_at", time.Now().In(time.FixedZone("legacy", 8*60*60)).Add(-2*time.Hour)).Error)
		current := claim(ds, log)
		require.Greater(t, current.Generation, old.Generation)
		assertStale(old, ds, log)
	})
	t.Run("checkpoint_transaction_rollback", func(t *testing.T) {
		ds := makeDS()
		log := admit(ds)
		owner := claim(ds, log)
		// A DS update failure must roll back the preceding log write, including
		// its terminal transition/release. Real DB trigger, not a fake repo.
		if dialect == "sqlite" {
			require.NoError(t, db.Exec(fmt.Sprintf("CREATE TRIGGER fail_ds BEFORE UPDATE OF last_sync_cursor ON data_sources WHEN NEW.id = '%s' BEGIN SELECT RAISE(FAIL, 'test rollback'); END", ds.ID)).Error)
			t.Cleanup(func() { _ = db.Exec("DROP TRIGGER fail_ds").Error })
		} else {
			require.NoError(t, db.Exec("CREATE FUNCTION fail_ds_update() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'test rollback'; END $$ LANGUAGE plpgsql").Error)
			require.NoError(t, db.Exec(fmt.Sprintf("CREATE TRIGGER fail_ds BEFORE UPDATE OF last_sync_cursor ON data_sources FOR EACH ROW WHEN (NEW.id = '%s') EXECUTE FUNCTION fail_ds_update()", ds.ID)).Error)
			t.Cleanup(func() { _ = db.Exec("DROP TRIGGER fail_ds ON data_sources").Error })
		}
		log.Status = types.SyncLogStatusSuccess
		log.ItemsTotal = 123
		ok, err := r.WriteExecution(ctx, owner, ds, log, true)
		require.Error(t, err)
		require.False(t, ok)
		var stored types.SyncLog
		require.NoError(t, db.First(&stored, "id = ?", log.ID).Error)
		require.Equal(t, types.SyncLogStatusRunning, stored.Status)
		require.True(t, stored.ExecutionClaimed)
		require.Zero(t, stored.ItemsTotal)
	})
}
