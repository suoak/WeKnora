//go:build anydoc && personal_exact_live

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	pgretrieve "github.com/Tencent/WeKnora/internal/application/repository/retriever/postgres"
	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/models/rerank"
	"github.com/Tencent/WeKnora/internal/reranking"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/hibiken/asynq"
	"github.com/panjf2000/ants/v2"
	"github.com/stretchr/testify/require"
)

type exactAcceptanceModels struct {
	*v2PipelineModel
	summary  chat.Chat
	recovery *PersonalRecovery
}

func (m *exactAcceptanceModels) GetChatModel(context.Context, string) (chat.Chat, error) {
	return m.summary, nil
}

// Acceptance of the actual public consumer and PG writer, using original
// bytes/identities and a synthetic HTTP embedding provider. This is explicitly
// NOT real-provider, atomic-publication or authorized-serving acceptance.
func exerciseOriginalExactConsumer(
	t *testing.T, f *personalRecoveryFixture, payload types.DocumentProcessPayload, proof *personalExactDocument,
) *personalExactParsedDocument {
	t.Helper()
	provider, tenant, calls := configureOriginalExactConsumer(t, f, proof)
	var err error
	dispatch := f.ks.repo.(interfaces.KnowledgeDispatchRepository)
	require.NoError(t, dispatch.SetKnowledgeDispatchState(context.Background(), 192370, "processing"))
	owner := mustPersonalAttempt(t, f, proof.attempt)
	// Renew through the real fenced service; no lease SQL or nonce reuse.
	require.NoError(t, f.p.mutate(context.Background(), owner, 2*time.Minute, nil))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if f.p.mutate(ctx, owner, 2*time.Minute, nil) != nil {
					cancel()
					return
				}
			}
		}
	}()
	defer func() { cancel(); <-stopped }()
	ctx, err = f.p.exactConsumerContext(ctx, proof)
	require.NoError(t, err)
	require.NoError(t, f.p.beginExactPersonalReceipt(ctx, proof))
	ctx = context.WithValue(ctx, personalFirstCreateContextKey{}, true)
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)
	bytes, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, f.ks.processDocument(ctx, asynq.NewTask(types.TypeDocumentProcess, bytes)))
	consumer, ok := exactPersonalConsumerFrom(ctx)
	require.True(t, ok)
	require.True(t, consumer.indexed)
	require.NotNil(t, consumer.parsed)
	chunks, err := f.ks.chunkRepo.ListAllChunksByKnowledgeID(ctx, proof.tenant, proof.knowledge)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)
	require.NoError(t, consumer.verifyParserChunks(chunks))
	for _, chunk := range chunks {
		require.False(t, chunk.IsEnabled, "staging rows must not become visible before publication")
	}
	var vectors, visible int64
	require.NoError(t, f.db.Table("embeddings").Where("knowledge_id=? AND dimension=1024 AND embedding IS NOT NULL",
		proof.knowledge).Count(&vectors).Error)
	require.Positive(t, vectors)
	require.Positive(t, calls.Load())
	require.NoError(
		t,
		f.db.Table("embeddings").Where("knowledge_id=? AND is_enabled", proof.knowledge).Count(&visible).Error,
	)
	require.Zero(t, visible)
	t.Logf("original consumer staging verified: chunks=%d vectors=%d provider_requests=%d; not published",
		len(chunks), vectors, calls.Load())
	evidence := map[string]any{
		"knowledge_id": proof.knowledge, "chunk_rows": len(chunks),
		"vector_rows": vectors, "embedding_provider": "synthetic HTTP; real client and PostgreSQL writes",
		"consumer_indexing_verified": true, "atomic_publication_verified": false, "retrieval_verified": false,
		"production_writes": false,
	}
	data, err := json.Marshal(evidence)
	require.NoError(t, err)
	path := fmt.Sprintf("%s.consumer.json", os.Getenv("PERSONAL_EXACT_EVIDENCE_OUT"))
	require.NoError(t, os.WriteFile(path, data, 0o600))
	if os.Getenv("PERSONAL_EXACT_PUBLICATION_ACCEPTANCE") == "1" {
		exerciseOriginalExactPublication(t, f, ctx, consumer, provider.URL)
	}
	return consumer.parsed
}

