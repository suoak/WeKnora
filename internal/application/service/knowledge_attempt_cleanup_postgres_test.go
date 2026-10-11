package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The vector executor is synthetic; SQL persistence and generation transitions
// below use production repositories against actual isolated PostgreSQL.
type failedAttemptVectorExecutor struct {
	parentChildRetrieveEngine
	batch        func(context.Context, []*types.IndexInfo) error
	cleanup      func(context.Context, []string) error
	wholeDeletes int
}

func (e *failedAttemptVectorExecutor) BatchIndex(ctx context.Context, _ embedding.Embedder, infos []*types.IndexInfo, _ []types.RetrieverType) error {
	return e.batch(ctx, infos)
}

func (e *failedAttemptVectorExecutor) DeleteByChunkIDList(ctx context.Context, ids []string, _ int, _ string) error {
	return e.cleanup(ctx, ids)
}

func (e *failedAttemptVectorExecutor) DeleteByKnowledgeIDList(context.Context, []string, int, string) error {
	e.wholeDeletes++
	return nil
}

func TestA3ProcessingAttemptCleanupPostgres(t *testing.T) {
	dsn := os.Getenv("KNOWHUB25_TEST_POSTGRES_DSN")
	if dsn == "" {
		if os.Getenv("KNOWHUB25_REQUIRE_POSTGRES") == "1" {
			t.Fatal("required isolated PostgreSQL is absent")
		}
		t.Skip("isolated PostgreSQL is not configured")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	schema := "cleanup_" + uuid.NewString()[:8]
	require.NoError(t, admin.Exec("CREATE SCHEMA "+schema).Error)
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	t.Cleanup(func() {
		if connection, e := db.DB(); e == nil {
			_ = connection.Close()
		}
		require.NoError(t, admin.Exec("DROP SCHEMA "+schema+" CASCADE").Error)
		if connection, e := admin.DB(); e == nil {
			_ = connection.Close()
		}
	})
	// Synthetic, isolated fixture only; not the application migration runner.
	require.NoError(t, db.AutoMigrate(&types.Chunk{}))
	chunks := repository.NewChunkRepository(db)
	service := &knowledgeService{chunkRepo: chunks}
	old := &types.Chunk{ID: uuid.NewString(), TenantID: 1, KnowledgeID: "one", IsEnabled: true}
	newer := &types.Chunk{ID: uuid.NewString(), TenantID: 1, KnowledgeID: "one", IsEnabled: true}
	other := &types.Chunk{ID: uuid.NewString(), TenantID: 2, KnowledgeID: "two", IsEnabled: true}
	require.NoError(t, chunks.CreateChunks(context.Background(), []*types.Chunk{old, newer, other}))
	knowledge := &types.Knowledge{ID: "one", TenantID: 1}
	vectorCalls := 0
	vectorDelete := func(ctx context.Context, ids []string) error {
		vectorCalls++
		require.NoError(t, ctx.Err())
		require.Equal(t, []string{old.ID}, ids)
		return errors.New("synthetic vector failure")
	}
	// A failed vector cleanup remains a failure despite successful SQL cleanup.
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, service.cleanupAttemptChunks(canceled, knowledge, []*types.Chunk{old}, vectorDelete), errAttemptCleanupFailed)
	require.Equal(t, 1, vectorCalls)
	var count int64
	require.NoError(t, db.Model(&types.Chunk{}).Where("id IN ?", []string{newer.ID, other.ID}).Count(&count).Error)
	require.Equal(t, int64(2), count)
	// Retrying the same exact IDs is safe and does not include the new generation.
	require.NoError(t, service.cleanupAttemptChunks(context.Background(), knowledge, []*types.Chunk{old}, func(ctx context.Context, ids []string) error {
		vectorCalls++
		require.Equal(t, []string{old.ID}, ids)
		return nil
	}))
	require.Equal(t, 2, vectorCalls)
	require.ErrorIs(t, service.cleanupAttemptChunks(context.Background(), knowledge, []*types.Chunk{other}, nil), errAttemptCleanupFailed)
	// Real SQL trigger failure must stop before vector deletion and remain visible.
	require.NoError(t, db.Exec(`CREATE FUNCTION reject_cleanup() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic_sql_cleanup_failure'; END $$`).Error)
	require.NoError(t, db.Exec(`CREATE TRIGGER reject_cleanup BEFORE UPDATE ON chunks FOR EACH ROW EXECUTE FUNCTION reject_cleanup()`).Error)
	require.ErrorIs(t, service.cleanupAttemptChunks(context.Background(), knowledge, []*types.Chunk{newer}, func(context.Context, []string) error { vectorCalls++; return nil }), errAttemptCleanupFailed)
	require.Equal(t, 2, vectorCalls)
	require.NoError(t, db.Model(&types.Chunk{}).Where("id = ?", newer.ID).Count(&count).Error)
	require.Equal(t, int64(1), count)

	t.Run("actual_processChunks_preserves_new_generation", func(t *testing.T) {
		require.NoError(t, db.Exec(`DROP TRIGGER reject_cleanup ON chunks`).Error)
		require.NoError(t, db.AutoMigrate(&types.Knowledge{}))
		migration, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "versioned", "003024_knowledge_processing_fencing.up.sql"))
		require.NoError(t, err)
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return tx.Exec(string(migration)).Error }))
		knowledge := &types.Knowledge{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb-one", ParseStatus: types.ParseStatusProcessing}
		knowledgeRepo := repository.NewKnowledgeRepository(db)
		require.NoError(t, knowledgeRepo.CreateKnowledge(context.Background(), knowledge))
		fences := repository.NewProcessingFenceRepository(db)
		first, err := fences.Allocate(context.Background(), 1, knowledge.ID, time.Minute)
		require.NoError(t, err)
		newChunk := &types.Chunk{ID: uuid.NewString(), TenantID: 1, KnowledgeID: knowledge.ID, IsEnabled: true}
		var oldIDs []string
		vectorDeletes := 0
		engine := &failedAttemptVectorExecutor{}
		engine.batch = func(ctx context.Context, infos []*types.IndexInfo) error {
			for _, info := range infos {
				require.Equal(t, info.SourceID, info.ChunkID)
				oldIDs = append(oldIDs, info.ChunkID)
			}
			require.NotEmpty(t, oldIDs)
			// The synchronous barrier is inside BatchIndex: new generation is
			// committed before the old invocation receives its indexing failure.
			current, err := fences.Allocate(context.Background(), 1, knowledge.ID, time.Minute)
			require.NoError(t, err)
			require.NoError(t, fences.PersistChunks(context.Background(), current, []*types.Chunk{newChunk}))
			return errors.New("synthetic_index_failure")
		}
		engine.cleanup = func(ctx context.Context, ids []string) error {
			vectorDeletes++
			require.ElementsMatch(t, oldIDs, ids)
			require.NotContains(t, ids, newChunk.ID)
			return nil
		}
		tenant := &types.Tenant{ID: 1, RetrieverEngines: types.RetrieverEngines{Engines: []types.RetrieverEngineParams{{RetrieverType: types.VectorRetrieverType, RetrieverEngineType: types.PostgresRetrieverEngineType}}}}
		ctx := context.WithValue(context.Background(), types.TenantInfoContextKey, tenant)
		ctx = types.WithProcessingExecution(ctx, types.ProcessingExecution{TenantID: 1, KnowledgeID: knowledge.ID, Generation: first.Generation, ExecutionID: first.ExecutionID})
		pipeline := &knowledgeService{repo: knowledgeRepo, chunkRepo: chunks, modelService: parentChildModelService{embedder: parentChildEmbedder{}}, retrieveEngine: parentChildRetrieveRegistry{engine: engine}, graphEngine: parentChildGraphRepo{}, tenantRepo: parentChildTenantRepo{}, task: parentChildTaskEnqueuer{}}
		kb := &types.KnowledgeBase{ID: "kb-one", TenantID: 1, EmbeddingModelID: "synthetic-embedding", IndexingStrategy: types.IndexingStrategy{VectorEnabled: true}}
		failure := pipeline.processChunks(ctx, kb, knowledge, []types.ParsedChunk{{Content: "synthetic content", Seq: 0, Start: 0, End: 17}})
		require.Error(t, failure)
		require.Equal(t, "processing_index_failed", failure.Error())
		require.Equal(t, 1, vectorDeletes)
		// A fenced invocation must never issue a knowledge-wide deletion.
		require.Zero(t, engine.wholeDeletes)
		require.NoError(t, db.Model(&types.Chunk{}).Where("id IN ?", oldIDs).Count(&count).Error)
		require.Zero(t, count)
		require.NoError(t, db.Model(&types.Chunk{}).Where("id = ?", newChunk.ID).Count(&count).Error)
		require.Equal(t, int64(1), count)
		stored, err := knowledgeRepo.GetKnowledgeByID(context.Background(), 1, knowledge.ID)
		require.NoError(t, err)
		require.Equal(t, types.ParseStatusProcessing, stored.ParseStatus, "old failed worker cannot settle the new generation")
	})
}
