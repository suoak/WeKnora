package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func processingPG(t *testing.T) (*gorm.DB, *ProcessingFenceRepository) {
	t.Helper()
	dsn := os.Getenv("KNOWHUB25_TEST_POSTGRES_DSN")
	if dsn == "" {
		if os.Getenv("KNOWHUB25_REQUIRE_POSTGRES") == "1" {
			t.Fatal("required isolated PostgreSQL is absent")
		}
		t.Skip("isolated PostgreSQL is not configured")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("isolated PostgreSQL connection failed")
	}
	schema := "fence_" + uuid.NewString()[:8]
	if admin.Exec("CREATE SCHEMA "+schema).Error != nil {
		t.Fatal("isolated schema creation failed")
	}
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("isolated schema connection failed")
	}
	t.Cleanup(func() {
		if sqlDB, e := db.DB(); e == nil {
			_ = sqlDB.Close()
		}
		if admin.Exec("DROP SCHEMA "+schema+" CASCADE").Error != nil {
			t.Error("isolated schema cleanup failed")
		}
		if sqlDB, e := admin.DB(); e == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.Exec(`CREATE TABLE knowledges (
		id VARCHAR(36) PRIMARY KEY, tenant_id BIGINT NOT NULL, parse_status TEXT NOT NULL,
		error_message TEXT, updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
		deleted_at TIMESTAMPTZ, file_path TEXT DEFAULT 'historical-file', pending_subtasks_count BIGINT DEFAULT 3
	)`).Error)
	// Test-only fixture initialization; production migrations are executed below.
	require.NoError(t, db.AutoMigrate(&types.Chunk{}))
	processingMigration(t, db, "up", true)
	require.NoError(t, db.Exec("INSERT INTO knowledges (id,tenant_id,parse_status) VALUES ('one',1,'processing'),('two',2,'processing')").Error)
	return db, NewProcessingFenceRepository(db)
}

func processingMigration(t *testing.T, db *gorm.DB, direction string, success bool) {
	t.Helper()
	text, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "versioned", "003024_knowledge_processing_fencing."+direction+".sql"))
	require.NoError(t, err)
	err = db.Transaction(func(tx *gorm.DB) error { return tx.Exec(string(text)).Error })
	if success {
		require.NoError(t, err)
	} else {
		require.Error(t, err)
	}
}

func TestA3ProcessingFencePostgresConcurrentGeneration(t *testing.T) {
	db, repo := processingPG(t)
	ctx := context.Background()
	const workers = 20
	fences := make(chan ProcessingFence, workers)
	errs := make(chan error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			f, err := repo.Allocate(ctx, 1, "one", time.Minute)
			fences <- f
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(fences)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var ordered []ProcessingFence
	for f := range fences {
		ordered = append(ordered, f)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Generation < ordered[j].Generation })
	for i, f := range ordered {
		require.Equal(t, int64(i+1), f.Generation)
		require.NotEmpty(t, f.ExecutionID)
	}
	for _, f := range ordered[:workers-1] {
		require.ErrorIs(t, repo.Finish(ctx, f, types.ParseStatusFailed, "index_failed"), ErrProcessingFence)
	}
	require.NoError(t, repo.Finish(ctx, ordered[workers-1], types.ParseStatusFinalizing, ""))
	var stored struct {
		ParseStatus          string
		FilePath             string
		PendingSubtasksCount int
	}
	require.NoError(t, db.Table("knowledges").Where("id = ?", "one").Take(&stored).Error)
	require.Equal(t, types.ParseStatusFinalizing, stored.ParseStatus)
	require.Equal(t, "historical-file", stored.FilePath)
	require.Equal(t, 3, stored.PendingSubtasksCount)
}

func TestA3ProcessingFencePostgresChunkOwnershipAndRollback(t *testing.T) {
	db, repo := processingPG(t)
	ctx := context.Background()
	old, err := repo.Allocate(ctx, 1, "one", time.Minute)
	require.NoError(t, err)
	oldChunk := &types.Chunk{ID: uuid.NewString(), TenantID: 1, KnowledgeID: "one", IsEnabled: true}
	require.NoError(t, repo.PersistChunks(ctx, old, []*types.Chunk{oldChunk}))
	newer, err := repo.Allocate(ctx, 1, "one", time.Minute)
	require.NoError(t, err)
	newChunk := &types.Chunk{ID: uuid.NewString(), TenantID: 1, KnowledgeID: "one", IsEnabled: true}
	require.NoError(t, repo.PersistChunks(ctx, newer, []*types.Chunk{newChunk}))
	other, err := repo.Allocate(ctx, 2, "two", time.Minute)
	require.NoError(t, err)
	otherChunk := &types.Chunk{ID: uuid.NewString(), TenantID: 2, KnowledgeID: "two", IsEnabled: true}
	require.NoError(t, repo.PersistChunks(ctx, other, []*types.Chunk{otherChunk}))
	require.ErrorIs(t, repo.PersistChunks(ctx, old, []*types.Chunk{{ID: uuid.NewString(), TenantID: 1, KnowledgeID: "one"}}), ErrProcessingFence)
	rolledBackID := uuid.NewString()
	// A duplicate ID fails after the first write: both ledger and SQL writes rollback.
	err = repo.PersistChunks(ctx, newer, []*types.Chunk{{ID: rolledBackID, TenantID: 1, KnowledgeID: "one"}, {ID: newChunk.ID, TenantID: 1, KnowledgeID: "one"}})
	require.Error(t, err)
	var count int64
	require.NoError(t, db.Model(&types.Chunk{}).Where("id = ?", rolledBackID).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Model(&processingOwnedChunk{}).Where("chunk_id = ?", rolledBackID).Count(&count).Error)
	require.Zero(t, count)
	ids, err := repo.CleanupSQL(ctx, old)
	require.NoError(t, err)
	require.Equal(t, []string{oldChunk.ID}, ids)
	// Partial vector failure keeps the exact worklist retryable. SQL cleanup is idempotent.
	ids, err = repo.CleanupSQL(ctx, old)
	require.NoError(t, err)
	require.Equal(t, []string{oldChunk.ID}, ids)
	for _, id := range []string{newChunk.ID, otherChunk.ID} {
		var chunk types.Chunk
		require.NoError(t, db.Where("id = ?", id).Take(&chunk).Error)
		require.True(t, chunk.IsEnabled)
	}
	require.NoError(t, repo.CompleteCleanup(ctx, old))
	ids, err = repo.CleanupSQL(ctx, old)
	require.NoError(t, err)
	require.Empty(t, ids)
}

