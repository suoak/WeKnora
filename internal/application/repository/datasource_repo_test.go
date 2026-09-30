package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupDataSourceRepoTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.DataSource{}, &types.SyncLog{}))
	return db
}

func TestDataSourceRepositoryUpdateSyncStateClearsErrorMessage(t *testing.T) {
	db := setupDataSourceRepoTestDB(t)
	repo := NewDataSourceRepository(db)
	now := time.Now().UTC()
	result := types.JSON(`{"total":0}`)

	ds := &types.DataSource{
		ID:              "ds-1",
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		Name:            "Feishu",
		Type:            types.ConnectorTypeFeishu,
		Status:          types.DataSourceStatusError,
		ErrorMessage:    "previous failure",
	}
	require.NoError(t, repo.Create(context.Background(), ds))

	ds.Status = types.DataSourceStatusActive
	ds.ErrorMessage = ""
	ds.LastSyncAt = &now
	ds.LastSyncResult = result
	require.NoError(t, repo.UpdateSyncState(context.Background(), ds))

	var stored types.DataSource
	require.NoError(t, db.First(&stored, "id = ?", ds.ID).Error)
	assert.Equal(t, types.DataSourceStatusActive, stored.Status)
	assert.Empty(t, stored.ErrorMessage)
	assert.Equal(t, result.ToString(), stored.LastSyncResult.ToString())
	require.NotNil(t, stored.LastSyncAt)
}

func TestDataSourceRepositoryUpdatePersistsDisabledSyncDeletions(t *testing.T) {
	db := setupDataSourceRepoTestDB(t)
	repo := NewDataSourceRepository(db)
	ctx := context.Background()

	ds := &types.DataSource{
		ID:              "ds-sync-deletions",
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		Name:            "Feishu",
		Type:            types.ConnectorTypeFeishu,
		SyncDeletions:   true,
	}
	require.NoError(t, repo.Create(ctx, ds))

	ds.SyncDeletions = false
	require.NoError(t, repo.Update(ctx, ds))
	assert.False(t, ds.SyncDeletions)

	var stored types.DataSource
	require.NoError(t, db.First(&stored, "id = ?", ds.ID).Error)
	assert.False(t, stored.SyncDeletions)

	ds.SyncDeletions = true
	require.NoError(t, repo.Update(ctx, ds))
	assert.True(t, ds.SyncDeletions)
	require.NoError(t, db.First(&stored, "id = ?", ds.ID).Error)
	assert.True(t, stored.SyncDeletions)
}

func TestDataSourceRepositoryCreatePersistsDisabledSyncDeletions(t *testing.T) {
	db := setupDataSourceRepoTestDB(t)
	repo := NewDataSourceRepository(db)
	ctx := context.Background()

	ds := &types.DataSource{
		ID:              "ds-create-sync-deletions",
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		Name:            "Feishu",
		Type:            types.ConnectorTypeFeishu,
		SyncDeletions:   false,
	}
	require.NoError(t, repo.Create(ctx, ds))
	assert.False(t, ds.SyncDeletions, "Create must not leave the in-memory field hydrated to the GORM default")

	var stored types.DataSource
	require.NoError(t, db.First(&stored, "id = ?", ds.ID).Error)
	assert.False(t, stored.SyncDeletions)

	// A later Updates() on the same pointer must not persist the hydrated default.
	ds.Name = "Renamed"
	require.NoError(t, repo.Update(ctx, ds))
	require.NoError(t, db.First(&stored, "id = ?", ds.ID).Error)
	assert.False(t, stored.SyncDeletions)
	assert.Equal(t, "Renamed", stored.Name)
}

func TestDataSourceRepositoryCreatePersistsEnabledSyncDeletions(t *testing.T) {
	db := setupDataSourceRepoTestDB(t)
	repo := NewDataSourceRepository(db)
	ctx := context.Background()

	ds := &types.DataSource{
		ID:              "ds-create-sync-deletions-enabled",
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		Name:            "Feishu",
		Type:            types.ConnectorTypeFeishu,
		SyncDeletions:   true,
	}
	require.NoError(t, repo.Create(ctx, ds))
	assert.True(t, ds.SyncDeletions)

	var stored types.DataSource
	require.NoError(t, db.First(&stored, "id = ?", ds.ID).Error)
	assert.True(t, stored.SyncDeletions)
}

