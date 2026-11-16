package repository

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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

func TestOrdinaryEmbeddingPostgres(t *testing.T) {
	dsn := feishulegacy.PrivateDSN(t)
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	name := strings.TrimPrefix(parsed.Path, "/")
	require.True(t, strings.HasPrefix(name, "feishu_atomic_test_"))
	t.Cleanup(func() {
		admin, err := gorm.Open(postgres.Open(os.Getenv("FEISHU_LEGACY_TEST_DSN")), &gorm.Config{})
		require.NoError(t, err)
		pool, err := admin.DB()
		require.NoError(t, err)
		defer pool.Close()
		require.NoError(t, admin.Exec("DROP DATABASE "+name+" WITH (FORCE)").Error)
	})
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	t.Chdir(root)
	require.NoError(t, database.RunMigrations(dsn))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	require.NoError(t, db.Exec("CREATE INDEX ordinary_hnsw3 ON embeddings USING hnsw ((embedding::halfvec(3)) halfvec_cosine_ops) WHERE dimension=3").Error)
	tenant := &types.Tenant{ID: 809937, Name: "ordinary-private", Status: "active"}
	require.NoError(t, db.Create(tenant).Error)
	model := &types.Model{ID: uuid.NewString(), TenantID: tenant.ID, Name: "ordinary", Type: types.ModelTypeEmbedding, Status: types.ModelStatusActive}
	require.NoError(t, db.Create(model).Error)
	kb := &types.KnowledgeBase{ID: uuid.NewString(), TenantID: tenant.ID, Name: "ordinary", Type: "document", EmbeddingModelID: model.ID}
	require.NoError(t, db.Create(kb).Error)
	ctx := types.WithExecutionTenant(context.Background(), tenant.ID)
	store := &OrdinaryEmbeddingStore{db: db}
	fixture := func(t *testing.T) (*types.Knowledge, *types.TaskPendingOp, []*types.Chunk) {
		k := &types.Knowledge{ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID, Channel: "web", Type: "file", FilePath: "opaque-original-source", FileHash: "original-sha", ParseStatus: types.ParseStatusProcessing}
		require.NoError(t, db.Create(k).Error)
		require.NoError(t, db.Create(&types.KnowledgeProcessingSpan{KnowledgeID: k.ID, Attempt: 1, SpanID: uuid.NewString(), Name: "parse", Kind: "stage", Status: "running"}).Error)
		plan := types.OrdinaryEmbeddingPlan{Task: types.DocumentProcessPayload{TenantID: tenant.ID, KnowledgeID: k.ID, KnowledgeBaseID: kb.ID, Attempt: 1}, StorageBytes: 1234, Dimension: 3}
		var currentKB types.KnowledgeBase
		require.NoError(t, db.First(&currentKB, "id=?", kb.ID).Error)
		plan.KBStamp = OrdinaryEmbeddingKBStamp(&currentKB)
		chunks := make([]*types.Chunk, 1000)
		for i := range chunks {
			c := &types.Chunk{ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeID: k.ID, KnowledgeBaseID: kb.ID, Content: "unaltered text", Metadata: types.JSON(`{"z":1,"longer_key":{"b":2,"a":9007199254740993}}`), ChunkIndex: i, ChunkType: types.ChunkTypeText, IsEnabled: true}
			chunks[i] = c
			plan.Inputs = append(plan.Inputs, types.OrdinaryEmbeddingInput{ChunkID: c.ID, SHA256: OrdinaryEmbeddingChunkStamp(c)})
		}
		require.NoError(t, db.CreateInBatches(chunks, 100).Error)
		op, err := store.Start(ctx, k, plan)
		require.NoError(t, err)
		return k, op, chunks
	}
	write := func(ctx context.Context, chunks []*types.Chunk) error {
		infos := make([]*types.IndexInfo, 0, len(chunks))
		vectors := map[string][]float32{}
		for _, c := range chunks {
			infos = append(infos, &types.IndexInfo{SourceID: c.ID, SourceType: types.ChunkSourceType, ChunkID: c.ID, KnowledgeID: c.KnowledgeID, KnowledgeBaseID: c.KnowledgeBaseID, Content: c.Content, IsEnabled: true})
			vectors[c.ID] = []float32{1, 0, 0}
		}
		return pgretrieve.NewPostgresRetrieveEngineRepository(db).BatchSave(ctx, infos, map[string]any{"embedding": vectors})
	}
	guarded := func(op *types.TaskPendingOp, p types.OrdinaryEmbeddingPlan) context.Context {
		return types.WithOrdinaryEmbeddingWrite(ctx, func(ctx context.Context, indexDB *gorm.DB, fn func(*gorm.DB) error) error {
			return store.WithWrite(ctx, indexDB, op.ID, tenant.ID, p.Cursor, p.LeaseToken, fn)
		})
	}
	count := func(k *types.Knowledge) int64 {
		var n int64
		require.NoError(t, db.Table("embeddings").Where("knowledge_id=?", k.ID).Count(&n).Error)
		return n
	}

	t.Run("partial-window-restart-and-duplicate", func(t *testing.T) {
		k, op, _ := fixture(t)
		_, p, chunks, err := store.Claim(ctx, op.ID, tenant.ID, 0)
		require.NoError(t, err)
		require.Len(t, chunks, 100)
		_, _, _, err = store.Claim(ctx, op.ID, tenant.ID, 0)
		require.ErrorIs(t, err, ErrOrdinaryEmbeddingBusy)
		require.ErrorIs(t, store.Renew(ctx, op.ID, tenant.ID, 0, "foreign-token"), ErrOrdinaryEmbeddingChanged)
		require.NoError(t, write(guarded(op, p), chunks[:37]))
		require.EqualValues(t, 37, count(k))
		require.NoError(t, store.Release(ctx, op.ID, tenant.ID, 0, p.LeaseToken, false))
		restarted := &OrdinaryEmbeddingStore{db: db.Session(&gorm.Session{NewDB: true})}
		_, p2, missing, err := restarted.Claim(ctx, op.ID, tenant.ID, 0)
		require.NoError(t, err)
		require.Len(t, missing, 63)
		require.Equal(t, 2, p2.BatchAttempts)
		require.ErrorIs(t, write(guarded(op, p), chunks), ErrOrdinaryEmbeddingChanged)
		require.NoError(t, write(guarded(op, p2), missing))
		next, err := restarted.Advance(ctx, op.ID, tenant.ID, 0, p2.LeaseToken)
		require.NoError(t, err)
		nextPlan, err := DecodeOrdinaryEmbedding(next)
		require.NoError(t, err)
		require.Equal(t, 100, nextPlan.Cursor)
		require.Zero(t, nextPlan.BatchAttempts)
		require.ErrorIs(t, restarted.Release(ctx, op.ID, tenant.ID, 0, p2.LeaseToken, false), ErrOrdinaryEmbeddingChanged)
		_, _, missing, err = restarted.Claim(ctx, op.ID, tenant.ID, 100)
		require.NoError(t, err)
		require.Len(t, missing, 100)
		require.EqualValues(t, 100, count(k))
	})

	for _, change := range []string{"cancel", "new-attempt", "chunk-edit", "source-edit", "model-edit", "delete"} {
		t.Run("reject-write-after-"+change, func(t *testing.T) {
			k, op, chunks := fixture(t)
			_, p, window, err := store.Claim(ctx, op.ID, tenant.ID, 0)
			require.NoError(t, err)
			switch change {
			case "cancel":
				require.NoError(t, db.Model(k).UpdateColumn("parse_status", types.ParseStatusCancelled).Error)
			case "new-attempt":
				require.NoError(t, db.Create(&types.KnowledgeProcessingSpan{KnowledgeID: k.ID, Attempt: 2, SpanID: uuid.NewString(), Name: "parse", Kind: "stage", Status: "running"}).Error)
			case "chunk-edit":
				require.NoError(t, db.Model(chunks[0]).UpdateColumn("content", "edited").Error)
			case "source-edit":
				require.NoError(t, db.Model(k).UpdateColumn("file_hash", "replacement").Error)
			case "model-edit":
				require.NoError(t, db.Model(model).UpdateColumn("name", "changed").Error)
				defer func() { require.NoError(t, db.Model(model).UpdateColumn("name", "ordinary").Error) }()
			case "delete":
				require.NoError(t, db.Delete(k).Error)
			}
			require.Error(t, write(guarded(op, p), window))
			require.Zero(t, count(k))
			require.NoError(t, store.Release(ctx, op.ID, tenant.ID, 0, p.LeaseToken, true))
			if change == "cancel" || change == "new-attempt" {
				var fresh types.Knowledge
				require.NoError(t, db.First(&fresh, "id=?", k.ID).Error)
				if change == "cancel" {
					require.Equal(t, types.ParseStatusCancelled, fresh.ParseStatus)
				} else {
					require.Equal(t, types.ParseStatusProcessing, fresh.ParseStatus)
				}
			}
		})
	}

	t.Run("bounded-attempts-survive-restart", func(t *testing.T) {
		k, op, _ := fixture(t)
		for i := 1; i <= 4; i++ {
			restarted := &OrdinaryEmbeddingStore{db: db}
			_, p, _, err := restarted.Claim(ctx, op.ID, tenant.ID, 0)
			require.NoError(t, err)
			require.Equal(t, i, p.BatchAttempts)
			require.NoError(t, restarted.Release(ctx, op.ID, tenant.ID, 0, p.LeaseToken, false))
		}
		_, _, _, err := store.Claim(ctx, op.ID, tenant.ID, 0)
		require.Error(t, err)
		var fresh types.Knowledge
		require.NoError(t, db.First(&fresh, "id=?", k.ID).Error)
		require.Equal(t, types.ParseStatusFailed, fresh.ParseStatus)
		require.NotEmpty(t, fresh.ErrorMessage)
	})

	t.Run("expired-lease-and-external-pool-fenced", func(t *testing.T) {
		_, op, _ := fixture(t)
		_, p, chunks, err := store.Claim(ctx, op.ID, tenant.ID, 0)
		require.NoError(t, err)
		other, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
		require.NoError(t, err)
		otherPool, err := other.DB()
		require.NoError(t, err)
		defer otherPool.Close()
		require.ErrorIs(t, store.WithWrite(ctx, other, op.ID, tenant.ID, 0, p.LeaseToken, func(*gorm.DB) error { t.Fatal("foreign pool reached write"); return nil }), ErrOrdinaryEmbeddingChanged)
		p.LeaseUntil = time.Now().Add(-time.Second)
		data, err := json.Marshal(p)
		require.NoError(t, err)
		require.NoError(t, db.Model(op).UpdateColumn("payload", json.RawMessage(data)).Error)
		require.Error(t, write(guarded(op, p), chunks))
		_, next, _, err := store.Claim(ctx, op.ID, tenant.ID, 0)
		require.NoError(t, err)
		require.NotEqual(t, p.LeaseToken, next.LeaseToken)
		require.Equal(t, 2, next.BatchAttempts)
	})

	t.Run("finish-requires-complete-manifest-and-charges-once", func(t *testing.T) {
		k, op, _ := fixture(t)
		var before types.Tenant
		require.NoError(t, db.First(&before, "id=?", tenant.ID).Error)
		for cursor := 0; cursor < 1000; cursor += 100 {
			_, p, chunks, err := store.Claim(ctx, op.ID, tenant.ID, cursor)
			require.NoError(t, err)
			require.NoError(t, write(guarded(op, p), chunks))
			_, err = store.Advance(ctx, op.ID, tenant.ID, cursor, p.LeaseToken)
			require.NoError(t, err)
		}
		finished, err := store.Finish(ctx, op.ID, tenant.ID)
		require.NoError(t, err)
		require.Equal(t, types.ParseStatusProcessing, finished.ParseStatus)
		require.EqualValues(t, 1234, finished.StorageSize)
		_, err = store.Finish(ctx, op.ID, tenant.ID)
		require.NoError(t, err)
		var after types.Tenant
		require.NoError(t, db.First(&after, "id=?", tenant.ID).Error)
		require.EqualValues(t, before.StorageUsed+1234, after.StorageUsed)
		require.EqualValues(t, 1000, count(k))
		require.NoError(t, store.DispatchedPostprocess(ctx, op.ID, tenant.ID))
	})

	t.Run("missing-vector-does-not-advance", func(t *testing.T) {
		_, op, _ := fixture(t)
		_, p, chunks, err := store.Claim(ctx, op.ID, tenant.ID, 0)
		require.NoError(t, err)
		require.NoError(t, write(guarded(op, p), chunks[:99]))
		_, err = store.Advance(ctx, op.ID, tenant.ID, 0, p.LeaseToken)
		require.ErrorIs(t, err, ErrOrdinaryEmbeddingChanged)
	})
}

