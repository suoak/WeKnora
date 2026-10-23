package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	pgretrieve "github.com/Tencent/WeKnora/internal/application/repository/retriever/postgres"
	"github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestFeishuParentAtomicPostgresP1(t *testing.T) {
	dsn := feishulegacy.PrivateDSN(t)
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	t.Chdir(root)
	require.NoError(t, database.RunMigrations(dsn))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE INDEX feishu_parent_hnsw3 ON embeddings USING hnsw ((embedding::halfvec(3)) halfvec_cosine_ops) WHERE dimension=3").Error)
	tenant := &types.Tenant{ID: 809903, Name: "parent-version-synthetic", Status: "active"}
	require.NoError(t, db.Create(tenant).Error)
	model := &types.Model{ID: uuid.NewString(), TenantID: tenant.ID, Name: "synthetic", Type: types.ModelTypeEmbedding}
	require.NoError(t, db.Create(model).Error)
	kb := &types.KnowledgeBase{ID: uuid.NewString(), TenantID: tenant.ID, Name: "parent", Type: "document", EmbeddingModelID: model.ID}
	kb.ChunkingConfig.EnableParentChild = true
	require.NoError(t, db.Create(kb).Error)
	ds := &types.DataSource{ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID, Type: "feishu", Status: "active", Config: types.JSON(`{}`)}
	require.NoError(t, db.Create(ds).Error)
	log := &types.SyncLog{ID: uuid.NewString(), DataSourceID: ds.ID, TenantID: tenant.ID, Status: types.SyncLogStatusRunning, ExecutionGeneration: 1, ExecutionClaimed: true}
	require.NoError(t, db.Create(log).Error)
	ctx := types.WithSyncExecution(context.Background(), types.SyncExecution{DataSourceID: ds.ID, SyncLogID: log.ID, TenantID: tenant.ID, Generation: 1})
	store := NewFeishuLegacyStore(db)
	stamp, err := store.ModelStamp(ctx, model.ID)
	require.NoError(t, err)
	item := func(rev int) *types.FetchedItem {
		return &types.FetchedItem{ExternalID: "parent-cloud", UpdatedAt: time.Date(2026, 10, 23, 1, rev, 0, 0, time.UTC)}
	}
	hash := func(body string) string { h := sha256.Sum256([]byte(body)); return hex.EncodeToString(h[:]) }
	begin := func(body string, rev int) (*types.FeishuLegacyResource, *types.FeishuLegacyAttempt, types.FeishuLegacyManifest) {
		r, a, e := store.Begin(ctx, ds, item(rev), hash(body))
		require.NoError(t, e)
		metadata, _ := json.Marshal(map[string]string{"datasource_id": ds.ID, "external_id": "parent-cloud"})
		parent := &types.Chunk{ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID, KnowledgeID: r.KnowledgeID, Content: body + " parent context", ChunkType: types.ChunkTypeParentText, IsEnabled: true, Metadata: types.JSON(`{}`)}
		chunks := []*types.Chunk{parent}
		for i := 0; i < 2; i++ {
			chunks = append(chunks, &types.Chunk{ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID, KnowledgeID: r.KnowledgeID, Content: fmt.Sprintf("%s child %d", body, i), ChunkType: types.ChunkTypeText, ParentChunkID: parent.ID, IsEnabled: true, ChunkIndex: i, Metadata: types.JSON(`{}`)})
		}
		chunks[1].NextChunkID = chunks[2].ID
		chunks[2].PreChunkID = chunks[1].ID
		return r, a, types.FeishuLegacyManifest{Knowledge: types.Knowledge{ID: r.KnowledgeID, TenantID: tenant.ID, KnowledgeBaseID: kb.ID, Title: "parent synthetic", Type: "file", Channel: "feishu", FileHash: body, Metadata: types.JSON(metadata)}, Chunks: chunks, EmbeddingModelID: model.ID, KBConfigSHA256: LegacyKBConfigHash(kb), ModelStamp: stamp, Dimension: 3}
	}
	vectors := [][]float32{{1, 0, 0}, {0, 1, 0}}
	r, a, m := begin("oldparentcanary", 1)
	_, _, err = store.Begin(ctx, ds, item(1), hash("ambiguous-first-attempt"))
	require.ErrorIs(t, err, ErrLegacyVersionFence, "unpublished initial high-water also binds one hash")
	require.NoError(t, store.Prepare(ctx, ds, a, m, vectors))
	var n int64
	require.NoError(t, db.Model(&types.Chunk{}).Where("knowledge_id=?", r.KnowledgeID).Count(&n).Error)
	require.Zero(t, n, "staged graph is private")
	receipt, err := store.Publish(ctx, ds, a)
	require.NoError(t, err)
	require.EqualValues(t, 3, receipt.Chunks)
	require.EqualValues(t, 2, receipt.Vectors)
	require.EqualValues(t, 2, receipt.BM25Rows)
	_, err = store.Receipt(ctx, r, a)
	require.NoError(t, err)
	snapshot := func() string {
		var raw string
		require.NoError(t, db.Raw("SELECT jsonb_build_object('k',(SELECT to_jsonb(k) FROM knowledges k WHERE id=?),'c',(SELECT jsonb_agg(to_jsonb(c) ORDER BY c.id) FROM chunks c WHERE knowledge_id=?),'e',(SELECT jsonb_agg(to_jsonb(e) ORDER BY e.id) FROM embeddings e WHERE knowledge_id=?))::text", r.KnowledgeID, r.KnowledgeID, r.KnowledgeID).Scan(&raw).Error)
		return raw
	}
	before := snapshot()
	_, next, candidate := begin("newparentcanary", 2)
	_, _, err = store.Begin(ctx, ds, item(3), hash("oldparentcanary"))
	require.ErrorIs(t, err, ErrLegacyVersionBusy, "active duplicate must not move watermark past a live unpublished candidate")
	var watermark string
	require.NoError(t, db.Raw("SELECT source_revision FROM feishu_legacy_resources WHERE id=?", r.ID).Scan(&watermark).Error)
	require.Equal(t, item(2).UpdatedAt.Format(time.RFC3339Nano), watermark)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _, e := store.Begin(ctx, ds, item(2), hash("newparentcanary")); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.ErrorIs(t, e, ErrLegacyVersionBusy)
	}
	broken := candidate
	broken.Chunks = append([]*types.Chunk(nil), candidate.Chunks...)
	child := *broken.Chunks[1]
	child.ParentChunkID = m.Chunks[0].ID
	broken.Chunks[1] = &child
	require.ErrorIs(t, store.Prepare(ctx, ds, next, broken, vectors), ErrLegacyVersionFence)
	require.JSONEq(t, before, snapshot())
	require.NoError(t, store.Abandon(ctx, next))
	previousOwner := next
	_, next, candidate = begin("newparentcanary", 2)
	require.ErrorIs(t, store.Prepare(ctx, ds, previousOwner, candidate, vectors), ErrLegacyVersionFence)
	require.NoError(t, store.Prepare(ctx, ds, next, candidate, vectors))
	// Fail after public replacement has started: the whole graph and indexes
	// must roll back to precisely the previously committed version.
	require.NoError(t, db.Exec(`CREATE FUNCTION parent_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.content LIKE '%newparentcanary%' THEN RAISE EXCEPTION 'synthetic parent publication failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER parent_fail BEFORE INSERT ON embeddings FOR EACH ROW EXECUTE FUNCTION parent_fail();`).Error)
	_, err = store.Publish(ctx, ds, next)
	require.Error(t, err)
	require.JSONEq(t, before, snapshot())
	require.NoError(t, db.Exec("DROP TRIGGER parent_fail ON embeddings; DROP FUNCTION parent_fail()").Error)
	// A ready attempt remains safely retryable without reusing another owner.
	sqlDB, err := db.DB()
	require.NoError(t, err)
	reader, err := sqlDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer reader.Rollback()
	_, err = reader.Exec("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ")
	require.NoError(t, err)
	var oldParent string
	require.NoError(t, reader.QueryRow("SELECT content FROM chunks WHERE id=$1 AND deleted_at IS NULL", m.Chunks[0].ID).Scan(&oldParent))
	receipt, err = store.Publish(ctx, ds, next)
	require.NoError(t, err)
	require.EqualValues(t, 3, receipt.Chunks)
	require.NoError(t, reader.QueryRow("SELECT content FROM chunks WHERE id=$1 AND deleted_at IS NULL", m.Chunks[0].ID).Scan(&oldParent))
	require.Contains(t, oldParent, "oldparentcanary")
	require.NoError(t, reader.Rollback())
	require.NoError(t, db.Raw("SELECT count(*) FROM chunks WHERE id=? AND deleted_at IS NULL", m.Chunks[0].ID).Scan(&n).Error)
	require.Zero(t, n)
	require.NoError(t, db.Raw("SELECT count(*) FROM chunks c LEFT JOIN chunks p ON p.id=c.parent_chunk_id WHERE c.knowledge_id=? AND c.deleted_at IS NULL AND c.chunk_type='text' AND (p.id IS NULL OR p.deleted_at IS NOT NULL OR p.chunk_type<>'parent_text' OR p.knowledge_id<>c.knowledge_id)", r.KnowledgeID).Scan(&n).Error)
	require.Zero(t, n)
	require.NoError(t, db.Raw("SELECT count(*) FROM embeddings WHERE knowledge_id=? AND content ||| 'newparentcanary'", r.KnowledgeID).Scan(&n).Error)
	require.EqualValues(t, 2, n)
	require.NoError(t, db.Raw("SELECT count(*) FROM embeddings e JOIN chunks c ON c.id=e.chunk_id WHERE c.knowledge_id=? AND c.chunk_type='parent_text'", r.KnowledgeID).Scan(&n).Error)
	require.Zero(t, n)
	engine := pgretrieve.NewPostgresRetrieveEngineRepository(types.EnableLegacyReadGuards(db))
	for _, kind := range []types.RetrieverType{types.KeywordsRetrieverType, types.VectorRetrieverType} {
		rows, e := engine.Retrieve(types.WithLegacyReadScope(ctx), types.RetrieveParams{Query: "newparentcanary", Embedding: []float32{1, 0, 0}, KnowledgeBaseIDs: []string{kb.ID}, KnowledgeIDs: []string{r.KnowledgeID}, TopK: 10, Threshold: 0, RetrieverType: kind})
		require.NoError(t, e)
		found := 0
		for _, row := range rows {
			require.NoError(t, row.Error)
			for _, hit := range row.Results {
				found++
				require.NotEqual(t, candidate.Chunks[0].ID, hit.ChunkID)
				require.Contains(t, hit.Content, "newparentcanary")
			}
		}
		require.Positive(t, found, "real BM25 and vector engines must retrieve only current children")
	}
	_, err = store.Publish(ctx, ds, a)
	require.ErrorIs(t, err, ErrLegacyVersionFence)
	_, stale, third := begin("expiredparentcanary", 3)
	require.NoError(t, db.Model(&types.FeishuLegacyAttempt{}).Where("id=?", stale.ID).UpdateColumn("lease_until", time.Now().Add(-time.Minute)).Error)
	err = store.Prepare(ctx, ds, stale, third, vectors)
	require.ErrorIs(t, err, ErrLegacyVersionFence)
	var diagnostic *LegacyFenceError
	require.ErrorAs(t, err, &diagnostic)
	require.Equal(t, "attempt_lease_expired", diagnostic.Reason)
	_, fresh, fourth := begin("freshparentcanary", 4)
	require.NoError(t, store.Prepare(ctx, ds, fresh, fourth, vectors))
	// A new sync execution generation invalidates even a live resource lease.
	require.NoError(t, db.Model(log).UpdateColumn("execution_generation", 2).Error)
	_, err = store.Publish(ctx, ds, fresh)
	require.ErrorAs(t, err, &diagnostic)
	require.Equal(t, "execution_generation_or_claim_lost", diagnostic.Reason)
	ctx = types.WithSyncExecution(context.Background(), types.SyncExecution{DataSourceID: ds.ID, SyncLogID: log.ID, TenantID: tenant.ID, Generation: 2})
	require.NoError(t, store.Abandon(ctx, fresh))
	_, final, finalCandidate := begin("finalparentcanary", 5)
	require.NoError(t, store.Prepare(ctx, ds, final, finalCandidate, vectors))
	_, err = store.Publish(ctx, ds, final)
	require.NoError(t, err)
	_, _, err = store.Begin(ctx, ds, item(3), hash("rollback"))
	require.ErrorAs(t, err, &diagnostic)
	require.Equal(t, "source_revision_regressed", diagnostic.Reason)
}