func TestDataSourceRepositoryDeleteSoftDeletesOnSQLite(t *testing.T) {
	db := setupDataSourceRepoTestDB(t)
	repo := NewDataSourceRepository(db)
	ctx := context.Background()

	target := &types.DataSource{
		ID:              "ds-delete-target",
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		Name:            "Delete target",
		Type:            types.ConnectorTypeFeishu,
	}
	other := &types.DataSource{
		ID:              "ds-delete-other",
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		Name:            "Other data source",
		Type:            types.ConnectorTypeFeishu,
	}
	require.NoError(t, repo.Create(ctx, target))
	require.NoError(t, repo.Create(ctx, other))

	require.NoError(t, repo.Delete(ctx, target.ID))

	var deleted types.DataSource
	require.NoError(t, db.Unscoped().First(&deleted, "id = ?", target.ID).Error)
	assert.True(t, deleted.DeletedAt.Valid)

	found, err := repo.FindByID(ctx, target.ID)
	assert.Error(t, err)
	assert.Nil(t, found)

	untouched, err := repo.FindByID(ctx, other.ID)
	require.NoError(t, err)
	assert.Equal(t, other.ID, untouched.ID)
}

func TestSyncLogRepositoryUpdateResultClearsErrorMessage(t *testing.T) {
	db := setupDataSourceRepoTestDB(t)
	repo := NewSyncLogRepository(db)
	finishedAt := time.Now().UTC()
	result := types.JSON(`{"total":0}`)

	log := &types.SyncLog{
		ID:           "log-1",
		DataSourceID: "ds-1",
		TenantID:     1,
		Status:       types.SyncLogStatusFailed,
		ErrorMessage: "previous failure",
		ItemsTotal:   1,
		ItemsFailed:  1,
	}
	require.NoError(t, repo.Create(context.Background(), log))

	log.Status = types.SyncLogStatusSuccess
	log.ErrorMessage = ""
	log.FinishedAt = &finishedAt
	log.ItemsTotal = 0
	log.ItemsFailed = 0
	log.Result = result
	require.NoError(t, repo.UpdateResult(context.Background(), log))

	var stored types.SyncLog
	require.NoError(t, db.First(&stored, "id = ?", log.ID).Error)
	assert.Equal(t, types.SyncLogStatusSuccess, stored.Status)
	assert.Empty(t, stored.ErrorMessage)
	assert.Zero(t, stored.ItemsTotal)
	assert.Zero(t, stored.ItemsFailed)
	assert.Equal(t, result.ToString(), stored.Result.ToString())
	require.NotNil(t, stored.FinishedAt)
}

func TestSyncLogRepositoryCreateIfNoRunningAllowsSingleWorkflow(t *testing.T) {
	db := setupDataSourceRepoTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	// SQLite has no row-level FOR UPDATE; one connection models its serialized
	// writer semantics while production MySQL/Postgres use the row lock clause.
	sqlDB.SetMaxOpenConns(1)
	dsRepo := NewDataSourceRepository(db)
	syncRepo := NewSyncLogRepository(db)
	ctx := context.Background()
	require.NoError(t, dsRepo.Create(ctx, &types.DataSource{
		ID: "ds-claim", TenantID: 1, KnowledgeBaseID: "kb-1", Name: "Feishu", Type: types.ConnectorTypeFeishu,
	}))

	const contenders = 10
	results := make(chan bool, contenders)
	errs := make(chan error, contenders)
	var wg sync.WaitGroup
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			claimed, claimErr := syncRepo.CreateIfNoRunning(ctx, &types.SyncLog{
				ID: fmt.Sprintf("claim-%d", i), DataSourceID: "ds-claim", TenantID: 1,
				Status: types.SyncLogStatusRunning, StartedAt: time.Now().UTC(),
			})
			results <- claimed
			errs <- claimErr
		}(i)
	}
	wg.Wait()
	close(results)
	close(errs)
	for claimErr := range errs {
		require.NoError(t, claimErr)
	}
	winners := 0
	for claimed := range results {
		if claimed {
			winners++
		}
	}
	assert.Equal(t, 1, winners)
}
