package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	pgretrieve "github.com/Tencent/WeKnora/internal/application/repository/retriever/postgres"
	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type ordinaryTestTenant struct {
	interfaces.TenantRepository
	tenant *types.Tenant
}

func (r ordinaryTestTenant) GetTenantByID(context.Context, uint64) (*types.Tenant, error) {
	return r.tenant, nil
}

type ordinaryTestModel struct {
	parentChildEmbedder
	calls atomic.Int32
	fail  bool
}

func (m *ordinaryTestModel) GetDimensions() int { return 3 }
func (m *ordinaryTestModel) BatchEmbedWithPool(ctx context.Context, _ embedding.Embedder, texts []string) ([][]float32, error) {
	m.calls.Add(1)
	if m.fail {
		return nil, errors.New("provider unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = []float32{1, 0, 0}
	}
	return out, nil
}

func TestOrdinaryEmbeddingServicePostgres(t *testing.T) {
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
	require.NoError(t, db.Exec("CREATE INDEX ordinary_service_hnsw3 ON embeddings USING hnsw ((embedding::halfvec(3)) halfvec_cosine_ops) WHERE dimension=3").Error)
	tenant := &types.Tenant{ID: 809938, Name: "ordinary-service", Status: "active", RetrieverEngines: types.RetrieverEngines{Engines: []types.RetrieverEngineParams{{RetrieverEngineType: types.PostgresRetrieverEngineType, RetrieverType: types.VectorRetrieverType}}}}
	require.NoError(t, db.Create(tenant).Error)
	modelRow := &types.Model{ID: uuid.NewString(), TenantID: tenant.ID, Name: "ordinary", Type: types.ModelTypeEmbedding, Status: types.ModelStatusActive}
	require.NoError(t, db.Create(modelRow).Error)
	kb := &types.KnowledgeBase{ID: uuid.NewString(), TenantID: tenant.ID, Name: "ordinary", Type: "document", EmbeddingModelID: modelRow.ID}
	require.NoError(t, db.Create(kb).Error)
	require.NoError(t, db.First(kb, "id=?", kb.ID).Error)
	base := repository.NewKnowledgeRepository(db)
	store := base.(interface {
		OrdinaryEmbeddingStore() *repository.OrdinaryEmbeddingStore
	}).OrdinaryEmbeddingStore()
	ctx := types.WithExecutionTenant(context.Background(), tenant.ID)
	k := &types.Knowledge{ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID, Type: "file", Channel: "web", FilePath: "original-source", FileHash: "original-sha", ParseStatus: types.ParseStatusProcessing}
	require.NoError(t, db.Create(k).Error)
	require.NoError(t, db.Create(&types.KnowledgeProcessingSpan{KnowledgeID: k.ID, Attempt: 1, SpanID: uuid.NewString(), Name: "parse", Kind: "stage", Status: "running"}).Error)
	kb.EnsureDefaults()
	plan := types.OrdinaryEmbeddingPlan{Task: types.DocumentProcessPayload{TenantID: tenant.ID, KnowledgeID: k.ID, KnowledgeBaseID: kb.ID, Attempt: 1}, StorageBytes: 1234, Dimension: 3, KBStamp: repository.OrdinaryEmbeddingKBStamp(kb)}
	chunks := make([]*types.Chunk, 1000)
	for i := range chunks {
		c := &types.Chunk{ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeID: k.ID, KnowledgeBaseID: kb.ID, Content: "original row", ChunkIndex: i, ChunkType: types.ChunkTypeText, IsEnabled: true}
		chunks[i] = c
		plan.Inputs = append(plan.Inputs, types.OrdinaryEmbeddingInput{ChunkID: c.ID, SHA256: repository.OrdinaryEmbeddingChunkStamp(c)})
	}
	require.NoError(t, db.CreateInBatches(chunks, 100).Error)
	op, err := store.Start(ctx, k, plan)
	require.NoError(t, err)
	queue := &dispatchQueue{down: true}
	svc := &knowledgeService{repo: base, task: queue}
	require.Error(t, svc.publishOrdinaryEmbedding(ctx, op))
	parentBytes, err := json.Marshal(plan.Task)
	require.NoError(t, err)
	// No parser, file service or tenant service is installed: a duplicate
	// parent must resume the committed intent without touching any of them.
	require.Error(t, svc.ProcessDocument(ctx, asynq.NewTask(types.TypeDocumentProcess, parentBytes)))
	queue.setDown(false)
	svc = &knowledgeService{repo: base, task: queue, personalRecovery: &deadlinePersonalReconciler{reconcile: func(context.Context) error {
		require.NotEmpty(t, queue.snapshot(), "ordinary delivery must precede a failing personal reconciliation")
		return context.DeadlineExceeded
	}}}
	require.ErrorIs(t, svc.RecoverKnowledgeDispatches(ctx), context.DeadlineExceeded)
	tasks := queue.snapshot()
	require.Len(t, tasks, 1)
	var payload types.DocumentProcessPayload
	require.NoError(t, json.Unmarshal(tasks[0].Payload(), &payload))
	require.Equal(t, op.ID, payload.OrdinaryEmbeddingID)
	require.Zero(t, payload.EmbeddingCursor)

	redisFixture := miniredis.RunT(t)
	client := asynq.NewClient(asynq.RedisClientOpt{Addr: redisFixture.Addr()})
	defer client.Close()
	inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: redisFixture.Addr()})
	defer inspector.Close()
	svc.task = client
	require.NoError(t, svc.publishOrdinaryEmbedding(ctx, op))
	require.NoError(t, svc.publishOrdinaryEmbedding(ctx, op))
	pending, err := inspector.ListPendingTasks(types.QueueDefault)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, 3, pending[0].MaxRetry)

	// Real hybrid indexer + PG repository, bounded synthetic provider. Each
	// restart advances the same original attempt without reparsing its rows.
	emb := &ordinaryTestModel{}
	engine := retriever.NewKVHybridRetrieveEngine(pgretrieve.NewPostgresRetrieveEngineRepository(db), types.PostgresRetrieverEngineType)
	for cursor := 0; cursor < 1000; cursor += 100 {
		svc = &knowledgeService{repo: base, task: queue, tenantRepo: ordinaryTestTenant{tenant: tenant}, kbService: dispatchActualKB{kb: kb}, modelService: parentChildModelService{embedder: emb}, retrieveEngine: parentChildRetrieveRegistry{engine: engine}}
		payload.EmbeddingCursor = cursor
		require.NoError(t, svc.processOrdinaryEmbedding(ctx, payload))
		// An old delivery must not re-run the provider or rewind the cursor.
		calls := emb.calls.Load()
		require.NoError(t, svc.processOrdinaryEmbedding(ctx, payload))
		require.Equal(t, calls, emb.calls.Load())
	}
	var vectors int64
	require.NoError(t, db.Table("embeddings").Where("knowledge_id=? AND is_enabled", k.ID).Count(&vectors).Error)
	require.EqualValues(t, 1000, vectors)
	finalOp, err := store.Find(ctx, tenant.ID, k.ID, 1)
	require.NoError(t, err)
	require.Equal(t, "finalizing", finalOp.Op)
	var fresh types.Knowledge
	require.NoError(t, db.First(&fresh, "id=?", k.ID).Error)
	require.Equal(t, types.ParseStatusProcessing, fresh.ParseStatus)
	var textRows int64
	require.NoError(t, db.Model(&types.Chunk{}).Where("knowledge_id=?", k.ID).Count(&textRows).Error)
	require.EqualValues(t, 1000, textRows)
	queued := queue.snapshot()
	posts := 0
	for _, task := range queued {
		if task.Type() == types.TypeKnowledgePostProcess {
			posts++
		}
	}
	require.Equal(t, 1, posts, "normal post-processing owns completion exactly once")
	// If Redis loses the postprocess task, the durable finalizing intent
	// must still re-publish its continuation rather than becoming terminal.
	require.NoError(t, svc.recoverOrdinaryEmbeddings(ctx))
	latest := queue.snapshot()
	require.Equal(t, types.TypeDocumentProcess, latest[len(latest)-1].Type())
	// The normal postprocessor's admission, not enqueue success, closes it.
	require.NoError(t, db.Model(k).UpdateColumn("parse_status", types.ParseStatusFinalizing).Error)
	payload.EmbeddingCursor = 1000
	require.NoError(t, svc.processOrdinaryEmbedding(ctx, payload))
	finalOp, err = store.Find(ctx, tenant.ID, k.ID, 1)
	require.NoError(t, err)
	require.Equal(t, "completed", finalOp.Op)
	// Terminal parent duplicates may not reset a failed/cancelled status.
	require.NoError(t, db.Model(k).UpdateColumn("parse_status", types.ParseStatusCancelled).Error)
	require.NoError(t, svc.ProcessDocument(ctx, asynq.NewTask(types.TypeDocumentProcess, parentBytes)))
	require.NoError(t, db.First(&fresh, "id=?", k.ID).Error)
	require.Equal(t, types.ParseStatusCancelled, fresh.ParseStatus)
	require.NoError(t, store.Reject(ctx, op.ID, tenant.ID, 1000))
	finalOp, err = store.Find(ctx, tenant.ID, k.ID, 1)
	require.NoError(t, err)
	require.Equal(t, "completed", finalOp.Op)
}
