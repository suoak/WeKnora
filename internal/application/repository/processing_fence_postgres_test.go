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
	processingMigration(t, db, "down", false) // A live worker still prevents rollback.
	require.NoError(t, repo.Release(ctx, f, true))
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

// Exercises the production KnowledgeRepository SQL write boundary, not a mock.
func TestA3ProcessingFencePostgresKnowledgeWriteGuard(t *testing.T) {
	db, fences := processingPG(t)
	knowledge := &knowledgeRepository{db: db}
	background := context.Background()
	first, err := fences.Allocate(background, 1, "one", time.Minute)
	require.NoError(t, err)
	guarded := func(f ProcessingFence) context.Context {
		return types.WithProcessingExecution(background, types.ProcessingExecution{TenantID: f.TenantID, KnowledgeID: f.KnowledgeID, Generation: f.Generation, ExecutionID: f.ExecutionID})
	}
	require.NoError(t, knowledge.UpdateKnowledgeColumns(guarded(first), "one", map[string]interface{}{"error_message": "", "pending_subtasks_count": 0}))
	var count int64
	require.NoError(t, db.Table("knowledges").Where("id = ? AND pending_subtasks_count = 0 AND error_message = ''", "one").Count(&count).Error)
	require.Equal(t, int64(1), count)
	next, err := fences.Allocate(background, 1, "one", time.Minute)
	require.NoError(t, err)
	require.ErrorIs(t, knowledge.UpdateKnowledgeColumn(guarded(first), "one", "parse_status", types.ParseStatusCompleted), ErrProcessingFence)
	require.ErrorIs(t, knowledge.UpdateKnowledgeColumn(guarded(next), "two", "parse_status", types.ParseStatusFailed), ErrProcessingFence)
	require.NoError(t, db.Model(&ProcessingFence{}).Where("tenant_id = ? AND knowledge_id = ?", 1, "one").Update("lease_until", gorm.Expr("CURRENT_TIMESTAMP - INTERVAL '1 second'")).Error)
	require.ErrorIs(t, knowledge.UpdateKnowledgeColumns(guarded(next), "one", map[string]interface{}{"parse_status": types.ParseStatusCompleted}), ErrProcessingFence)
	recovered, err := fences.Claim(background, 1, "one", next.Generation, time.Minute)
	require.NoError(t, err)
	require.NotEqual(t, next.ExecutionID, recovered.ExecutionID)
	require.ErrorIs(t, knowledge.UpdateKnowledgeColumn(guarded(next), "one", "parse_status", types.ParseStatusCompleted), ErrProcessingFence)
	require.NoError(t, knowledge.UpdateKnowledgeColumn(guarded(recovered), "one", "error_message", "controlled_failure"))
	require.NoError(t, db.Table("knowledges").Where("id = ?", "one").Update("parse_status", types.ParseStatusCancelled).Error)
	require.ErrorIs(t, knowledge.UpdateKnowledgeColumn(guarded(recovered), "one", "parse_status", types.ParseStatusProcessing), ErrProcessingFence)
	var row struct {
		ParseStatus  string
		ErrorMessage string
	}
	require.NoError(t, db.Table("knowledges").Where("id = ?", "one").Take(&row).Error)
	require.Equal(t, types.ParseStatusCancelled, row.ParseStatus)
	require.Equal(t, "controlled_failure", row.ErrorMessage)
	require.NoError(t, db.Table("knowledges").Where("id = ? AND tenant_id = ? AND parse_status = ?", "two", 2, types.ParseStatusProcessing).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestA3ProcessingFencePostgresAtomicPendingAdmission(t *testing.T) {
	db, repo := processingPG(t)
	ctx := context.Background()
	first, err := repo.Allocate(ctx, 1, "one", time.Minute)
	require.NoError(t, err)
	start := make(chan struct{})
	admitted := make(chan struct {
		generation int64
		err        error
	}, 1)
	finished := make(chan error, 1)
	go func() {
		<-start
		generation, err := repo.ReservePending(ctx, 1, "one")
		admitted <- struct {
			generation int64
			err        error
		}{generation, err}
	}()
	go func() {
		<-start
		finished <- repo.Finish(ctx, first, types.ParseStatusCompleted, "")
	}()
	close(start)
	result := <-admitted
	require.NoError(t, result.err)
	require.Equal(t, first.Generation+1, result.generation)
	finishErr := <-finished
	require.True(t, finishErr == nil || errors.Is(finishErr, ErrProcessingFence))
	var state struct {
		ParseStatus  string
		ErrorMessage string
	}
	require.NoError(t, db.Table("knowledges").Where("id = ?", "one").Take(&state).Error)
	require.Equal(t, types.ParseStatusPending, state.ParseStatus)
	require.Empty(t, state.ErrorMessage)
	require.ErrorIs(t, repo.Finish(ctx, first, types.ParseStatusCompleted, ""), ErrProcessingFence)
	current, err := repo.Claim(ctx, 1, "one", result.generation, time.Minute)
	require.NoError(t, err)
	require.Equal(t, result.generation, current.Generation)

	// Failure after pending has been written must roll back both writes. The
	// current running generation and its state remain claimable/unchanged.
	require.NoError(t, db.Table("knowledges").Where("id = ?", "one").Updates(map[string]any{"parse_status": types.ParseStatusProcessing, "error_message": "previous_failure"}).Error)
	require.NoError(t, db.Exec(`CREATE FUNCTION reject_admission() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'controlled_admission_fault'; END $$ LANGUAGE plpgsql;
		CREATE TRIGGER reject_admission BEFORE INSERT OR UPDATE ON knowledge_processing_executions FOR EACH ROW EXECUTE FUNCTION reject_admission();`).Error)
	generation, err := repo.ReservePending(ctx, 1, "one")
	require.Error(t, err)
	require.Zero(t, generation, "an uncommitted generation cannot be enqueued")
	require.NoError(t, db.Table("knowledges").Where("id = ?", "one").Take(&state).Error)
	require.Equal(t, types.ParseStatusProcessing, state.ParseStatus)
	require.Equal(t, "previous_failure", state.ErrorMessage)
	var retained ProcessingFence
	require.NoError(t, db.Where("tenant_id = ? AND knowledge_id = ?", 1, "one").Take(&retained).Error)
	require.Equal(t, current.Generation, retained.Generation)
	require.Equal(t, current.ExecutionID, retained.ExecutionID)
	require.NoError(t, db.Exec("DROP TRIGGER reject_admission ON knowledge_processing_executions").Error)
	retryGeneration, err := repo.ReservePending(ctx, 1, "one")
	require.NoError(t, err)
	require.Equal(t, current.Generation+1, retryGeneration)

	// Tenant mismatch and cancellation cannot be overwritten by admission.
	generation, err = repo.ReservePending(ctx, 2, "one")
	require.Error(t, err)
	require.Zero(t, generation)
	require.NoError(t, db.Table("knowledges").Where("id = ?", "one").Update("parse_status", types.ParseStatusCancelled).Error)
	generation, err = repo.ReservePending(ctx, 1, "one")
	require.ErrorIs(t, err, ErrProcessingFence)
	require.Zero(t, generation)
	require.NoError(t, db.Table("knowledges").Where("id = ?", "one").Take(&state).Error)
	require.Equal(t, types.ParseStatusCancelled, state.ParseStatus)
}

func TestA3ProcessingFencePostgresEnqueueFailureCannotOverwriteClaim(t *testing.T) {
	db, repo := processingPG(t)
	ctx := context.Background()
	generation, err := repo.ReservePending(ctx, 1, "one")
	require.NoError(t, err)
	require.NoError(t, repo.FailPendingAdmission(ctx, 1, "one", generation))
	var state struct {
		ParseStatus  string
		ErrorMessage string
	}
	require.NoError(t, db.Table("knowledges").Where("id = ?", "one").Take(&state).Error)
	require.Equal(t, types.ParseStatusFailed, state.ParseStatus)
	require.Equal(t, "enqueue_failed", state.ErrorMessage)
	require.ErrorIs(t, repo.FailPendingAdmission(ctx, 1, "one", generation), ErrProcessingFence)
	next, err := repo.ReservePending(ctx, 1, "one")
	require.NoError(t, err)
	require.ErrorIs(t, repo.FailPendingAdmission(ctx, 1, "one", generation), ErrProcessingFence)
	require.ErrorIs(t, repo.FailPendingAdmission(ctx, 2, "one", next), ErrProcessingFence)
	start := make(chan struct{})
	claimed := make(chan error, 1)
	failed := make(chan error, 1)
	go func() { <-start; _, e := repo.Claim(ctx, 1, "one", next, time.Minute); claimed <- e }()
	go func() { <-start; failed <- repo.FailPendingAdmission(ctx, 1, "one", next) }()
	close(start)
	claimErr, failureErr := <-claimed, <-failed
	require.True(t, (claimErr == nil && errors.Is(failureErr, ErrProcessingFence)) || (failureErr == nil && errors.Is(claimErr, ErrProcessingFence)))
	require.NoError(t, db.Table("knowledges").Where("id = ?", "one").Take(&state).Error)
	if claimErr == nil {
		require.Equal(t, types.ParseStatusPending, state.ParseStatus)
		require.Empty(t, state.ErrorMessage)
	} else {
		require.Equal(t, types.ParseStatusFailed, state.ParseStatus)
	}
	fresh, err := repo.ReservePending(ctx, 1, "one")
	require.NoError(t, err)
	require.NoError(t, db.Table("knowledges").Where("id = ?", "one").Update("parse_status", types.ParseStatusCancelled).Error)
	require.ErrorIs(t, repo.FailPendingAdmission(ctx, 1, "one", fresh), ErrProcessingFence)
	require.NoError(t, db.Table("knowledges").Where("id = ?", "one").Take(&state).Error)
	require.Equal(t, types.ParseStatusCancelled, state.ParseStatus)
}

func TestA3ProcessingFencePostgresAtomicMetadataPublication(t *testing.T) {
	db, repo := processingPG(t)
	ctx := context.Background()
	require.NoError(t, db.Exec("ALTER TABLE knowledges ADD COLUMN title TEXT DEFAULT 'historical-title', ADD COLUMN metadata JSONB DEFAULT '{}'::jsonb").Error)
	first, err := repo.Allocate(ctx, 1, "one", time.Minute)
	require.NoError(t, err)
	request := map[string]string{"file_path": "new-file", "title": "request-title", "metadata": `{"source":"fixture"}`}
	generation, err := repo.ReservePendingWithMetadata(ctx, 1, "one", request)
	require.NoError(t, err)
	require.Equal(t, first.Generation+1, generation)
	var row struct {
		FilePath             string
		Title                string
		ParseStatus          string
		Metadata             string
		PendingSubtasksCount int64
	}
	require.NoError(t, db.Table("knowledges").Select("file_path,title,parse_status,metadata::text AS metadata,pending_subtasks_count").Where("id = ?", "one").Take(&row).Error)
	require.Equal(t, "new-file", row.FilePath)
	require.Equal(t, "request-title", row.Title)
	require.JSONEq(t, request["metadata"], row.Metadata)
	require.Equal(t, types.ParseStatusPending, row.ParseStatus)
	require.EqualValues(t, 3, row.PendingSubtasksCount, "request publication does not reset worker counters")
	require.ErrorIs(t, repo.Finish(ctx, first, types.ParseStatusFinalizing, ""), ErrProcessingFence)
	oldWorker := types.WithProcessingExecution(ctx, types.ProcessingExecution{TenantID: 1, KnowledgeID: "one", Generation: first.Generation, ExecutionID: first.ExecutionID})
	require.ErrorIs(t, NewKnowledgeRepository(db).UpdateKnowledgeColumn(oldWorker, "one", "title", "stale-worker-title"), ErrProcessingFence)
	require.NoError(t, db.Table("knowledges").Where("id = ?", "one").Update("parse_status", types.ParseStatusProcessing).Error)
	// An execution-table write failure rolls back metadata and pending state too.
	require.NoError(t, db.Exec(`CREATE FUNCTION reject_metadata_admission() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'controlled_metadata_publication_fault'; END $$ LANGUAGE plpgsql;
 CREATE TRIGGER reject_metadata_admission BEFORE INSERT OR UPDATE ON knowledge_processing_executions FOR EACH ROW EXECUTE FUNCTION reject_metadata_admission();`).Error)
	failedGeneration, err := repo.ReservePendingWithMetadata(ctx, 1, "one", map[string]string{"file_path": "uncommitted-file", "title": "uncommitted-title"})
	require.Error(t, err)
	require.Zero(t, failedGeneration)
	require.NoError(t, db.Table("knowledges").Select("file_path,title,parse_status,metadata::text AS metadata,pending_subtasks_count").Where("id = ?", "one").Take(&row).Error)
	require.Equal(t, "new-file", row.FilePath)
	require.Equal(t, "request-title", row.Title)
	require.Equal(t, types.ParseStatusProcessing, row.ParseStatus, "pending state must roll back with metadata")
	require.NoError(t, db.Exec("DROP TRIGGER reject_metadata_admission ON knowledge_processing_executions").Error)
	var current ProcessingFence
	require.NoError(t, db.Where("tenant_id = ? AND knowledge_id = ?", 1, "one").Take(&current).Error)
	require.Equal(t, generation, current.Generation)
	for _, changes := range []map[string]string{{"tenant_id": "2"}, {"parse_status": "completed"}, {"pending_subtasks_count": "0"}, {"execution_id": "untrusted"}, {"metadata": "broken-json"}} {
		rejected, err := repo.ReservePendingWithMetadata(ctx, 1, "one", changes)
		require.ErrorIs(t, err, ErrProcessingAdmissionMetadata)
		require.Zero(t, rejected)
	}
	retryGeneration, err := repo.ReservePendingWithMetadata(ctx, 1, "one", map[string]string{"title": ""})
	require.NoError(t, err)
	require.Equal(t, generation+1, retryGeneration)
	require.NoError(t, db.Table("knowledges").Select("title").Where("id = ?", "one").Take(&row).Error)
	require.Empty(t, row.Title, "explicit empty request values are persisted")
	require.NoError(t, db.Table("knowledges").Where("id = ?", "one").Update("parse_status", types.ParseStatusCancelled).Error)
	rejected, err := repo.ReservePendingWithMetadata(ctx, 1, "one", request)
	require.ErrorIs(t, err, ErrProcessingFence)
	require.Zero(t, rejected)
}
