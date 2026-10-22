package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
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

type legacyAckLossPool struct {
	*sql.DB
	armed *atomic.Bool
}

func (p *legacyAckLossPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := p.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &legacyAckLossTx{Tx: tx, armed: p.armed}, nil
}

type legacyAckLossTx struct {
	*sql.Tx
	armed *atomic.Bool
}

func (t *legacyAckLossTx) Commit() error {
	if err := t.Tx.Commit(); err != nil {
		return err
	}
	if t.armed.Swap(false) {
		return errors.New("injected acknowledgement loss AFTER real PostgreSQL COMMIT")
	}
	return nil
}

func TestFeishuLegacyAtomicPostgres(t *testing.T) {
	dsn := feishulegacy.PrivateDSN(t)
	if dsn == "" {
		t.Skip("requires disposable feishu_atomic_test PostgreSQL/ParadeDB")
	}
	require.Contains(t, dsn, "feishu_atomic_test", "never use a shared or production database")
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	t.Chdir(root)
	require.NoError(t, database.RunMigrations(dsn))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE INDEX feishu_test_hnsw3 ON embeddings USING hnsw ((embedding::halfvec(3)) halfvec_cosine_ops) WHERE dimension=3").Error)
	tenant := &types.Tenant{ID: 809901, Name: "synthetic-feishu-atomic", Status: "active"}
	require.NoError(t, db.Create(tenant).Error)
	model := &types.Model{ID: "synthetic-1024", TenantID: tenant.ID, Name: "synthetic", Type: types.ModelTypeEmbedding}
	require.NoError(t, db.Create(model).Error)
	kb := &types.KnowledgeBase{ID: uuid.NewString(), TenantID: tenant.ID, Name: "synthetic", Type: "document", EmbeddingModelID: "synthetic-1024"}
	require.NoError(t, db.Create(kb).Error)
	ds := &types.DataSource{ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID, Name: "synthetic-Wiki", Type: "feishu", Status: "active", Config: types.JSON(`{}`)}
	require.NoError(t, db.Create(ds).Error)
	log := &types.SyncLog{ID: uuid.NewString(), DataSourceID: ds.ID, TenantID: tenant.ID, Status: types.SyncLogStatusRunning, ExecutionGeneration: 1, ExecutionClaimed: true}
	require.NoError(t, db.Create(log).Error)
	ctx := types.WithSyncExecution(context.Background(), types.SyncExecution{DataSourceID: ds.ID, SyncLogID: log.ID, TenantID: tenant.ID, Generation: 1})
	store := NewFeishuLegacyStore(db)
	stamp, err := store.ModelStamp(ctx, kb.EmbeddingModelID)
	require.NoError(t, err)
	boundMetadata, _ := json.Marshal(map[string]string{"datasource_id": ds.ID, "external_id": "same-cloud-id"})
	kid := uuid.NewString()
	baseline := &types.Knowledge{ID: kid, TenantID: tenant.ID, KnowledgeBaseID: kb.ID, Title: "RC3 existing", Type: "file", Channel: "feishu", Metadata: types.JSON(boundMetadata), FileHash: "legacy-before-enrollment", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled"}
	require.NoError(t, db.Create(baseline).Error)
	baselineChunk := uuid.NewString()
	require.NoError(t, db.Create(&types.Chunk{ID: baselineChunk, TenantID: tenant.ID, KnowledgeBaseID: kb.ID, KnowledgeID: kid, Content: "rc3baselinecanary", IsEnabled: true, ChunkType: "text"}).Error)
	require.NoError(t, db.Exec("INSERT INTO embeddings(source_id,source_type,chunk_id,knowledge_id,knowledge_base_id,content,dimension,embedding,is_enabled) VALUES(?,0,?,?,?,'rc3baselinecanary',3,'[1,0,0]'::halfvec,true)", baselineChunk, baselineChunk, kid, kb.ID).Error)
	makeVersion := func(body string, rev int) (*types.FeishuLegacyResource, *types.FeishuLegacyAttempt, types.FeishuLegacyManifest) {
		item := &types.FetchedItem{ExternalID: "same-cloud-id", UpdatedAt: time.Date(2026, 10, 21, 1, rev, 0, 0, time.UTC), Content: []byte(body), FileName: "document.md"}
		h := sha256.Sum256(item.Content)
		r, a, e := store.Begin(ctx, ds, item, hex.EncodeToString(h[:]))
		require.NoError(t, e)
		if kid == "" {
			kid = r.KnowledgeID
		}
		require.Equal(t, kid, r.KnowledgeID)
		chunk := &types.Chunk{ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID, KnowledgeID: kid, Content: body, IsEnabled: true, IndexStatus: "ready", ChunkType: "text", RelationChunks: types.JSON(`[]`), IndirectRelationChunks: types.JSON(`[]`), Metadata: types.JSON(`{}`)}
		return r, a, types.FeishuLegacyManifest{Knowledge: types.Knowledge{ID: kid, TenantID: tenant.ID, KnowledgeBaseID: kb.ID, Title: "canary", Type: "file", Channel: "feishu", FileName: "document.md", FileHash: body, Metadata: types.JSON(boundMetadata)}, Chunks: []*types.Chunk{chunk}, EmbeddingModelID: kb.EmbeddingModelID, KBConfigSHA256: LegacyKBConfigHash(kb), ModelStamp: stamp, Dimension: 3}
	}
	visible := func(word string) int64 {
		var n int64
		require.NoError(t, db.Raw("SELECT count(*) FROM embeddings WHERE knowledge_id=? AND content ||| ?", kid, word).Scan(&n).Error)
		return n
	}
	r, a, m := makeVersion("oldcanary", 1)
	require.NoError(t, store.Prepare(ctx, ds, a, m, [][]float32{{1, 0, 0}}))
	require.Zero(t, visible("oldcanary"), "private candidate leaked into public BM25")
	require.EqualValues(t, 1, visible("rc3baselinecanary"), "existing RC3 index must remain readable during staging")
	var private int64
	require.NoError(t, db.Raw("SELECT count(*) FROM feishu_legacy_index_candidates WHERE attempt_id=? AND content ||| ?", a.ID, "oldcanary").Scan(&private).Error)
	require.EqualValues(t, 1, private)
	receipt, err := store.Publish(ctx, ds, a)
	require.NoError(t, err)
	require.EqualValues(t, 1, receipt.Chunks)
	require.EqualValues(t, 1, visible("oldcanary"))
	require.Zero(t, visible("rc3baselinecanary"), "old committed version must not mix with the new version")
	_, err = store.Receipt(ctx, r, a)
	require.NoError(t, err, "lost-ACK reconciliation must use durable receipt")
	t.Run("legacy-worker-mutations-denied", func(t *testing.T) {
		for _, sql := range []string{"DELETE FROM embeddings WHERE knowledge_id=?", "UPDATE chunks SET content='stale' WHERE knowledge_id=?", "UPDATE knowledges SET title='stale' WHERE id=?"} {
			require.Error(t, db.Exec(sql, kid).Error)
		}
		require.EqualValues(t, 1, visible("oldcanary"))
	})
	t.Run("failed-candidate-preserves-current", func(t *testing.T) {
		_, next, candidate := makeVersion("invalidvector", 2)
		require.Error(t, store.Prepare(ctx, ds, next, candidate, [][]float32{{1, 0}}))
		require.EqualValues(t, 1, visible("oldcanary"))
		require.NoError(t, store.Abandon(ctx, next))
	})
	t.Run("partial-public-index-write-rolls-back", func(t *testing.T) {
		_, next, candidate := makeVersion("faultcanary", 3)
		require.NoError(t, store.Prepare(ctx, ds, next, candidate, [][]float32{{0, 1, 0}}))
		require.NoError(t, db.Exec(`CREATE FUNCTION feishu_test_fail_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.content LIKE '%faultcanary%' THEN RAISE EXCEPTION 'injected index failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER feishu_test_fail_insert BEFORE INSERT ON embeddings FOR EACH ROW EXECUTE FUNCTION feishu_test_fail_insert();`).Error)
		_, e := store.Publish(ctx, ds, next)
		require.Error(t, e)
		require.EqualValues(t, 1, visible("oldcanary"))
		require.Zero(t, visible("faultcanary"))
		var active types.FeishuLegacyResource
		require.NoError(t, db.First(&active, "id=?", r.ID).Error)
		require.Equal(t, a.ID, active.ActiveAttempt)
		require.NoError(t, db.Exec("DROP TRIGGER feishu_test_fail_insert ON embeddings; DROP FUNCTION feishu_test_fail_insert()").Error)
		require.NoError(t, store.Abandon(ctx, next))
	})
	t.Run("atomic-read-and-delayed-cleanup", func(t *testing.T) {
		_, next, candidate := makeVersion("newcanary", 4)
		require.NoError(t, store.Prepare(ctx, ds, next, candidate, [][]float32{{0, 1, 0}}))
		// A reader begun before publication keeps the complete old MVCC view.
		sqlDB, e := db.DB()
		require.NoError(t, e)
		reader, e := sqlDB.BeginTx(ctx, nil)
		require.NoError(t, e)
		_, e = reader.Exec("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ")
		require.NoError(t, e)
		var body string
		require.NoError(t, reader.QueryRow("SELECT content FROM chunks WHERE knowledge_id=$1 AND deleted_at IS NULL", kid).Scan(&body))
		require.Equal(t, "oldcanary", body)
		_, e = store.Publish(ctx, ds, next)
		require.NoError(t, e)
		require.NoError(t, reader.QueryRow("SELECT content FROM embeddings WHERE knowledge_id=$1", kid).Scan(&body))
		require.True(t, strings.Contains(body, "oldcanary"))
		require.NoError(t, reader.Rollback())
		require.Zero(t, visible("oldcanary"))
		require.EqualValues(t, 1, visible("newcanary"))
		require.Error(t, db.Exec("DELETE FROM embeddings WHERE knowledge_id=?", kid).Error, "old worker cleanup after its earlier check must be rejected")
		_, e = store.Publish(ctx, ds, a)
		require.Error(t, e, "superseded attempt cannot republish")
		_, e = store.Receipt(ctx, r, next)
		require.NoError(t, e)
	})
	t.Run("enrollment-drains-uncommitted-old-worker", func(t *testing.T) {
		other := &types.DataSource{ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID, Name: "old worker", Type: "feishu_drive", Status: "active", Config: types.JSON(`{}`)}
		require.NoError(t, db.Create(other).Error)
		log := &types.SyncLog{ID: uuid.NewString(), DataSourceID: other.ID, TenantID: tenant.ID, Status: types.SyncLogStatusRunning, ExecutionGeneration: 1, ExecutionClaimed: true}
		require.NoError(t, db.Create(log).Error)
		otherCtx := types.WithSyncExecution(context.Background(), types.SyncExecution{DataSourceID: other.ID, SyncLogID: log.ID, TenantID: tenant.ID, Generation: 1})
		oldTx := db.Begin()
		require.NoError(t, oldTx.Error)
		defer oldTx.Rollback()
		meta, _ := json.Marshal(map[string]string{"datasource_id": other.ID, "external_id": "inflight-cloud"})
		inflightKID := uuid.NewString()
		require.NoError(t, oldTx.Create(&types.Knowledge{ID: inflightKID, TenantID: tenant.ID, KnowledgeBaseID: kb.ID, Channel: "feishu", Type: "file", Title: "old worker", Metadata: types.JSON(meta), FileHash: "inflight", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled"}).Error)
		item := &types.FetchedItem{ExternalID: "inflight-cloud", Content: []byte("new version"), UpdatedAt: time.Now().UTC()}
		hash := sha256.Sum256(item.Content)
		type result struct {
			resource *types.FeishuLegacyResource
			err      error
		}
		done := make(chan result, 1)
		go func() {
			r, _, e := store.Begin(otherCtx, other, item, hex.EncodeToString(hash[:]))
			done <- result{r, e}
		}()
		require.Eventually(t, func() bool {
			var waiting int64
			_ = db.Raw("SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND NOT granted").Scan(&waiting).Error
			return waiting > 0
		}, 3*time.Second, 10*time.Millisecond)
		select {
		case <-done:
			t.Fatal("enrollment passed an uncommitted old writer")
		default:
		}
		require.NoError(t, oldTx.Commit().Error)
		select {
		case result := <-done:
			require.NoError(t, result.err)
			require.Equal(t, inflightKID, result.resource.KnowledgeID)
		case <-time.After(5 * time.Second):
			t.Fatal("enrollment failed to resume after old writer commit")
		}
	})
	t.Run("commit-ack-loss-uses-durable-receipt", func(t *testing.T) {
		resource, next, candidate := makeVersion("acklostcanary", 5)
		require.NoError(t, store.Prepare(ctx, ds, next, candidate, [][]float32{{1, 0, 0}}))
		pool, e := db.DB()
		require.NoError(t, e)
		armed := &atomic.Bool{}
		armed.Store(true)
		faultDB := db.Session(&gorm.Session{NewDB: true})
		faultPool := &legacyAckLossPool{DB: pool, armed: armed}
		faultDB.Statement = &gorm.Statement{DB: faultDB, Context: ctx, ConnPool: faultPool}
		_, e = NewFeishuLegacyStore(faultDB).Publish(ctx, ds, next)
		require.ErrorContains(t, e, "acknowledgement loss")
		require.EqualValues(t, 1, visible("acklostcanary"))
		_, e = store.Receipt(ctx, resource, next)
		require.NoError(t, e, "independent connection must reconcile actual committed state")
		_, e = store.Publish(ctx, ds, next)
		require.Error(t, e, "do not run destructive publication a second time after unknown commit")
	})
	t.Run("stale-publication-cannot-confirm-sync-cursor", func(t *testing.T) {
		checkpointCtx := types.WithLegacyReceiptSet(ctx)
		types.AddLegacyReceipt(checkpointCtx, *receipt)
		ds.LastSyncCursor = types.JSON(`{"must_not_persist":true}`)
		_, e := NewSyncLogRepository(db).(*SyncLogRepository).WriteExecution(checkpointCtx, types.SyncExecution{DataSourceID: ds.ID, SyncLogID: log.ID, TenantID: tenant.ID, Generation: 1}, ds, log, false)
		require.ErrorIs(t, e, ErrLegacyVersionFence)
		var saved types.DataSource
		require.NoError(t, db.First(&saved, "id=?", ds.ID).Error)
		require.NotContains(t, string(saved.LastSyncCursor), "must_not_persist")
	})
	t.Run("expired-owner-cannot-write-after-takeover", func(t *testing.T) {
		_, stale, candidate := makeVersion("expiredcandidate", 6)
		require.NoError(t, store.Prepare(ctx, ds, stale, candidate, [][]float32{{1, 0, 0}}))
		require.NoError(t, db.Model(&types.FeishuLegacyAttempt{}).Where("id=?", stale.ID).Update("lease_until", time.Now().Add(-time.Minute)).Error)
		_, current, next := makeVersion("takeovercanary", 7)
		require.Error(t, store.CheckOwner(ctx, ds, stale))
		require.Error(t, store.Prepare(ctx, ds, stale, candidate, [][]float32{{1, 0, 0}}))
		_, e := store.Publish(ctx, ds, stale)
		require.Error(t, e)
		require.NoError(t, store.Prepare(ctx, ds, current, next, [][]float32{{1, 0, 0}}))
		_, e = store.Publish(ctx, ds, current)
		require.NoError(t, e)
		require.NoError(t, store.Abandon(ctx, stale), "stale cleanup must not mutate current publication")
		require.EqualValues(t, 1, visible("takeovercanary"))
	})
	t.Run("model-change-blocks-staged-publication", func(t *testing.T) {
		_, attempt, candidate := makeVersion("wrongmodelcanary", 8)
		require.NoError(t, store.Prepare(ctx, ds, attempt, candidate, [][]float32{{1, 0, 0}}))
		require.NoError(t, db.Model(&types.Model{}).Where("id=?", model.ID).UpdateColumn("name", "different-binding").Error)
		_, e := store.Publish(ctx, ds, attempt)
		require.ErrorIs(t, e, ErrLegacyVersionFence)
		require.EqualValues(t, 1, visible("takeovercanary"))
		require.Zero(t, visible("wrongmodelcanary"))
		require.NoError(t, db.Model(&types.Model{}).Where("id=?", model.ID).UpdateColumn("name", model.Name).Error)
		require.NoError(t, store.Abandon(ctx, attempt))
	})
	t.Run("quota-failure-preserves-current-publication", func(t *testing.T) {
		_, attempt, candidate := makeVersion("muchlongerquotafailurecanarymustnotpublish", 9)
		require.NoError(t, store.Prepare(ctx, ds, attempt, candidate, [][]float32{{1, 0, 0}}))
		var before types.Tenant
		require.NoError(t, db.First(&before, "id=?", tenant.ID).Error)
		require.NoError(t, db.Model(&types.Tenant{}).Where("id=?", tenant.ID).UpdateColumn("storage_quota", 1).Error)
		_, e := store.Publish(ctx, ds, attempt)
		require.ErrorContains(t, e, "quota")
		require.EqualValues(t, 1, visible("takeovercanary"))
		require.Zero(t, visible("muchlongerquotafailurecanarymustnotpublish"))
		var after types.Tenant
		require.NoError(t, db.First(&after, "id=?", tenant.ID).Error)
		require.Equal(t, before.StorageUsed, after.StorageUsed, "failed publication must not alter quota accounting")
		require.NoError(t, db.Model(&types.Tenant{}).Where("id=?", tenant.ID).UpdateColumn("storage_quota", before.StorageQuota).Error)
		require.NoError(t, store.Abandon(ctx, attempt))
	})
	t.Run("persistent-uncertainty-closes-direct-history-model-and-file-reads", func(t *testing.T) {
		guardDB := types.EnableLegacyReadGuards(db.Session(&gorm.Session{NewDB: true}))
		requestScope := types.WithLegacyReadScope(context.Background())
		engine := pgretrieve.NewPostgresRetrieveEngineRepository(guardDB)
		search := func(empty bool) {
			for _, kind := range []types.RetrieverType{types.KeywordsRetrieverType, types.VectorRetrieverType} {
				rows, e := engine.Retrieve(requestScope, types.RetrieveParams{Query: "takeovercanary", Embedding: []float32{1, 0, 0}, KnowledgeBaseIDs: []string{kb.ID}, KnowledgeIDs: []string{kid}, TopK: 10, Threshold: 0, RetrieverType: kind})
				require.NoError(t, e)
				count := 0
				for _, row := range rows {
					require.NoError(t, row.Error)
					count += len(row.Results)
				}
				if empty {
					require.Zero(t, count, "held KB must be excluded inside real candidate query")
				} else {
					require.Positive(t, count)
				}
			}
		}
		search(false)
		require.NoError(t, store.SetSourceUncertain(ctx, ds, true))
		require.ErrorIs(t, legacyTenantModelGate(requestScope, guardDB, 999999), types.ErrLegacySourceRestricted,
			"cross-space model calls must recheck the actual source KB, not only the caller tenant")
		search(true)
		require.ErrorIs(t, types.CheckLegacySourceRead(guardDB, kid, kb.ID), types.ErrLegacySourceRestricted)
		var knowledge types.Knowledge
		require.ErrorIs(t, guardDB.First(&knowledge, "id=?", kid).Error, types.ErrLegacySourceRestricted)
		adminCtx := types.WithLegacyKBAdminMetadata(context.Background(), kb.ID)
		var administrativeKB types.KnowledgeBase
		require.NoError(t, guardDB.WithContext(adminCtx).First(&administrativeKB, "id=?", kb.ID).Error,
			"source administration must remain available for recovery")
		require.ErrorIs(t, guardDB.WithContext(adminCtx).First(&types.Knowledge{}, "id=?", kid).Error, types.ErrLegacySourceRestricted,
			"metadata administration must not authorize a document body")
		var chunk types.Chunk
		require.ErrorIs(t, guardDB.Where("knowledge_id=? AND deleted_at IS NULL", kid).First(&chunk).Error, types.ErrLegacySourceRestricted)
		require.ErrorIs(t, legacyTenantModelGate(context.Background(), guardDB, tenant.ID), types.ErrLegacySourceRestricted)
		require.NoError(t, store.SetSourceUncertain(ctx, ds, false))
		require.NoError(t, types.CheckLegacySourceRead(guardDB, kid, kb.ID))
		require.NoError(t, db.Exec(`CREATE FUNCTION feishu_test_redaction_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.content='' THEN RAISE EXCEPTION 'injected redaction failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER feishu_test_redaction_fault BEFORE UPDATE ON chunks FOR EACH ROW EXECUTE FUNCTION feishu_test_redaction_fault();`).Error)
		_, e := store.RestrictSource(ctx, ds, "same-cloud-id", false)
		require.Error(t, e)
		require.ErrorIs(t, types.CheckLegacySourceRead(guardDB, kid, kb.ID), types.ErrLegacySourceRestricted,
			"failed redaction must retain the independently committed deny")
		search(true)
		require.NoError(t, db.Exec("DROP TRIGGER feishu_test_redaction_fault ON chunks; DROP FUNCTION feishu_test_redaction_fault()").Error)
		terminal, e := store.RestrictSource(ctx, ds, "same-cloud-id", false)
		require.NoError(t, e)
		require.False(t, terminal, "absence is not confirmed permanent deletion")
		require.ErrorIs(t, types.CheckLegacySourceRead(guardDB, kid, kb.ID), types.ErrLegacySourceRestricted)
		require.ErrorIs(t, store.SetSourceUncertain(ctx, ds, false), types.ErrLegacySourceRestricted)
		var bodies int64
		require.NoError(t, db.Raw("SELECT count(*) FROM embeddings WHERE knowledge_id=? AND (is_enabled OR content<>'')", kid).Scan(&bodies).Error)
		require.Zero(t, bodies)
		var snapshot string
		require.NoError(t, db.Raw("SELECT quarantine_snapshot->'knowledge'->>'file_hash' FROM feishu_legacy_resources WHERE id=?", r.ID).Scan(&snapshot).Error)
		require.Equal(t, "takeovercanary", snapshot, "effective content must survive privately")
		terminal, e = store.RestrictSource(ctx, ds, "same-cloud-id", true)
		require.NoError(t, e)
		require.True(t, terminal)
		terminal, e = store.RestrictSource(ctx, ds, "same-cloud-id", true)
		require.NoError(t, e)
		require.True(t, terminal, "trusted withdrawal retry is idempotent")
	})
}
