//go:build anydoc && personal_exact_live

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	chatpipeline "github.com/Tencent/WeKnora/internal/application/service/chat_pipeline"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/reranking"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

// Real native parsing, PostgreSQL indexing and atomic publication of three
// independent sources. Feishu and model HTTP are synthetic in this regression;
// this must not be reported as the separate live-OAuth browser acceptance.
func exerciseAdditionalPublishedSnapshots(t *testing.T, f *personalRecoveryFixture, first string, files map[string][]byte) {
	t.Helper()
	ids := []string{first}
	for _, token := range []string{"real2", "real3"} {
		m := f.prepare("https://tenant.feishu.cn/file/" + token)
		require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
		ids = append(ids, m.LogicalKnowledgeID)
		t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY_KNOWLEDGE_IDS", strings.Join(ids, ","))
		metadata, err := f.p.loadDispatchMetadata(f.ctx, 1, "kb", m.LogicalKnowledgeID)
		require.NoError(t, err)
		sum := sha256.Sum256(files[token])
		file := types.StoredResource{
			ID: uuid.NewString(), Handle: strings.ReplaceAll(uuid.NewString(), "-", "")[:22], TenantID: 1,
			Provider: "local", PhysicalPath: metadata.FilePath, LocationHash: uuid.NewString(), Size: int64(len(files[token])),
			ContentHash: hex.EncodeToString(sum[:]), State: "active", Lifecycle: "persistent",
		}
		require.NoError(t, f.db.Create(&file).Error)
		require.NoError(t, f.db.Create(&types.ResourceBinding{
			ID: uuid.NewString(), ResourceID: file.ID, TenantID: 1,
			OwnerType: "knowledge", OwnerID: metadata.ID, Relation: "source_file",
		}).Error)
		dispatch := f.ks.repo.(interfaces.KnowledgeDispatchRepository)
		op, err := dispatch.GetKnowledgeDispatch(f.ctx, 1, metadata.ID)
		require.NoError(t, err)
		var intent types.KnowledgeDispatch
		require.NoError(t, json.Unmarshal(op.Payload, &intent))
		intent.Task.FilePath = types.BuildResourcePath(file.Handle)
		require.NoError(t, f.db.Exec("UPDATE knowledges SET file_path=? WHERE id=?", intent.Task.FilePath, metadata.ID).Error)
		payload, err := json.Marshal(intent)
		require.NoError(t, err)
		require.NoError(t, f.db.Exec("UPDATE task_pending_ops SET payload=? WHERE id=?", string(payload), op.ID).Error)
		body, err := json.Marshal(intent.Task)
		require.NoError(t, err)
		require.NoError(t, f.ks.ProcessDocument(context.Background(), asynq.NewTask(op.TaskType, body)))
		var receipt types.TaskPendingOp
		require.NoError(t, f.db.Where("scope=? AND scope_id=?", types.PersonalExactReceiptScope, metadata.ID).Take(&receipt).Error)
		require.Equal(t, "published", receipt.Op)
	}
	t.Run("three_published_assets_survive_disconnect", func(t *testing.T) {
		tx := f.db.Begin()
		defer tx.Rollback()
		personal, recovery, knowledge := *f.s, *f.p, *f.ks
		personal.db, personal.knowledge = tx, &knowledge
		recovery.service, recovery.repo = &personal, repository.NewPersonalRecoveryRepository(tx)
		knowledge.personalRecovery = &recovery
		knowledge.repo, knowledge.chunkRepo = repository.NewKnowledgeRepository(tx), repository.NewChunkRepository(tx)
		models := *knowledge.modelService.(*exactAcceptanceModels)
		models.recovery = &recovery
		knowledge.modelService = &models
		var calls atomic.Int32
		denied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(403) }))
		defer denied.Close()
		personal.newClient = func(cfg *core.Config) *core.Client { cfg.BaseURL = denied.URL; return core.NewClient(cfg) }
		require.NoError(t, personal.Disconnect(f.ctx))
		require.NoError(t, tx.Exec("UPDATE tenant_members SET role='viewer' WHERE user_id='B'").Error)
		var connections int64
		require.NoError(t, tx.Model(&types.FeishuConnection{}).Where("user_id='A'").Count(&connections).Error)
		require.Zero(t, connections)
		viewer := types.WithPrincipal(f.userContext("B"), types.Principal{Type: types.PrincipalWebUser, ID: "B"})
		viewer = types.WithCaller(viewer, types.Caller{UserID: "B", TenantID: 1, Role: types.TenantRoleViewer})
		viewer = types.WithExecutionTenant(viewer, 1)
		viewer, err := restoreSummaryRefreshTenantInfo(viewer, knowledge.tenantRepo, 1)
		require.NoError(t, err)
		chunks := &chunkService{personalRecovery: &recovery, chunkRepository: knowledge.chunkRepo}
		for _, id := range ids {
			read, err := recovery.exactPublishedReadContext(viewer, id)
			require.NoError(t, err)
			scoped, writeErr := types.CheckExactDocumentCommit(tx.WithContext(read), 1, id, "kb")
			require.True(t, scoped)
			require.Error(t, writeErr, "published read proof cannot authorize a write")
			row, err := knowledge.GetKnowledgeByID(viewer, id)
			require.NoError(t, err)
			require.Equal(t, id, row.ID)
			file, _, err := knowledge.GetKnowledgeFile(viewer, id)
			require.NoError(t, err)
			data, err := io.ReadAll(file)
			require.NoError(t, err)
			require.NoError(t, file.Close())
			require.NotEmpty(t, data)
			rows, err := chunks.ListChunksByKnowledgeID(viewer, id)
			require.NoError(t, err)
			require.NotEmpty(t, rows)
			var receipt types.TaskPendingOp
			require.NoError(t, tx.Where("scope=? AND scope_id=?", types.PersonalExactReceiptScope, id).Take(&receipt).Error)
			var published types.PersonalExactReceipt
			require.NoError(t, json.Unmarshal(receipt.Payload, &published))
			require.NoError(t, recovery.verifyExactPublishedDatabase(viewer, published))
		}
		search := &knowledgeBaseService{
			personalRecovery: &recovery, repo: repository.NewKnowledgeBaseRepository(tx),
			kgRepo: knowledge.repo, chunkRepo: knowledge.chunkRepo, modelService: knowledge.modelService,
			kbShareService: recovery.shares, retrieveEngine: knowledge.retrieveEngine,
		}
		vector := make([]float32, 1024)
		vector[0] = 1
		results, err := search.HybridSearch(viewer, "kb", types.SearchParams{KnowledgeIDs: ids, QueryText: "recovery", QueryEmbedding: vector, MatchCount: 100})
		require.NoError(t, err)
		found := make(map[string]bool)
		for _, result := range results {
			found[result.KnowledgeID] = true
		}
		require.Len(t, found, 3)
		bundle, err := recovery.preparePublishedReadBundle(viewer, results)
		require.NoError(t, err)
		require.NotNil(t, bundle)
		canonical, err := bundle.canonicalResults(viewer, results)
		require.NoError(t, err)
		require.Len(t, canonical, len(results))
		adapter := &publishedBundleChunks{ChunkRepository: knowledge.chunkRepo, bundle: bundle}
		var chunkIDs []string
		for _, result := range results {
			chunkIDs = append(chunkIDs, result.ID)
		}
		_, err = adapter.ListChunksByIDOnly(viewer, chunkIDs)
		require.NoError(t, err)
		_, err = adapter.ListChunksByIDOnly(viewer, []string{"unknown-outside-request-document"})
		require.Error(t, err)
		// Exercise the ordinary merge/context/completion plugins with all three
		// documents. Only the provider response is synthetic, not read authority.
		configuration := *knowledge.config
		conversation := *configuration.Conversation
		configuration.Conversation = &conversation
		conversation.Summary = &config.SummaryConfig{Prompt: "Answer using verified context only. {{contexts}}", ContextTemplate: "{{contexts}}\nQuestion: {{query}}"}
		knowledge.config = &configuration
		thinking := false
		request := &types.ChatManage{PipelineRequest: types.PipelineRequest{
			SessionID: "published-three-reader-b", UserID: "B", TenantID: 1, Query: "recovery", Language: "English",
			ChatModelID: "isolated-summary", RerankTopK: 100, EmbeddingTopK: 100,
			SummaryConfig: types.SummaryConfig{Prompt: conversation.Summary.Prompt, ContextTemplate: conversation.Summary.ContextTemplate, MaxTokens: 256, Thinking: &thinking},
		}}
		request.SearchResult = results
		request.RewriteQuery = "recovery"
		manager := chatpipeline.NewEventManager()
		merge := chatpipeline.NewPluginMerge(manager, knowledge.chunkRepo, nil, knowledge.modelService)
		into := chatpipeline.NewPluginIntoChatMessage(manager, nil)
		completion := chatpipeline.NewPluginChatCompletion(manager, knowledge.modelService)
		next := func() *chatpipeline.PluginError { return nil }
		require.Nil(t, merge.OnEvent(viewer, types.CHUNK_MERGE, request, next))
		require.Nil(t, into.OnEvent(viewer, types.INTO_CHAT_MESSAGE, request, next))
		require.Nil(t, completion.OnEvent(viewer, types.CHAT_COMPLETION, request, next))
		require.NotNil(t, request.ChatResponse)
		require.NotEmpty(t, request.ChatResponse.Content)
		spy := &legacySpyModel{}
		guarded := &legacyGuardedRerank{Reranker: spy, gate: repository.NewModelRepository(tx).(legacyModelInputGate), tenant: 1}
		model, err := (&modelService{personalRecovery: &recovery}).publishedBundleReranker(viewer, guarded, "recovery", canonical)
		require.NoError(t, err)
		_ = reranking.Rerank(viewer, model, "recovery", canonical, reranking.Options{TopK: 100})
		require.EqualValues(t, 1, spy.calls.Load())
		require.Zero(t, calls.Load(), "B's three-document read/search must make no Feishu requests")
		require.NoError(t, tx.Exec("UPDATE tenant_members SET status='disabled' WHERE user_id='B'").Error)
		_, err = knowledge.GetKnowledgeByID(viewer, ids[0])
		require.Error(t, err)
		require.Error(t, bundle.validate(viewer))
	})
}