func TestA3ProcessingFencePostgresCancelDeleteAndLease(t *testing.T) {
	db, repo := processingPG(t)
	ctx := context.Background()
	f, err := repo.Allocate(ctx, 1, "one", time.Minute)
	require.NoError(t, err)
	require.NoError(t, repo.Renew(ctx, f, time.Minute))
	require.NoError(t, db.Exec("UPDATE knowledge_processing_executions SET lease_until = CURRENT_TIMESTAMP - INTERVAL '1 second'").Error)
	require.ErrorIs(t, repo.Renew(ctx, f, time.Minute), ErrProcessingFence)
	require.ErrorIs(t, repo.Finish(ctx, f, types.ParseStatusFailed, "index_failed"), ErrProcessingFence)
	for _, status := range []string{types.ParseStatusCancelled, types.ParseStatusDeleting} {
		require.NoError(t, db.Exec("UPDATE knowledges SET parse_status = ? WHERE id = 'one'", status).Error)
		_, err = repo.Allocate(ctx, 1, "one", time.Minute)
		require.ErrorIs(t, err, ErrProcessingFence)
		require.ErrorIs(t, repo.Finish(ctx, f, types.ParseStatusFailed, "index_failed"), ErrProcessingFence)
	}
	require.NoError(t, db.Exec("UPDATE knowledges SET deleted_at = CURRENT_TIMESTAMP WHERE id = 'one'").Error)
	_, err = repo.Allocate(ctx, 1, "one", time.Minute)
	require.True(t, errors.Is(err, gorm.ErrRecordNotFound))
}

func TestA3ProcessingFencePostgresMigrationSafety(t *testing.T) {
	db, repo := processingPG(t)
	ctx := context.Background()
	processingMigration(t, db, "up", true)
	f, err := repo.Allocate(ctx, 1, "one", time.Minute)
	require.NoError(t, err)
	require.NoError(t, repo.PersistChunks(ctx, f, []*types.Chunk{{ID: uuid.NewString(), TenantID: 1, KnowledgeID: "one"}}))
	processingMigration(t, db, "down", false)
	var count int64
	require.NoError(t, db.Model(&processingOwnedChunk{}).Count(&count).Error)
	require.Equal(t, int64(1), count)
	require.NoError(t, repo.CompleteCleanup(ctx, f))
	processingMigration(t, db, "down", true)
	processingMigration(t, db, "down", true)
	processingMigration(t, db, "up", true)
	require.NoError(t, db.Table("knowledges").Count(&count).Error)
	require.Equal(t, int64(2), count)
}

func TestA3ProcessingFencePostgresQueueClaimAndRecovery(t *testing.T) {
	db, repo := processingPG(t)
	ctx := context.Background()
	generation, err := repo.Reserve(ctx, 1, "one")
	require.NoError(t, err)
	const deliveries = 10
	claims := make(chan ProcessingFence, deliveries)
	errs := make(chan error, deliveries)
	barrier := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < deliveries; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-barrier
			claim, err := repo.Claim(ctx, 1, "one", generation, time.Minute)
			claims <- claim
			errs <- err
		}()
	}
	close(barrier)
	wg.Wait()
	close(claims)
	close(errs)
	success := 0
	for err := range errs {
		if err == nil {
			success++
		} else {
			require.ErrorIs(t, err, ErrProcessingClaimBusy)
		}
	}
	require.Equal(t, 1, success)
	var old ProcessingFence
	require.NoError(t, db.Where("tenant_id = ? AND knowledge_id = ?", 1, "one").Take(&old).Error)
	require.Equal(t, generation, old.Generation)
	require.NoError(t, db.Exec("UPDATE knowledge_processing_executions SET lease_until = CURRENT_TIMESTAMP - INTERVAL '1 second'").Error)
	recovered, err := repo.Claim(ctx, 1, "one", generation, time.Minute)
	require.NoError(t, err)
	require.Equal(t, generation, recovered.Generation)
	require.NotEqual(t, old.ExecutionID, recovered.ExecutionID)
	require.ErrorIs(t, repo.Renew(ctx, old, time.Minute), ErrProcessingFence)
	require.ErrorIs(t, repo.Finish(ctx, old, types.ParseStatusFailed, "index_failed"), ErrProcessingFence)
	newGeneration, err := repo.Reserve(ctx, 1, "one")
	require.NoError(t, err)
	require.Equal(t, generation+1, newGeneration)
	_, err = repo.Claim(ctx, 1, "one", generation, time.Minute)
	require.ErrorIs(t, err, ErrProcessingFence)
}