// Configuration only: this helper neither issues proof nor claims an attempt.
func configureOriginalExactConsumer(
	t *testing.T, f *personalRecoveryFixture, proof *personalExactDocument,
) (*httptest.Server, *types.Tenant, *atomic.Int64) {
	t.Helper()
	require.NoError(t, f.db.AutoMigrate(&types.KnowledgeBase{}, &types.Tenant{}, &types.Model{}))
	ddl, err := os.ReadFile("../../../migrations/versioned/000002_embeddings.up.sql")
	require.NoError(t, err)
	require.NoError(t, f.db.Exec(string(ddl)).Error)
	require.NoError(t, f.db.Exec("ALTER TABLE embeddings ADD COLUMN IF NOT EXISTS tag_id varchar(36)").Error)
	// The production migration only attaches this trigger when public.embeddings
	// exists. This isolated schema owns its table, so attach the same function here.
	require.NoError(t, f.db.Exec(`CREATE TRIGGER feishu_legacy_embedding_fence
 BEFORE INSERT OR UPDATE OR DELETE ON embeddings FOR EACH ROW
 EXECUTE FUNCTION knowhub_feishu_legacy_write_fence()`).Error)

	calls := &atomic.Int64{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/chat/completions" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "isolated-summary", "object": "chat.completion", "model": "gpt-4o-mini",
				"choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{
					"role": "assistant", "content": `{"summary":"Isolated original-file recovery acceptance summary."}`,
				}}},
			})
			return
		}
		if r.URL.Path != "/v1/embeddings" {
			http.NotFound(w, r)
			return
		}
		var request struct {
			Input []string `json:"input"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || len(request.Input) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		calls.Add(1)
		var rows []any
		for i := range request.Input {
			vector := make([]float32, 1024)
			vector[0] = 1
			rows = append(rows, map[string]any{"object": "embedding", "index": i, "embedding": vector})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": rows, "model": "isolated-fixture"})
	}))
	t.Cleanup(provider.Close)
	model := types.Model{
		ID: "exact-embedding", TenantID: proof.tenant,
		Name: "isolated-fixture", Type: types.ModelTypeEmbedding, Source: types.ModelSourceRemote,
	}
	model.Parameters.BaseURL = provider.URL + "/v1"
	model.Parameters.Provider = "generic"
	model.Parameters.EmbeddingParameters.Dimension = 1024

	if fixture := loadOriginalAcceptanceModels(t); fixture != nil {
		model.Name, model.Source = fixture.Embedding.Name, fixture.Embedding.Source
		model.Parameters = fixture.Embedding.Parameters
		require.Equal(
			t,
			1024,
			model.Parameters.EmbeddingParameters.Dimension,
			"this fixture uses the configured 1024-dimensional store",
		)
	}
	require.NoError(t, f.db.Create(&model).Error)
	kb := &types.KnowledgeBase{
		ID: proof.kb, TenantID: proof.tenant, CreatorID: "A", Type: "document",
		EmbeddingModelID: model.ID, ChunkingConfig: types.ChunkingConfig{ChunkSize: 512, ChunkOverlap: 50},
		IndexingStrategy: types.IndexingStrategy{VectorEnabled: true, KeywordEnabled: true},
	}
	require.NoError(t, f.db.Model(&types.KnowledgeBase{}).Where("id=?", proof.kb).Updates(map[string]any{
		"type": kb.Type, "embedding_model_id": kb.EmbeddingModelID,
		"chunking_config": kb.ChunkingConfig, "indexing_strategy": kb.IndexingStrategy,
	}).Error)
	workers, err := ants.NewPool(1)
	require.NoError(t, err)
	t.Cleanup(workers.Release)
	f.ks.config = &config.Config{Conversation: &config.ConversationConfig{
		GenerateSummaryPrompt: "Summarize only the verified document. Return a JSON summary.",
	}}
	f.ks.modelService = &v2PipelineModel{
		legacyLiveModel: legacyLiveModel{db: f.db},
		pool:            embedding.NewBatchEmbedder(workers),
	}
	f.ks.tenantRepo = repository.NewTenantRepository(f.db)
	f.ks.graphEngine = parentChildGraphRepo{}
	f.ks.imageResolver = docparser.NewImageResolver()
	f.ks.retrieveEngine = parentChildRetrieveRegistry{engine: retriever.NewKVHybridRetrieveEngine(
		pgretrieve.NewPostgresRetrieveEngineRepository(f.db), types.PostgresRetrieverEngineType)}
	tenant := &types.Tenant{
		ID: proof.tenant, Status: "active",
		RetrieverEngines: types.RetrieverEngines{Engines: []types.RetrieverEngineParams{
			{RetrieverType: types.VectorRetrieverType, RetrieverEngineType: types.PostgresRetrieverEngineType},
			{RetrieverType: types.KeywordsRetrieverType, RetrieverEngineType: types.PostgresRetrieverEngineType},
		}},
	}
	require.NoError(t, f.db.Model(&types.Tenant{}).Where("id=?", proof.tenant).
		Update("retriever_engines", tenant.RetrieverEngines).Error)
	return provider, tenant, calls
}

func exerciseOriginalPublicDispatch(t *testing.T, f *personalRecoveryFixture,
	payload types.DocumentProcessPayload, factory func(asynq.HandlerFunc) interfaces.TaskEnqueuer,
) {
	t.Helper()
	t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY", "1")
	t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY_KNOWLEDGE_IDS", payload.KnowledgeID)
	provider, _, _ := configureOriginalExactConsumer(
		t,
		f,
		&personalExactDocument{tenant: payload.TenantID, kb: payload.KnowledgeBaseID},
	)
	inner, err := originalAcceptanceChat(t, provider.URL)
	require.NoError(t, err)
	gate := repository.NewModelRepository(f.db).(legacyModelInputGate)
	f.ks.modelService = &exactAcceptanceModels{
		v2PipelineModel: f.ks.modelService.(*v2PipelineModel),
		recovery:        f.p,
		summary:         &legacyGuardedChat{inner: inner, gate: gate, tenant: payload.TenantID},
	}
	require.NoError(
		t,
		f.db.Table("knowledge_bases").
			Where("id=?", payload.KnowledgeBaseID).
			Update("summary_model_id", "isolated-summary").
			Error,
	)
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	task := asynq.NewTask(types.TypeDocumentProcess, body)
	if addr := os.Getenv("PERSONAL_EXACT_QUEUE_REDIS_ADDR"); addr != "" {
		require.True(t, strings.HasPrefix(addr, "knowhub-personal-exact-queue-"))
		redisOption := asynq.RedisClientOpt{Addr: addr}
		client := asynq.NewClient(redisOption)
		defer client.Close()
		server := asynq.NewServer(redisOption, asynq.Config{
			Concurrency: 1, Queues: map[string]int{types.QueueDefault: 1},
			TaskCheckInterval: 100 * time.Millisecond, ShutdownTimeout: time.Second,
		})
		completed := make(chan error, 1)
		mux := asynq.NewServeMux()
		mux.HandleFunc(types.TypeDocumentProcess, func(ctx context.Context, task *asynq.Task) error {
			err := f.ks.ProcessDocument(ctx, task)
			completed <- err
			return err
		})
		require.NoError(t, server.Start(mux))
		defer server.Shutdown()
		f.ks.task = client
		var op types.TaskPendingOp
		require.NoError(t, f.db.Where("scope=? AND scope_id=?",
			types.KnowledgeDispatchScope, payload.KnowledgeID).Take(&op).Error)
		require.NoError(t, f.ks.publishKnowledgeDispatch(context.Background(), &op))
		select {
		case err := <-completed:
			require.NoError(t, err)
		case <-time.After(3 * time.Minute):
			t.Fatal("isolated Redis public consumer did not complete")
		}
	} else if factory != nil {
		completed := make(chan error, 1)
		f.ks.task = factory(func(ctx context.Context, task *asynq.Task) error {
			err := f.ks.ProcessDocument(ctx, task)
			completed <- err
			return err
		})
		var op types.TaskPendingOp
		require.NoError(t, f.db.Where("scope=? AND scope_id=?",
			types.KnowledgeDispatchScope, payload.KnowledgeID).Take(&op).Error)
		require.NoError(t, f.ks.publishKnowledgeDispatch(context.Background(), &op))
		select {
		case err := <-completed:
			require.NoError(t, err)
		case <-time.After(3 * time.Minute):
			t.Fatal("Lite public consumer did not complete")
		}
	} else {
		require.NoError(t, f.ks.ProcessDocument(context.Background(), task))
	}
	imported, resource, attempt, err := f.p.repo.Load(context.Background(), payload.PersonalAttemptID)
	require.NoError(t, err)
	require.EqualValues(t, 4, resource.Generation)
	require.Equal(t, "indexed", attempt.Status)
	require.Equal(t, payload.CandidateVersionID, resource.ActiveVersionID)
	require.Empty(t, attempt.OwnerNonce)
	imported.AttemptID = attempt.ID
	statusRows := []types.FeishuPersonalImport{*imported}
	require.NoError(t, f.p.firstCreateStatuses(context.Background(), statusRows))
	require.Equal(t, "verified", statusRows[0].IndexStatus)
	require.Equal(t, "synced", statusRows[0].Status)
	proof, err := f.p.prepareExactPersonalSource(context.Background(), attempt, true)
	require.NoError(t, err)
	require.NoError(t, f.p.reconcileFirstCreatePublic(context.Background(), imported, attempt))
	require.NoError(t, f.p.repo.ReconcileIntent(context.Background(), attempt.ID))
	// A duplicate public delivery is acknowledged without claiming or publishing again.
	require.NoError(t, f.ks.ProcessDocument(context.Background(), task))
	_, after, _, err := f.p.repo.Load(context.Background(), attempt.ID)
	require.NoError(t, err)
	require.Equal(t, resource.Generation, after.Generation)
	var chunks, vectors int64
	require.NoError(
		t,
		f.db.Table("chunks").Where("knowledge_id=? AND is_enabled", payload.KnowledgeID).Count(&chunks).Error,
	)
	require.NoError(
		t,
		f.db.Table("embeddings").Where("knowledge_id=? AND is_enabled", payload.KnowledgeID).Count(&vectors).Error,
	)
	require.Positive(t, chunks)
	require.Positive(t, vectors)
	exerciseOriginalExactRetrieval(t, f, &exactPersonalConsumer{recovery: f.p, source: proof})
	evidence, err := json.Marshal(map[string]any{
		"knowledge_id": payload.KnowledgeID, "generation": resource.Generation,
		"chunk_rows": chunks, "vector_rows": vectors, "public_dispatch_verified": true,
		"atomic_publication_verified": true, "duplicate_delivery_unchanged": true,
		"reconcile_without_new_claim": true, "authorized_pg_retrieval_verified": true,
		"normal_hybrid_search_verified": true, "ordinary_viewer_verified": true,
		"actual_rerank_passages_verified": true,
		"normal_chat_completion_verified": true, "unproved_history_and_cache_rejected": true,
		"authorization_provider": "synthetic isolated OAuth", "model_provider": originalAcceptanceModelMode(),
		"production_writes":     false,
		"actual_redis_dispatch": os.Getenv("PERSONAL_EXACT_QUEUE_REDIS_ADDR") != "",
		"actual_lite_dispatch":  factory != nil,
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(os.Getenv("PERSONAL_EXACT_EVIDENCE_OUT")+".public-dispatch.json", evidence, 0o600))
}

func exerciseOriginalExactPublication(
	t *testing.T, f *personalRecoveryFixture, ctx context.Context, consumer *exactPersonalConsumer, provider string,
) {
	t.Helper()
	inner, err := originalAcceptanceChat(t, provider)
	require.NoError(t, err)
	gate := repository.NewModelRepository(f.db).(legacyModelInputGate)
	require.ErrorIs(
		t,
		gate.CheckLegacyModelInputs(context.Background(), consumer.source.tenant),
		types.ErrLegacySourceRestricted,
	)
	f.ks.modelService = &exactAcceptanceModels{
		v2PipelineModel: f.ks.modelService.(*v2PipelineModel),
		recovery:        f.p,
		summary:         &legacyGuardedChat{inner: inner, gate: gate, tenant: consumer.source.tenant},
	}
	require.NoError(t, f.db.Table("knowledge_bases").Where("id=?", consumer.source.kb).
		Update("summary_model_id", "isolated-summary").Error)
	_, err = f.ks.RegenerateKnowledgeSummary(ctx, consumer.source.knowledge)
	require.NoError(t, err)
	require.True(t, consumer.derivedReady)
	// These mutations are confined to the disposable fixture. A file proof
	// must not adopt an unknown/tampered output, even under the same document.
	var id string
	for key := range consumer.outputs {
		id = key
		break
	}
	var original string
	require.NoError(t, f.db.Table("chunks").Select("content").Where("id=?", id).Scan(&original).Error)
	require.NoError(t, f.db.Table("chunks").Where("id=?", id).Update("content", "isolated conflicting output").Error)
	require.Error(t, consumer.publish(ctx))
	var prematurelyVisible int64
	require.NoError(t, f.db.Table("chunks").Where("knowledge_id=? AND is_enabled", consumer.source.knowledge).
		Count(&prematurelyVisible).Error)
	require.Zero(t, prematurelyVisible)
	require.NoError(t, f.db.Table("chunks").Where("id=?", id).Update("content", original).Error)
	require.NoError(t, f.db.Table("feishu_personal_grant_epochs").Where("epoch=?", consumer.source.grantEpoch).
		Update("epoch", "isolated-revoked").Error)
	require.Error(t, consumer.publish(ctx))
	require.NoError(t, f.db.Table("feishu_personal_grant_epochs").Where("epoch='isolated-revoked'").
		Update("epoch", consumer.source.grantEpoch).Error)
	require.NoError(t, consumer.publish(ctx))
	imported, resource, attempt, err := f.p.repo.Load(context.Background(), consumer.source.attempt)
	require.NoError(t, err)
	require.Equal(t, consumer.source.candidate, resource.ActiveVersionID)
	require.Equal(t, "indexed", attempt.Status)
	require.Empty(t, attempt.OwnerNonce)
	require.Nil(t, attempt.LeaseUntil)
	require.Error(t, consumer.publish(ctx), "a consumed claim must never publish twice")
	publishedSource, err := f.p.prepareExactPersonalSource(context.Background(), attempt, true)
	require.NoError(t, err)
	require.Equal(t, consumer.source.fileSHA256, publishedSource.fileSHA256)
	require.Empty(t, publishedSource.nonce)
	_, err = f.p.exactConsumerContext(context.Background(), publishedSource)
	require.Error(t, err, "published source proof cannot reacquire execution authority")
	require.NoError(t, f.p.reconcileFirstCreatePublic(context.Background(), imported, attempt))
	require.NoError(t, f.p.repo.ReconcileIntent(context.Background(), attempt.ID))
	_, reconciledResource, reconciledAttempt, err := f.p.repo.Load(context.Background(), attempt.ID)
	require.NoError(t, err)
	require.Equal(t, resource.Generation, reconciledResource.Generation)
	require.Equal(t, "indexed", reconciledAttempt.Status)
	var chunks, vectors int64
	require.NoError(
		t,
		f.db.Table("chunks").Where("knowledge_id=? AND is_enabled", consumer.source.knowledge).Count(&chunks).Error,
	)
	require.NoError(
		t,
		f.db.Table("embeddings").
			Where("knowledge_id=? AND is_enabled", consumer.source.knowledge).
			Count(&vectors).
			Error,
	)
	require.Positive(t, chunks)
	require.Positive(t, vectors)
	exerciseOriginalExactRetrieval(t, f, consumer)
	evidence, err := json.Marshal(map[string]any{
		"knowledge_id": consumer.source.knowledge, "generation": consumer.source.generation,
		"chunk_rows": chunks, "vector_rows": vectors, "derived_write_verified": true,
		"atomic_publication_verified": true, "duplicate_publication_rejected": true,
		"tampered_output_publication_rejected": true, "revoked_grant_publication_rejected": true,
		"model_provider":                originalAcceptanceModelMode(),
		"authorized_retrieval_verified": true, "production_writes": false,
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(os.Getenv("PERSONAL_EXACT_EVIDENCE_OUT")+".publication.json", evidence, 0o600))
	t.Logf(
		"original consumer publication verified: chunks=%d vectors=%d; exact authorized PG retrieval verified",
		chunks,
		vectors,
	)
}

func exerciseOriginalExactRetrieval(t *testing.T, f *personalRecoveryFixture, consumer *exactPersonalConsumer) {
	t.Helper()
	const heldDocument = "isolated-held-document"
	require.NoError(t, f.db.Exec(`INSERT INTO knowledges
(id,tenant_id,knowledge_base_id,channel,custom_metadata,metadata)
VALUES (?, ?, ?, 'feishu_drive', '{}'::json, '{"datasource_id":"held-sibling"}'::json)`,
		heldDocument, consumer.source.tenant, consumer.source.kb).Error)
	require.NoError(t, f.db.Exec(`INSERT INTO chunks
(id,tenant_id,knowledge_id,knowledge_base_id,content,is_enabled)
VALUES ('isolated-held-chunk', ?, ?, ?, 'original restricted history', true)`,
		consumer.source.tenant, heldDocument, consumer.source.kb).Error)
	require.NoError(t, f.db.Exec(`INSERT INTO embeddings
(source_id,source_type,chunk_id,knowledge_id,knowledge_base_id,content,dimension,embedding,is_enabled)
SELECT 'isolated-held-chunk',source_type,'isolated-held-chunk',?,knowledge_base_id,
'original restricted history',dimension,embedding,true FROM embeddings WHERE knowledge_id=? LIMIT 1`,
		heldDocument, consumer.source.knowledge).Error)
	require.NoError(t, f.db.Exec(`INSERT INTO feishu_legacy_resources
(id,tenant_id,knowledge_base_id,knowledge_id,data_source_id,state,original_file_path,external_id)
VALUES ('isolated-held-resource', ?, ?, ?, 'held-sibling', 'hold', 'local://held-only/old.docx',
 'unrelated-old-source')`,
		consumer.source.tenant, consumer.source.kb, heldDocument).Error)

	// Existing historical bodies are enrolled before these refusal checks.
	// The exact recovery must never obtain an old-source database write nonce.
	for _, mutation := range []string{
		"UPDATE knowledges SET description='forbidden' WHERE id=?",
		"UPDATE chunks SET content='forbidden' WHERE knowledge_id=?",
		"UPDATE embeddings SET content='forbidden' WHERE knowledge_id=?",
	} {
		err := f.db.Exec(mutation, heldDocument).Error
		require.Error(t, err)
		require.Contains(t, err.Error(), "40001")
	}
	web := types.WithPrincipal(personalUser("A"), types.Principal{Type: types.PrincipalWebUser, ID: "A"})
	web = types.WithCaller(
		web,
		types.Caller{TenantID: consumer.source.tenant, UserID: "A", Role: types.TenantRoleAdmin},
	)
	web = types.WithExecutionTenant(web, consumer.source.tenant)
	read, err := f.p.exactPublishedReadContext(web, consumer.source.knowledge)
	require.NoError(t, err)
	pg := pgretrieve.NewPostgresRetrieveEngineRepository(f.db)
	query := make([]float32, 1024)
	query[0] = 1
	if loadOriginalAcceptanceModels(t) != nil {
		model, err := f.ks.modelService.GetEmbeddingModel(read, "exact-embedding")
		require.NoError(t, err)
		query, err = model.Embed(types.WithEmbedQuery(read), "original")
		require.NoError(t, err)
		require.Len(t, query, 1024)
	}

	params := types.RetrieveParams{
		KnowledgeBaseIDs: []string{consumer.source.kb},
		Query:            "original", Embedding: query, TopK: 10, Threshold: 0,
		RetrieverType: types.VectorRetrieverType,
	}
	for _, kind := range []types.RetrieverType{types.VectorRetrieverType, types.KeywordsRetrieverType} {
		params.RetrieverType = kind
		results, err := pg.Retrieve(read, params)
		require.NoError(t, err)
		hits := 0
		for _, result := range results {
			require.NoError(t, result.Error)
			for _, hit := range result.Results {
				require.Equal(t, consumer.source.knowledge, hit.KnowledgeID)
				require.Equal(t, consumer.source.kb, hit.KnowledgeBaseID)
				hits++
			}
		}
		require.Positive(t, hits)
		t.Logf("original exact authorized retrieval verified: type=%s hits=%d", kind, hits)
	}

	// Exercise the normal service, including configuration projection, store
	// fan-out, SQL candidates, Knowledge/Chunk loading and result enrichment.
	t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY", "1")
	search := &knowledgeBaseService{
		personalRecovery: f.p, repo: repository.NewKnowledgeBaseRepository(f.db),
		kgRepo: f.ks.repo, chunkRepo: f.ks.chunkRepo, modelService: f.ks.modelService,
		kbShareService: f.p.shares, retrieveEngine: f.ks.retrieveEngine,
	}
	normal, err := restoreSummaryRefreshTenantInfo(web, f.ks.tenantRepo, consumer.source.tenant)
	require.NoError(t, err)
	results, err := search.HybridSearch(normal, consumer.source.kb, types.SearchParams{
		KnowledgeIDs: []string{consumer.source.knowledge}, QueryText: "original", QueryEmbedding: query,
		MatchCount: 10,
	})
	require.NoError(t, err)
	require.NotEmpty(t, results)
	for _, result := range results {
		require.Equal(t, consumer.source.knowledge, result.KnowledgeID)
	}

	exerciseOriginalExactChat(t, f, normal, search)

	// KB-wide search must discover exact receipts without a client-supplied ID.
	// An unrelated incomplete staging receipt remains closed and does not stop
	// the already-published file from being searched.
	incomplete, err := json.Marshal(map[string]any{
		"knowledge_base_id": consumer.source.kb, "knowledge_id": "isolated-incomplete-document",
	})
	require.NoError(t, err)
	require.NoError(t, f.db.Create(&types.TaskPendingOp{
		TenantID: consumer.source.tenant, Scope: types.PersonalExactReceiptScope,
		ScopeID: "isolated-incomplete-document", TaskType: "knowledge:exact_receipt", Op: "staging",
		DedupKey: "isolated-incomplete-receipt", Payload: json.RawMessage(incomplete),
	}).Error)
	kbResults, err := search.HybridSearch(normal, consumer.source.kb, types.SearchParams{
		QueryText: "original", QueryEmbedding: query, MatchCount: 10,
	})
	require.NoError(t, err)
	require.NotEmpty(t, kbResults)
	for _, hit := range kbResults {
		require.Equal(t, consumer.source.knowledge, hit.KnowledgeID)
	}
	spy := &legacySpyModel{}
	guardedRerank := &legacyGuardedRerank{
		Reranker: spy,
		gate:     repository.NewModelRepository(f.db).(legacyModelInputGate),
		tenant:   consumer.source.tenant,
	}
	actualModel, err := search.exactRerankModel(read, guardedRerank, "original", results)
	require.NoError(t, err)
	_ = reranking.Rerank(read, actualModel, "original", results, reranking.Options{TopK: 10})
	require.EqualValues(t, 1, spy.calls.Load())
	changed := *results[0]
	changed.Content += " unproven cached passage"
	_, err = search.exactRerankModel(read, guardedRerank, "original", []*types.SearchResult{&changed})
	require.Error(t, err)
	changed = *results[0]
	changed.ImageInfo = `[{"caption":"unproven historical media"}]`
	_, err = search.exactRerankModel(read, guardedRerank, "original", []*types.SearchResult{&changed})
	require.Error(t, err)
	require.EqualValues(t, 1, spy.calls.Load())
	// A real ordinary viewer membership is sufficient; creator/admin identity
	// is not used as a substitute for the current document-read grant.
	require.NoError(t, f.db.Table("tenant_members").Where("user_id='B' AND tenant_id=?", consumer.source.tenant).
		Update("role", types.TenantRoleViewer).Error)
	viewer := types.WithPrincipal(personalUser("B"), types.Principal{Type: types.PrincipalWebUser, ID: "B"})
	viewer = types.WithCaller(
		viewer,
		types.Caller{TenantID: consumer.source.tenant, UserID: "B", Role: types.TenantRoleViewer},
	)
	viewer = types.WithExecutionTenant(viewer, consumer.source.tenant)
	viewer, err = restoreSummaryRefreshTenantInfo(viewer, f.ks.tenantRepo, consumer.source.tenant)
	require.NoError(t, err)
	viewerResults, err := search.HybridSearch(viewer, consumer.source.kb, types.SearchParams{
		KnowledgeIDs: []string{consumer.source.knowledge}, QueryText: "original", QueryEmbedding: query, MatchCount: 10,
	})
	require.NoError(t, err)
	require.NotEmpty(t, viewerResults)
	for _, hit := range viewerResults {
		require.Equal(t, consumer.source.knowledge, hit.KnowledgeID)
	}
	stranger := types.WithPrincipal(personalUser("C"), types.Principal{Type: types.PrincipalWebUser, ID: "C"})
	stranger = types.WithCaller(
		stranger,
		types.Caller{TenantID: consumer.source.tenant, UserID: "C", Role: types.TenantRoleAdmin},
	)
	stranger = types.WithExecutionTenant(stranger, consumer.source.tenant)
	_, err = f.p.exactPublishedReadContext(stranger, consumer.source.knowledge)
	require.Error(t, err, "claimed administrator role is not live membership")
	_, err = search.HybridSearch(stranger, consumer.source.kb, types.SearchParams{
		KnowledgeIDs: []string{consumer.source.knowledge}, QueryText: "original", QueryEmbedding: query, MatchCount: 10,
	})
	require.Error(t, err)

	require.ErrorIs(
		t,
		types.CheckLegacySourceRead(f.db.WithContext(read), "", consumer.source.kb),
		types.ErrLegacySourceRestricted,
	)
	require.ErrorIs(t, types.CheckLegacySourceRead(f.db.WithContext(read), heldDocument, consumer.source.kb),
		types.ErrLegacySourceRestricted)
	if _, err = types.CheckExactPublishedDocumentRead(read); err != nil {
		t.Fatal("valid published scope unexpectedly rejected")
	}
	require.NoError(t, f.db.Table("feishu_personal_grant_epochs").Where("epoch=?", consumer.source.grantEpoch).
		Update("epoch", "isolated-serving-revoked").Error)
	_, err = pg.Retrieve(read, params)
	require.Error(t, err, "a previously issued serving scope must honor live grant revocation")
	require.NoError(t, f.db.Table("feishu_personal_grant_epochs").Where("epoch='isolated-serving-revoked'").
		Update("epoch", consumer.source.grantEpoch).Error)
}

type originalAcceptanceModels struct {
	Embedding types.Model `json:"embedding"`
	Chat      types.Model `json:"chat"`
}

func originalAcceptanceModelMode() string {
	if os.Getenv("PERSONAL_EXACT_MODEL_FIXTURE") != "" {
		return "configured test-environment models; real provider HTTP"
	}
	return "synthetic HTTP"
}

func loadOriginalAcceptanceModels(t *testing.T) *originalAcceptanceModels {
	t.Helper()
	path := os.Getenv("PERSONAL_EXACT_MODEL_FIXTURE")
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var models originalAcceptanceModels
	require.NoError(t, json.Unmarshal(data, &models))

	key := []byte(os.Getenv("PERSONAL_EXACT_MODEL_AES_KEY"))
	require.Len(t, key, 32, "test model configuration requires its test-only decryption key")
	for _, model := range []*types.Model{&models.Embedding, &models.Chat} {
		model.Parameters.APIKey, err = utils.DecryptAESGCM(model.Parameters.APIKey, key)
		require.NoError(t, err)
		model.Parameters.AppSecret, err = utils.DecryptAESGCM(model.Parameters.AppSecret, key)
		require.NoError(t, err)
		require.False(t, strings.HasPrefix(model.Parameters.APIKey, utils.EncPrefix))
	}
	require.NotEmpty(t, models.Embedding.Name)
	require.NotEmpty(t, models.Chat.Name)
	return &models
}

func originalAcceptanceChat(t *testing.T, provider string) (chat.Chat, error) {
	t.Helper()
	if fixture := loadOriginalAcceptanceModels(t); fixture != nil {
		return chat.NewRemoteChat(chat.ConfigFromModel(&fixture.Chat, "", ""))
	}
	return chat.NewRemoteChat(&chat.ChatConfig{
		ModelID: "isolated-summary", ModelName: "gpt-4o-mini", Provider: "openai",
		BaseURL: provider + "/v1", APIKey: "synthetic-isolated",
	})
}

func (m *exactAcceptanceModels) AuthorizeExactPersonalChat(
	ctx context.Context, request *types.ChatManage, messages []chat.Message, options *chat.ChatOptions,
) (context.Context, *chat.ChatOptions, error) {
	return (&modelService{personalRecovery: m.recovery}).AuthorizeExactPersonalChat(ctx, request, messages, options)
}

func (m *exactAcceptanceModels) AuthorizeExactPersonalRead(
	ctx context.Context, results []*types.SearchResult,
) (context.Context, error) {
	return (&modelService{personalRecovery: m.recovery}).AuthorizeExactPersonalRead(ctx, results)
}

func (m *exactAcceptanceModels) AuthorizeExactPersonalRerank(
	ctx context.Context, model rerank.Reranker, query string, results []*types.SearchResult,
) (context.Context, rerank.Reranker, error) {
	return (&modelService{personalRecovery: m.recovery}).AuthorizeExactPersonalRerank(ctx, model, query, results)
}
