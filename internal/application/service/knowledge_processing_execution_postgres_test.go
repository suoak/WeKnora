package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func processingExecutionPG(t *testing.T) (*gorm.DB, *knowledgeService, *repository.ProcessingFenceRepository) {
	t.Helper()
	dsn := os.Getenv("KNOWHUB25_TEST_POSTGRES_DSN")
	if dsn == "" {
		if os.Getenv("KNOWHUB25_REQUIRE_POSTGRES") == "1" {
			t.Fatal("required isolated PostgreSQL absent")
		}
		t.Skip("isolated PostgreSQL is not configured")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	schema := "execution_" + uuid.NewString()[:8]
	require.NoError(t, admin.Exec("CREATE SCHEMA "+schema).Error)
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	t.Cleanup(func() {
		if c, e := db.DB(); e == nil {
			_ = c.Close()
		}
		require.NoError(t, admin.Exec("DROP SCHEMA "+schema+" CASCADE").Error)
		if c, e := admin.DB(); e == nil {
			_ = c.Close()
		}
	})
	require.NoError(t, db.Exec(`CREATE TABLE knowledges (id VARCHAR(36) PRIMARY KEY, tenant_id BIGINT NOT NULL, parse_status TEXT NOT NULL, error_message TEXT, deleted_at TIMESTAMPTZ, updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP)`).Error)
	migration, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "versioned", "003024_knowledge_processing_fencing.up.sql"))
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(migration)).Error)
	require.NoError(t, db.Exec("INSERT INTO knowledges (id,tenant_id,parse_status) VALUES ('one',1,'pending')").Error)
	repo := repository.NewKnowledgeRepository(db)
	return db, &knowledgeService{repo: repo}, repository.NewProcessingFenceRepository(db)
}

func TestA3ProcessingExecutionPostgresLifecycle(t *testing.T) {
	db, svc, fences := processingExecutionPG(t)
	ctx := context.Background()
	generation, err := fences.Reserve(ctx, 1, "one")
	require.NoError(t, err)
	var calls atomic.Int32
	retry := errors.New("controlled_retry")
	err = svc.runProcessingExecution(ctx, 1, "one", generation, time.Second, func(work context.Context) error {
		calls.Add(1)
		capability, present := types.ProcessingExecutionFromContext(work)
		require.True(t, present)
		require.NotEmpty(t, capability.ExecutionID)
		var first repository.ProcessingFence
		require.NoError(t, db.Take(&first).Error)
		require.Eventually(t, func() bool {
			var current repository.ProcessingFence
			return db.Take(&current).Error == nil && current.LeaseUntil.After(first.LeaseUntil)
		}, 5*time.Second, 20*time.Millisecond, "actual DB lease must renew")
		require.NoError(t, svc.repo.UpdateKnowledgeColumn(work, "one", "parse_status", types.ParseStatusFailed))
		return retry
	})
	require.ErrorIs(t, err, retry)
	var row struct{ ParseStatus string }
	require.NoError(t, db.Table("knowledges").Take(&row).Error)
	require.Equal(t, types.ParseStatusPending, row.ParseStatus)
	require.NoError(t, svc.runProcessingExecution(ctx, 1, "one", generation, time.Second, func(work context.Context) error {
		calls.Add(1)
		return nil
	}))
	require.NoError(t, svc.runProcessingExecution(ctx, 1, "one", generation, time.Second, func(context.Context) error { calls.Add(100); return nil }))
	require.EqualValues(t, 2, calls.Load(), "settled duplicate must not enter worker")
}

func TestA3ProcessingExecutionPostgresSupersession(t *testing.T) {
	_, svc, fences := processingExecutionPG(t)
	ctx := context.Background()
	generation, err := fences.Reserve(ctx, 1, "one")
	require.NoError(t, err)
	entered, done := make(chan struct{}), make(chan error, 1)
	rejected := make(chan error, 1)
	go func() {
		done <- svc.runProcessingExecution(ctx, 1, "one", generation, time.Second, func(work context.Context) error {
			close(entered)
			<-work.Done()
			capability, _ := types.ProcessingExecutionFromContext(work)
			independent := types.WithProcessingExecution(context.Background(), capability)
			rejected <- svc.repo.UpdateKnowledgeColumn(independent, "one", "parse_status", types.ParseStatusCompleted)
			return work.Err()
		})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not enter")
	}
	_, err = fences.Reserve(ctx, 1, "one")
	require.NoError(t, err)
	select {
	case err = <-done:
		require.ErrorIs(t, err, errProcessingExecution)
		require.ErrorIs(t, <-rejected, repository.ErrProcessingFence)
	case <-time.After(5 * time.Second):
		t.Fatal("superseded worker did not exit")
	}
}

func TestA3ProcessingExecutionPostgresParentCancellation(t *testing.T) {
	_, svc, fences := processingExecutionPG(t)
	generation, err := fences.Reserve(context.Background(), 1, "one")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, done := make(chan struct{}), make(chan error, 1)
	go func() {
		done <- svc.runProcessingExecution(ctx, 1, "one", generation, time.Second, func(work context.Context) error { close(entered); <-work.Done(); return work.Err() })
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not enter")
	}
	cancel()
	select {
	case err = <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled worker did not exit")
	}
	claim, err := fences.Claim(context.Background(), 1, "one", generation, time.Second)
	require.NoError(t, err, "cancelled delivery must release lease for retry")
	require.NotEmpty(t, claim.ExecutionID)
}
