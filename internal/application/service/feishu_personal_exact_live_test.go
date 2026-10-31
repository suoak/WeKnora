//go:build feishu_recovery_live && anydoc && personal_exact_live

package service

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	pgretrieve "github.com/Tencent/WeKnora/internal/application/repository/retriever/postgres"
	files "github.com/Tencent/WeKnora/internal/application/service/file"
	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/panjf2000/ants/v2"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

// Real OAuth, revision/ACL, export and configured models. Every KB, protocol,
// resource, task and disconnect write is confined to a disposable test-site
// clone. The remote document and the original test connection are not revoked.
func TestPersonalExactLiveOAuthPublicationAndDisconnect(t *testing.T) {
	if os.Getenv("FEISHU_RECOVERY_LIVE_ACCEPTANCE") != "1" {
		t.Skip("explicit isolated live acceptance required")
	}
	u, err := url.Parse(os.Getenv("FEISHU_RECOVERY_LIVE_CLONE_DSN"))
	require.NoError(t, err)
	require.Equal(t, "knowhub-feishu-v2-d6e99d894d3a-postgres-1", u.Hostname())
	require.True(t, strings.HasPrefix(u.Path, "/kh_personal_exact_live_"))
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: gormlog.Discard})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	defer pool.Close()
	db = types.EnablePrivateKBGuards(types.EnableLegacyReadGuards(db))
	t.Setenv("FEISHU_PERSONAL_MVP_FIRST_CREATE", "1")
	t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY", "1")
	t.Setenv("RETRIEVE_DRIVER", "postgres")
	var original types.DataSource
	require.NoError(t, db.Where("id=?", "2b19f3f7-5ad3-4144-a359-7aeeab691289").Take(&original).Error)
	cfg, err := original.ParseConfig()
	require.NoError(t, err)
	ref, err := types.ParseFeishuSourceReference(cfg)
	require.NoError(t, err)
	require.NotNil(t, ref)
	require.Equal(t, types.FeishuUserOAuth, ref.Kind)
	var connection types.FeishuConnection
	require.NoError(t, db.Where("user_id=?", ref.OwnerUserID).Take(&connection).Error)
	require.True(t, connection.ExpiresAt.After(time.Now().Add(15*time.Minute)), "fresh original test token required")
	owner := types.WithPrincipal(context.WithValue(context.Background(), types.UserIDContextKey, ref.OwnerUserID),
		types.Principal{Type: types.PrincipalWebUser, ID: ref.OwnerUserID})
	tenants := repository.NewTenantRepository(db)
	tenant, err := NewTenantService(
		tenants,
		nil,
	).CreateTenant(owner, &types.Tenant{Name: "isolated-personal-exact-" + uuid.NewString()})
	require.NoError(t, err)
	members := NewTenantMemberService(repository.NewTenantMemberRepository(db), nil, nil, nil)
	_, err = members.EnsureOwner(owner, ref.OwnerUserID, tenant.ID)
	require.NoError(t, err)
	ctx := types.WithCaller(
		owner,
		types.Caller{UserID: ref.OwnerUserID, TenantID: tenant.ID, Role: types.TenantRoleOwner},
	)
	ctx = types.WithExecutionTenant(ctx, tenant.ID)
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	var oldKB types.KnowledgeBase
	require.NoError(t, db.WithContext(owner).Where("id=?", original.KnowledgeBaseID).Take(&oldKB).Error)
	var embedModel, summaryModel types.Model
	require.NoError(t, db.Where("id=?", oldKB.EmbeddingModelID).Take(&embedModel).Error)
	require.NoError(
		t,
		db.Where("type IN ? AND source=? AND deleted_at IS NULL",
			[]string{"KnowledgeQA", "VLLM"}, types.ModelSourceRemote).
			Order("created_at").
			First(&summaryModel).
			Error,
	)
	for _, model := range []*types.Model{&embedModel, &summaryModel} {
		model.ID, model.TenantID = uuid.NewString(), tenant.ID
		require.NoError(t, db.Create(model).Error)
	}
	workers, err := ants.NewPool(1)
	require.NoError(t, err)
	defer workers.Release()
	inner, err := chat.NewRemoteChat(chat.ConfigFromModel(&summaryModel, "", ""))
	require.NoError(t, err)
	models := &exactAcceptanceModels{
		v2PipelineModel: &v2PipelineModel{
			legacyLiveModel: legacyLiveModel{db: db},
			pool:            embedding.NewBatchEmbedder(workers),
		},
		summary: &legacyGuardedChat{
			inner:  inner,
			gate:   repository.NewModelRepository(db).(legacyModelInputGate),
			tenant: tenant.ID,
		},
	}
	configuration := &config.Config{Conversation: &config.ConversationConfig{
		GenerateSummaryPrompt: "Summarize the supplied document. Return JSON with a summary field.",
	}}
	kbs := &knowledgeBaseService{repo: repository.NewKnowledgeBaseRepository(db)}
	kb, err := kbs.CreateKnowledgeBase(types.WithPrivateKBCreation(ctx, ref.OwnerUserID), &types.KnowledgeBase{
		Name: "isolated-personal-exact", Type: types.KnowledgeBaseTypeDocument,
		EmbeddingModelID: embedModel.ID, SummaryModelID: summaryModel.ID,
		ChunkingConfig: types.ChunkingConfig{
			ChunkSize: 512, ChunkOverlap: 50,
			ParserEngineRules: []types.ParserEngineRule{{FileTypes: []string{"docx"}, Engine: "anydoc"}},
		},
		IndexingStrategy: types.IndexingStrategy{VectorEnabled: true, KeywordEnabled: true},
	})
	require.NoError(t, err)
	// Use the same canonical editor grant the normal HTTP adapter resolves.
	// Creating a tenant or setting a caller role alone is not KB write authority.
	authorizedKB, err := kbs.GetKnowledgeBaseByID(ctx, kb.ID)
	require.NoError(t, err)
	grant, err := access.ResolveKB(ctx, access.KBRequest{Caller: types.CallerFromContext(ctx)},
		authorizedKB, types.OrgRoleEditor, nil, nil)
	require.NoError(t, err)
	ctx = grant.Context(ctx)
	root := t.TempDir()
	t.Setenv("LOCAL_STORAGE_BASE_DIR", root)
	catalog := NewResourceCatalog(repository.NewResourceRepository(db))
	ks := &knowledgeService{
		config: configuration, repo: repository.NewKnowledgeRepository(db),
		chunkRepo: repository.NewChunkRepository(db),
		kbService: kbs, modelService: models, tenantRepo: tenants, tenantService: &legacyNoCloud{},
		fileSvc: files.NewResourceCatalogFileService(
			files.NewLocalFileService(root, ""),
			catalog,
		), resourceCatalog: catalog,
		task: &dispatchQueue{}, graphEngine: parentChildGraphRepo{}, imageResolver: docparser.NewImageResolver(),
	}
	ks.retrieveEngine = parentChildRetrieveRegistry{engine: retriever.NewKVHybridRetrieveEngine(
		pgretrieve.NewPostgresRetrieveEngineRepository(db), types.PostgresRetrieverEngineType)}
	kbs.kgRepo, kbs.chunkRepo, kbs.modelService, kbs.retrieveEngine = ks.repo, ks.chunkRepo, models, ks.retrieveEngine
	personal := NewFeishuPersonalService(db, repository.NewSystemSettingRepository(db), ks, kbs, nil)
	personal.api.Transport = recoveryNoRefresh{} // Never rotate a copied refresh token.
	require.NoError(t, EnablePersonalRecovery(personal, ks, nil, t.TempDir()))
	models.recovery = personal.recovery
	links, referenced, err := core.ReferenceLinks(cfg)
	require.NoError(t, err)
	require.True(t, referenced)
	require.Len(t, links, 1)
	imported, err := personal.ImportWithIdentity(ctx, kb.ID, links[0], types.FeishuUserOAuth)
	require.NoError(t, err)
	var manifestOp types.TaskPendingOp
	require.NoError(
		t,
		db.Where("dedup_key=?", "personal-"+imported.AttemptID).
			Take(&manifestOp).
			Error,
	)
	var manifestIntent types.KnowledgeDispatch
	require.NoError(t, json.Unmarshal(manifestOp.Payload, &manifestIntent))
	require.NoError(
		t,
		ks.ProcessDocument(
			context.Background(),
			asynq.NewTask(manifestOp.TaskType, mustMarshalPersonalTask(t, manifestIntent.Task)),
		),
	)
	m, resource, attempt, err := personal.recovery.repo.Load(ctx, imported.AttemptID)
	require.NoError(t, err)
	require.NotEmpty(t, m.KnowledgeID)
	// Historical held source fixture, unrelated to the real OAuth resource.
	heldSource, heldKnowledge := uuid.NewString(), uuid.NewString()
	require.NoError(t, db.Create(&types.DataSource{
		ID: heldSource, TenantID: tenant.ID, KnowledgeBaseID: kb.ID,
		Type: "feishu_drive", Status: "error", LegacyAccessState: "hold", Config: types.JSON(`{}`),
	}).Error)
	require.NoError(
		t,
		db.Exec(`INSERT INTO knowledges(id,tenant_id,knowledge_base_id,type,title,source,channel,metadata)
VALUES (?,?,?,'file','isolated historical fixture','feishu','feishu_drive','{}'::json)`,
			heldKnowledge, tenant.ID, kb.ID).Error,
	)
	require.NoError(t, db.Exec(`INSERT INTO feishu_legacy_resources
(id,tenant_id,knowledge_base_id,data_source_id,external_id,knowledge_id,state)
VALUES (?,?,?,?,?,?,'hold')`, uuid.NewString(), tenant.ID, kb.ID, heldSource, "isolated-unrelated-history",
		heldKnowledge).Error)
	require.ErrorIs(
		t,
		types.CheckLegacySourceRead(db.WithContext(ctx), m.KnowledgeID, kb.ID),
		types.ErrLegacySourceRestricted,
	)
	t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY_KNOWLEDGE_IDS", m.KnowledgeID)
	var op types.TaskPendingOp
	require.NoError(t, db.Where("scope=? AND scope_id=?", types.KnowledgeDispatchScope, m.KnowledgeID).Take(&op).Error)
	var intent types.KnowledgeDispatch
	require.NoError(t, json.Unmarshal(op.Payload, &intent))
	before := resource.Generation
	require.NoError(
		t,
		ks.ProcessDocument(context.Background(), asynq.NewTask(op.TaskType, mustMarshalPersonalTask(t, intent.Task))),
	)
	_, resource, attempt, err = personal.recovery.repo.Load(ctx, attempt.ID)
	require.NoError(t, err)
	require.Equal(t, before+1, resource.Generation)
	require.Equal(t, "indexed", attempt.Status)
	require.Equal(t, attempt.CandidateVersionID, resource.ActiveVersionID)
	var chunks, vectors int64
	require.NoError(t, db.WithContext(ctx).Raw("SELECT count(*) FROM chunks WHERE knowledge_id=? AND is_enabled",
		m.KnowledgeID).Scan(&chunks).Error)
	require.NoError(
		t,
		db.WithContext(ctx).Raw("SELECT count(*) FROM embeddings WHERE knowledge_id=? AND is_enabled",
			m.KnowledgeID).Scan(&vectors).Error,
	)
	require.Positive(t, chunks)
	require.Positive(t, vectors)
	results, err := kbs.HybridSearch(
		ctx,
		kb.ID,
		types.SearchParams{KnowledgeIDs: []string{m.KnowledgeID}, QueryText: "docx", MatchCount: 10},
	)
	require.NoError(t, err)
	require.NotEmpty(t, results)
	for _, result := range results {
		require.Equal(t, m.KnowledgeID, result.KnowledgeID)
	}
	read, err := personal.recovery.exactPublishedReadContext(ctx, m.KnowledgeID)
	require.NoError(t, err)
	require.ErrorIs(
		t,
		types.CheckLegacySourceRead(db.WithContext(read), heldKnowledge, kb.ID),
		types.ErrLegacySourceRestricted,
	)
	require.NoError(t, personal.Disconnect(ctx)) // Clone DB only; no remote revocation endpoint.
	_, err = personal.recovery.exactPublishedReadContext(ctx, m.KnowledgeID)
	require.Error(t, err)
	_, err = pgretrieve.NewPostgresRetrieveEngineRepository(db).Retrieve(read, types.RetrieveParams{
		KnowledgeBaseIDs: []string{kb.ID}, Query: "docx", TopK: 10, RetrieverType: types.KeywordsRetrieverType,
	})
	require.Error(t, err, "an issued serving proof must reject the normal disconnect")
	require.NoError(
		t,
		ks.ProcessDocument(context.Background(), asynq.NewTask(op.TaskType, mustMarshalPersonalTask(t, intent.Task))),
	)
	var afterChunks int64
	require.NoError(t, db.WithContext(ctx).Raw("SELECT count(*) FROM chunks WHERE knowledge_id=?",
		m.KnowledgeID).Scan(&afterChunks).Error)
	require.Equal(t, chunks, afterChunks)
	evidence, err := json.Marshal(map[string]any{
		"real_oauth_acl_revision_export": true, "real_models": true, "chunk_rows": chunks, "vector_rows": vectors,
		"publication_verified": true, "authorized_retrieval_verified": true, "disconnect_denied_read": true,
		"old_held_source_denied": true, "duplicate_delivery_unchanged": true, "production_writes": false,
	})
	require.NoError(t, err)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(os.Getenv("PERSONAL_EXACT_LIVE_EVIDENCE_DIR"), "oauth-lifecycle.json"),
			evidence,
			0o600,
		),
	)
}

func mustMarshalPersonalTask(t *testing.T, payload types.DocumentProcessPayload) []byte {
	t.Helper()
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	return data
}
