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

func TestA3ProcessingExecutionPostgresDurableCleanup(t *testing.T) {
	db, svc, fences := processingExecutionPG(t)
	require.NoError(t, db.AutoMigrate(&types.Chunk{}))
	ctx := context.Background()
	generation, err := fences.Reserve(ctx, 1, "one")
	require.NoError(t, err)
	first, err := fences.Claim(ctx, 1, "one", generation, time.Minute)
	require.NoError(t, err)
	work := types.WithProcessingExecution(ctx, types.ProcessingExecution{TenantID: 1, KnowledgeID: "one", Generation: generation, ExecutionID: first.ExecutionID})
	knowledge := &types.Knowledge{ID: "one", TenantID: 1}
	old := &types.Chunk{ID: uuid.NewString(), TenantID: 1, KnowledgeID: "one", IsEnabled: true}
	require.NoError(t, svc.persistProcessingChunks(work, knowledge, []*types.Chunk{old}))
	second, err := fences.Allocate(ctx, 1, "one", time.Minute)
	require.NoError(t, err)
	newer := &types.Chunk{ID: uuid.NewString(), TenantID: 1, KnowledgeID: "one", IsEnabled: true}
	require.NoError(t, fences.PersistChunks(ctx, second, []*types.Chunk{newer}))
	calls := 0
	cleanup := func(_ context.Context, ids []string) error {
		calls++
		require.Equal(t, []string{old.ID}, ids)
		if calls == 1 {
			return errors.New("controlled_vector_failure")
		}
		return nil
	}
	require.ErrorIs(t, svc.cleanupAttemptChunks(work, knowledge, []*types.Chunk{old}, cleanup), errAttemptCleanupFailed)
	var pending int64
	require.NoError(t, db.Table("knowledge_processing_owned_chunks").Where("chunk_id = ? AND cleanup_complete = FALSE", old.ID).Count(&pending).Error)
	require.EqualValues(t, 1, pending, "failed external cleanup remains durable")
	require.NoError(t, svc.cleanupAttemptChunks(work, knowledge, []*types.Chunk{old}, cleanup))
	require.NoError(t, svc.cleanupAttemptChunks(work, knowledge, []*types.Chunk{old}, cleanup))
	require.Equal(t, 2, calls, "completed cleanup must not repeat backend work")
	var active int64
	require.NoError(t, db.Model(&types.Chunk{}).Where("id = ?", newer.ID).Count(&active).Error)
	require.EqualValues(t, 1, active, "old cleanup cannot delete new generation")
	require.NoError(t, db.Model(&types.Chunk{}).Where("id = ?", old.ID).Count(&active).Error)
	require.Zero(t, active)
}

func TestA3ProcessingExecutionPostgresRestartCleanupReplay(t *testing.T) {
	db, svc, fences := processingExecutionPG(t)
	require.NoError(t, db.AutoMigrate(&types.Chunk{}))
	ctx := context.Background()
	generation, err := fences.Reserve(ctx, 1, "one")
	require.NoError(t, err)
	first, err := fences.Claim(ctx, 1, "one", generation, time.Minute)
	require.NoError(t, err)
	work := types.WithProcessingExecution(ctx, types.ProcessingExecution{TenantID: 1, KnowledgeID: "one", Generation: generation, ExecutionID: first.ExecutionID})
	old := &types.Chunk{ID: uuid.NewString(), TenantID: 1, KnowledgeID: "one", IsEnabled: true}
	require.NoError(t, svc.persistProcessingChunks(work, &types.Knowledge{ID: "one", TenantID: 1}, []*types.Chunk{old}))
	newer, err := fences.Allocate(ctx, 1, "one", time.Minute)
	require.NoError(t, err)
	fresh := &types.Chunk{ID: uuid.NewString(), TenantID: 1, KnowledgeID: "one", IsEnabled: true}
	require.NoError(t, fences.PersistChunks(ctx, newer, []*types.Chunk{fresh}))
	// A missing configured backend is a failure, never a cleanup acknowledgement.
	require.ErrorIs(t, replayProcessingCleanup(ctx, fences, first, nil), errAttemptCleanupFailed)
	var pending int64
	require.NoError(t, db.Table("knowledge_processing_owned_chunks").Where("chunk_id = ? AND cleanup_complete = FALSE", old.ID).Count(&pending).Error)
	require.EqualValues(t, 1, pending)
	// New orchestration object has no old Chunk slice; reconstruct by durable capability.
	restarted := repository.NewProcessingFenceRepository(db)
	calls := 0
	backend := func(_ context.Context, ids []string) error {
		calls++
		require.Equal(t, []string{old.ID}, ids)
		if calls == 1 {
			return errors.New("controlled_backend_unavailable")
		}
		return nil
	}
	require.ErrorIs(t, replayProcessingCleanup(ctx, restarted, first, backend), errAttemptCleanupFailed)
	require.NoError(t, replayProcessingCleanup(ctx, restarted, first, backend))
	require.NoError(t, replayProcessingCleanup(ctx, restarted, first, backend))
	require.Equal(t, 2, calls)
	require.NoError(t, db.Table("knowledge_processing_owned_chunks").Where("chunk_id = ? AND cleanup_complete = FALSE", old.ID).Count(&pending).Error)
	require.Zero(t, pending)
	var active int64
	require.NoError(t, db.Model(&types.Chunk{}).Where("id = ?", fresh.ID).Count(&active).Error)
	require.EqualValues(t, 1, active)
}

func TestA3ProcessingExecutionPostgresPreIndexCleanup(t *testing.T) {
	db, svc, fences := processingExecutionPG(t)
	require.NoError(t, db.AutoMigrate(&types.Chunk{}))
	ctx := context.Background()
	generation, err := fences.Reserve(ctx, 1, "one")
	require.NoError(t, err)
	first, err := fences.Claim(ctx, 1, "one", generation, time.Minute)
	require.NoError(t, err)
	work := types.WithProcessingExecution(ctx, types.ProcessingExecution{TenantID: 1, KnowledgeID: "one", Generation: generation, ExecutionID: first.ExecutionID})
	chunk := &types.Chunk{ID: uuid.NewString(), TenantID: 1, KnowledgeID: "one", IsEnabled: true}
	knowledge := &types.Knowledge{ID: "one", TenantID: 1}
	require.NoError(t, svc.persistProcessingChunks(work, knowledge, []*types.Chunk{chunk}))
	// SQL rows can exist before any external write. Only this known pre-index
	// boundary explicitly confirms zero vector writes; nil replay remains unsafe.
	require.NoError(t, svc.cleanupAttemptChunks(work, knowledge, []*types.Chunk{chunk}, confirmNoAttemptVectorWrites))
	var pending, active int64
	require.NoError(t, db.Table("knowledge_processing_owned_chunks").Where("chunk_id = ? AND cleanup_complete = FALSE", chunk.ID).Count(&pending).Error)
	require.Zero(t, pending)
	require.NoError(t, db.Model(&types.Chunk{}).Where("id = ?", chunk.ID).Count(&active).Error)
	require.Zero(t, active)
}