func TestOrdinaryEmbeddingKBStampNormalizesWithoutTransientState(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 7, Type: types.KnowledgeBaseTypeDocument, EmbeddingModelID: "model", ExtractConfig: &types.ExtractConfig{Enabled: true}}
	raw := OrdinaryEmbeddingKBStamp(kb)
	require.False(t, kb.IndexingStrategy.GraphEnabled, "hashing must not mutate the caller")
	kb.EnsureDefaults()
	require.Equal(t, raw, OrdinaryEmbeddingKBStamp(kb))
	kb.KnowledgeCount = 19
	kb.ChunkCount = 100
	kb.ProcessingCount = 2
	kb.Description = "generated description"
	kb.UpdatedAt = time.Now()
	require.Equal(t, raw, OrdinaryEmbeddingKBStamp(kb))
	kb.EmbeddingModelID = "replacement"
	require.NotEqual(t, raw, OrdinaryEmbeddingKBStamp(kb))
	kb.EmbeddingModelID = "model"
	kb.VLMConfig.APIKey = "changed-secret"
	require.NotEqual(t, raw, OrdinaryEmbeddingKBStamp(kb), "API redaction must not hide configuration changes")
}

func TestOrdinaryEmbeddingStampCanonicalJSONRetainsLargeIntegers(t *testing.T) {
	chunk := &types.Chunk{ID: "chunk", Metadata: types.JSON(`{"z":1,"longer_key":{"b":2,"a":9007199254740993}}`)}
	stamp := OrdinaryEmbeddingChunkStamp(chunk)
	chunk.Metadata = types.JSON(`{"longer_key":{"a":9007199254740993,"b":2},"z":1}`)
	require.Equal(t, stamp, OrdinaryEmbeddingChunkStamp(chunk))
	chunk.Metadata = types.JSON(`{"longer_key":{"a":9007199254740992,"b":2},"z":1}`)
	require.NotEqual(t, stamp, OrdinaryEmbeddingChunkStamp(chunk))
}
