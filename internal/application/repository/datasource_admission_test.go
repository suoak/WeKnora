package repository

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSyncAdmissionPostgresLockOrdering(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{DisableAutomaticPing: true})
	require.NoError(t, err)
	mock.ExpectBegin()
	// The running-log query must not execute before the datasource row lock.
	mock.ExpectQuery(`SELECT .* FROM "data_sources" .* FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("ds"))
	mock.ExpectExec(`UPDATE "sync_logs"`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "sync_logs"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectCommit()
	ok, err := (&SyncLogRepository{db: db}).CreateIfNoRunning(context.Background(), &types.SyncLog{
		DataSourceID: "ds", TenantID: 1, Status: types.SyncLogStatusRunning,
	})
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSyncAdmissionAcrossConnections(t *testing.T) {
	dsn := filepath.ToSlash(filepath.Join(t.TempDir(), "admission.db")) + "?_busy_timeout=10000&_journal_mode=WAL"
	var repos []*SyncLogRepository
	for i := 0; i < 2; i++ {
		db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
		require.NoError(t, err)
		pool, err := db.DB()
		require.NoError(t, err)
		pool.SetMaxOpenConns(4)
		t.Cleanup(func() { _ = pool.Close() })
		repos = append(repos, &SyncLogRepository{db: db})
	}
	db := repos[0].db
	require.NoError(t, db.AutoMigrate(&types.DataSource{}, &types.SyncLog{}))
	for _, id := range []string{"same", "other"} {
		require.NoError(t, db.Create(&types.DataSource{ID: id, TenantID: 1, KnowledgeBaseID: "kb", Name: id, Type: types.ConnectorTypeFeishu}).Error)
	}
	type outcome struct {
		created bool
		err     error
	}
	results := make(chan outcome, 20)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ok, err := repos[i%2].CreateIfNoRunning(context.Background(), &types.SyncLog{
				ID: fmt.Sprintf("run-%d", i), DataSourceID: "same", TenantID: 1,
				Status: types.SyncLogStatusRunning, StartedAt: time.Now().UTC(),
			})
			results <- outcome{ok, err}
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	count := 0
	for result := range results {
		require.NoError(t, result.err)
		if result.created {
			count++
		}
	}
	require.Equal(t, 1, count)
	var stored int64
	require.NoError(t, db.Model(&types.SyncLog{}).Where("data_source_id = ?", "same").Count(&stored).Error)
	require.EqualValues(t, 1, stored)
	ok, err := repos[1].CreateIfNoRunning(context.Background(), &types.SyncLog{DataSourceID: "other", TenantID: 1, Status: types.SyncLogStatusRunning})
	require.NoError(t, err)
	require.True(t, ok, "different datasource must not share admission")
	// A terminal run releases admission. This does NOT fence its queue retry;
	// execution-level retry fencing remains a separate release blocker.
	require.NoError(t, db.Model(&types.SyncLog{}).Where("data_source_id = ?", "same").Update("status", types.SyncLogStatusSuccess).Error)
	ok, err = repos[1].CreateIfNoRunning(context.Background(), &types.SyncLog{DataSourceID: "same", TenantID: 1, Status: types.SyncLogStatusRunning})
	require.NoError(t, err)
	require.True(t, ok)
}

func TestSyncAdmissionRejectsInvalidOwnerAndCancellation(t *testing.T) {
	db := setupDataSourceRepoTestDB(t)
	r := &SyncLogRepository{db: db}
	require.NoError(t, db.Create(&types.DataSource{ID: "ds", TenantID: 1, KnowledgeBaseID: "kb", Name: "ds", Type: types.ConnectorTypeFeishu}).Error)
	for _, log := range []*types.SyncLog{nil, {DataSourceID: "ds", TenantID: 1, Status: types.SyncLogStatusSuccess},
		{DataSourceID: "missing", TenantID: 1, Status: types.SyncLogStatusRunning},
		{DataSourceID: "ds", TenantID: 2, Status: types.SyncLogStatusRunning}} {
		ok, err := r.CreateIfNoRunning(context.Background(), log)
		require.Error(t, err)
		require.False(t, ok)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ok, err := r.CreateIfNoRunning(ctx, &types.SyncLog{DataSourceID: "ds", TenantID: 1, Status: types.SyncLogStatusRunning})
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, ok)
	var count int64
	require.NoError(t, db.Model(&types.SyncLog{}).Count(&count).Error)
	require.Zero(t, count)
}
