//go:build feishu_recovery_live && anydoc

package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	pgretrieve "github.com/Tencent/WeKnora/internal/application/repository/retriever/postgres"
	files "github.com/Tencent/WeKnora/internal/application/service/file"
	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/drive"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/wiki"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/panjf2000/ants/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

type recoveryNoRefresh struct{}

func (recoveryNoRefresh) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("clone token refresh prohibited")
}

// Cloud reads and configured embedding requests are real. All source writes,
// memberships, generations and revocation mutations are confined to a uniquely
// named disposable database without APP/worker and a temporary file root.
func TestRecoveryLiveNormalSyncPublicationAndRevocation(t *testing.T) {
	if os.Getenv("FEISHU_RECOVERY_LIVE_ACCEPTANCE") != "1" {
		t.Skip("explicit live acceptance required")
	}
	u, err := url.Parse(os.Getenv("FEISHU_RECOVERY_LIVE_CLONE_DSN"))
	require.NoError(t, err)
	require.Equal(t, "knowhub-feishu-v2-d6e99d894d3a-postgres-1", u.Hostname())
	require.True(t, strings.HasPrefix(u.Path, "/kh_recovery_live_"))
	require.NotEqual(t, "/kh_recovery_live_base", u.Path)
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: gormlog.Discard})
	require.NoError(t, err)
	poolDB, err := db.DB()
	require.NoError(t, err)
	defer poolDB.Close()
	db = types.EnablePrivateKBGuards(types.EnableLegacyReadGuards(db))
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	t.Chdir(root)
	t.Setenv("RETRIEVE_DRIVER", "postgres")
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	t.Setenv("FEISHU_LEGACY_SYNC_SUSPENDED", "false")
	// Source preparation precedes the explicit synthetic incident. All actual
	// sync/model work below runs with the real recovery guard enabled.
	t.Setenv("FEISHU_SYNC_SAFETY_ENABLED", "false")
	var original types.DataSource
	require.NoError(t, db.Where("id=?", "847ec22f-ceb3-4f73-b268-ba54c7c3ff67").Take(&original).Error)
	cfg, err := original.ParseConfig()
	require.NoError(t, err)
	ref, err := types.ParseFeishuSourceReference(cfg)
	require.NoError(t, err)
	require.NotNil(t, ref)
	var oldKB types.KnowledgeBase
	ownerCtx := context.WithValue(context.Background(), types.UserIDContextKey, ref.OwnerUserID)
	ownerCtx = types.WithPrincipal(ownerCtx, types.Principal{Type: types.PrincipalWebUser, ID: ref.OwnerUserID})
	require.NoError(t, db.WithContext(ownerCtx).Where("id=?", original.KnowledgeBaseID).Take(&oldKB).Error)
	var model types.Model
	require.NoError(t, db.Where("id=?", oldKB.EmbeddingModelID).Take(&model).Error)
	tenants := repository.NewTenantRepository(db)
	tenant, err := NewTenantService(tenants, nil).CreateTenant(ownerCtx, &types.Tenant{Name: "isolated-recovery-" + uuid.NewString()})
	require.NoError(t, err)
	members := NewTenantMemberService(repository.NewTenantMemberRepository(db), nil, nil, nil)
	_, err = members.EnsureOwner(ownerCtx, ref.OwnerUserID, tenant.ID)
	require.NoError(t, err)
	ctx := types.WithCaller(ownerCtx, types.Caller{UserID: ref.OwnerUserID, TenantID: tenant.ID, Role: types.TenantRoleOwner})
	ctx = context.WithValue(ctx, types.TenantIDContextKey, tenant.ID)
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	// Same configured test model, new fixture identity; credentials never leave
	// this isolated test server. No production model or grant is copied.
	model.ID = uuid.NewString()
	model.TenantID = tenant.ID
	require.NoError(t, db.Create(&model).Error)
	kbRepo := repository.NewKnowledgeBaseRepository(db)
	kbs := &knowledgeBaseService{repo: kbRepo}
	kb, err := kbs.CreateKnowledgeBase(types.WithPrivateKBCreation(ctx, ref.OwnerUserID), &types.KnowledgeBase{
		ChunkingConfig: types.ChunkingConfig{ChunkSize: 512, ChunkOverlap: 50, ParserEngineRules: []types.ParserEngineRule{{FileTypes: []string{"xlsx", "docx", "pptx"}, Engine: "anydoc"}}},
		Name:           "recovery-cloud-canary", Type: types.KnowledgeBaseTypeDocument, EmbeddingModelID: model.ID,
		IndexingStrategy: types.IndexingStrategy{VectorEnabled: true, KeywordEnabled: true},
	})
	require.NoError(t, err)
	localRoot := t.TempDir()
	t.Setenv("LOCAL_STORAGE_BASE_DIR", localRoot)
	workers, err := ants.NewPool(1)
	require.NoError(t, err)
	defer workers.Release()
	ks := &knowledgeService{config: &config.Config{}, repo: repository.NewKnowledgeRepository(db),
		kbService: &v2PipelineKB{legacyLiveKB{db: db}}, modelService: &v2PipelineModel{legacyLiveModel: legacyLiveModel{db: db}, pool: embedding.NewBatchEmbedder(workers)},
		tenantService: &legacyNoCloud{}, tenantRepo: tenants, chunkRepo: repository.NewChunkRepository(db), task: &dispatchQueue{}, graphEngine: parentChildGraphRepo{}, fileSvc: files.NewLocalFileService(localRoot, "")}
	ks.retrieveEngine = parentChildRetrieveRegistry{engine: retriever.NewKVHybridRetrieveEngine(pgretrieve.NewPostgresRetrieveEngineRepository(db), types.PostgresRetrieverEngineType)}
	personal := NewFeishuPersonalService(db, repository.NewSystemSettingRepository(db), ks, ks.kbService, nil)
	personal.api.Transport = recoveryNoRefresh{}
	require.NoError(t, EnablePersonalRecovery(personal, ks, nil, t.TempDir()))
	registry := datasource.NewConnectorRegistry()
	require.NoError(t, registry.Register(drive.NewDriveConnector(core.RegionFeishuDrive)))
	require.NoError(t, registry.Register(wiki.NewConnector(core.RegionFeishu)))
	logs := repository.NewSyncLogRepository(db)
	queue := &syncExecutionQueue{}
	svc := &DataSourceService{dsRepo: repository.NewDataSourceRepository(db), syncLogRepo: logs, knowledgeService: ks, kbService: ks.kbService, taskEnqueuer: queue, connectorRegistry: registry, tenantRepo: tenants, tagService: &processSyncTagService{}}
	svc.SetFeishuIdentityService(personal)
	blob, err := cfg.ToJSON()
	require.NoError(t, err)
	source, err := svc.CreateDataSource(ctx, &types.DataSource{TenantID: tenant.ID, KnowledgeBaseID: kb.ID, Name: "isolated-real-feishu", Type: original.Type, Status: types.DataSourceStatusActive, Config: blob, SyncMode: types.SyncModeFull})
	require.NoError(t, err)
	t.Setenv("FEISHU_LEGACY_SYNC_ALLOWED_SOURCE_IDS", source.ID)
	address := os.Getenv("FEISHU_RECOVERY_LIVE_REDIS_ADDR")
	require.Regexp(t, regexp.MustCompile(`^knowhub-recovery-live-redis-[a-f0-9]{12}:6379$`), address)
	controlRedis := redis.NewClient(&redis.Options{Addr: address, MaxRetries: -1})
	defer controlRedis.Close()
	policy := feishusafety.DefaultConfig()
	policy.Namespace = "live-recovery-" + uuid.NewString()
	modelURL, err := url.Parse(model.Parameters.BaseURL)
	require.NoError(t, err)
	policy.EndpointHost = modelURL.Hostname()
	policy.InFlight = 1
	policy.RPS = 1
	policy.RPM = 30
	policy.TPM = 120000
	policy.TokenBoundVerified = true
	policy.TokenOverhead = 128
	policy.WaitBudgetDuration = 65 * time.Second
	guard, err := feishusafety.New(controlRedis, policy)
	require.NoError(t, err)
	controller, err := feishusynccontrol.New(controlRedis, guard, feishusynccontrol.Config{Automatic: true, WorkerID: "isolated-live:1", SourceSHA: strings.Repeat("e", 40), Scope: "all", Maintenance: 4}, func(stop context.Context) error { _, e := repository.FenceLegacySyncExecutions(stop, db); return e }, func(feishusynccontrol.Event) error { return nil })
	require.NoError(t, err)
	// Inventory is a controlled single-process fixture, not an attestation of
	// production Asynq health. Real two-process consumer tests run separately.
	controller.Consumers = func(context.Context) ([]string, error) { return []string{"isolated-live:1"}, nil }
	require.NoError(t, controller.Step(ctx))
	feishusafety.InstallDefault(guard)
	defer feishusafety.InstallDefault(nil)
	t.Setenv("FEISHU_SYNC_SAFETY_ENABLED", "true")
	require.NoError(t, guard.Trip(ctx, "feishu_headers_io_failure"))
	require.NoError(t, controlRedis.Set(ctx, "asynq:{sync}:paused", "feishu_safety_stop", 0).Err())
	for i := 0; i < 6; i++ {
		id := uuid.NewString()
		require.NoError(t, controlRedis.RPush(ctx, "asynq:{sync}:pending", id).Err())
		require.NoError(t, controlRedis.HSet(ctx, "asynq:{sync}:t:"+id, "msg", []byte{byte(i), 255}, "state", "pending").Err())
	}
	manager, err := feishusynccontrol.NewRecoveryManager(controller, []byte(strings.Repeat("q", 32)), func(call context.Context, id string) (feishusynccontrol.RecoveryFacts, error) {
		return feishusynccontrol.RecoveryFactsFromDatabase(call, db, id)
	})
	require.NoError(t, err)
	plan, err := manager.Plan(ctx, source.ID, ref.OwnerUserID)
	require.NoError(t, err)
	approval := func(operation string) feishusynccontrol.RecoveryApproval {
		digest, e := feishusynccontrol.RecoveryDigest(plan)
		require.NoError(t, e)
		a := feishusynccontrol.RecoveryApproval{Version: 1, Operation: operation, PlanSHA256: digest, ActorID: ref.OwnerUserID, Reference: "isolated-live-acceptance-only", EvidenceSHA256: strings.Repeat("d", 64), ExpiresMS: plan.ExpiresMS}
		b, e := json.Marshal(a)
		require.NoError(t, e)
		var fields map[string]any
		require.NoError(t, json.Unmarshal(b, &fields))
		b, e = json.Marshal(fields)
		require.NoError(t, e)
		mac := hmac.New(sha256.New, []byte(strings.Repeat("q", 32)))
		_, _ = mac.Write(b)
		a.Signature = hex.EncodeToString(mac.Sum(nil))
		return a
	}
	require.NoError(t, manager.Reset(ctx, plan, approval("reset"), ref.OwnerUserID))
	require.NoError(t, controller.Step(ctx))
	controlContext, stopController := context.WithCancel(ctx)
	defer stopController()
	controllerDone := make(chan struct{})
	go func() { defer close(controllerDone); controller.Run(controlContext) }()
	defer func() { stopController(); <-controllerDone }()
	boundRun := ""
	err = manager.Canary(ctx, plan, approval("canary"), ref.OwnerUserID, func(work context.Context, bind func(string, int64) error) error {
		return svc.RunRecoveryCanary(work, source.ID, func(run string, generation int64) error {
			require.EqualValues(t, 1, generation)
			require.Empty(t, boundRun)
			boundRun = run
			return bind(run, generation)
		})
	})
	if err != nil {
		t.Fatalf("real normal sync/publication failed: category=%s error_type=%T; no authorization or safety bypass applied", feishuRemoteFailureCategory(err), err)
	}
	require.Zero(t, queue.calls.Load())
	require.EqualValues(t, 6, controlRedis.LLen(ctx, "asynq:{sync}:pending").Val())
	require.EqualValues(t, 1, controlRedis.Exists(ctx, "asynq:{sync}:paused").Val())
	stored, err := logs.FindByID(ctx, boundRun)
	require.NoError(t, err)
	require.Equal(t, types.SyncLogStatusSuccess, stored.Status)
	require.False(t, stored.ExecutionClaimed)
	publishedSource, e := svc.dsRepo.FindByID(ctx, source.ID)
	require.NoError(t, e)
	require.NotNil(t, publishedSource.LastSyncAt)
	require.NotEmpty(t, publishedSource.LastSyncCursor)
	_, e = svc.ManualSync(ctx, source.ID)
	require.Error(t, e, "ordinary sync cannot borrow the completed canary lifecycle")
	var resources []types.FeishuLegacyResource
	require.NoError(t, db.Where("data_source_id=? AND state='active'", source.ID).Find(&resources).Error)
	require.NotEmpty(t, resources)
	for _, resource := range resources {
		k, err := ks.repo.GetKnowledgeByID(ctx, tenant.ID, resource.KnowledgeID)
		require.NoError(t, err)
		require.Equal(t, types.ParseStatusCompleted, k.ParseStatus)
		var chunks, vectors int64
		require.NoError(t, db.WithContext(ctx).Table("chunks").Where("knowledge_id=? AND deleted_at IS NULL", k.ID).Count(&chunks).Error)
		require.NoError(t, db.WithContext(ctx).Table("embeddings").Where("knowledge_id=?", k.ID).Count(&vectors).Error)
		require.Positive(t, chunks)
		require.Positive(t, vectors)
		chunkRows, e := ks.chunkRepo.ListChunksByKnowledgeID(ctx, tenant.ID, k.ID)
		require.NoError(t, e)
		require.NotEmpty(t, chunkRows)
		query := regexp.MustCompile(`[\p{L}\p{N}]{2,}`).FindString(chunkRows[0].Content)
		require.NotEmpty(t, query)
		engine := pgretrieve.NewPostgresRetrieveEngineRepository(db)
		rows, e := engine.Retrieve(types.WithLegacyReadScope(ctx), types.RetrieveParams{Query: query, KnowledgeBaseIDs: []string{kb.ID}, KnowledgeIDs: []string{k.ID}, RetrieverType: types.KeywordsRetrieverType, TopK: 5, Threshold: 0})
		require.NoError(t, e)
		found := 0
		for _, row := range rows {
			require.NoError(t, row.Error)
			found += len(row.Results)
		}
		require.Positive(t, found, "authorized real keyword retrieval must return the published content")
		embedder, e := ks.modelService.GetEmbeddingModelForTenant(ctx, model.ID, tenant.ID)
		require.NoError(t, e)
		vector, e := embedder.Embed(ctx, query)
		if e != nil {
			t.Fatal("real authorized retrieval query embedding failed")
		}
		vectorRows, e := engine.Retrieve(types.WithLegacyReadScope(ctx), types.RetrieveParams{Query: query, Embedding: vector, KnowledgeBaseIDs: []string{kb.ID}, KnowledgeIDs: []string{k.ID}, RetrieverType: types.VectorRetrieverType, TopK: 5, Threshold: 0})
		require.NoError(t, e)
		vectorFound := 0
		for _, row := range vectorRows {
			require.NoError(t, row.Error)
			vectorFound += len(row.Results)
		}
		require.Positive(t, vectorFound)
		stranger := context.WithValue(ctx, types.UserIDContextKey, "unrelated-test-user")
		stranger = types.WithCaller(stranger, types.Caller{UserID: "unrelated-test-user", TenantID: tenant.ID})
		stranger = types.WithPrincipal(stranger, types.Principal{Type: types.PrincipalWebUser, ID: "unrelated-test-user"})
		_, err = ks.repo.GetKnowledgeByID(stranger, tenant.ID, k.ID)
		require.Error(t, err)
		denied, e := engine.Retrieve(types.WithLegacyReadScope(stranger), types.RetrieveParams{Query: query, KnowledgeBaseIDs: []string{kb.ID}, KnowledgeIDs: []string{k.ID}, RetrieverType: types.KeywordsRetrieverType, TopK: 5, Threshold: 0})
		if e == nil {
			for _, row := range denied {
				require.Empty(t, row.Results, "SQL candidates must exclude a different private principal")
			}
		}
		var publication types.FeishuLegacyAttempt
		require.NoError(t, db.Where("id=? AND resource_id=?", resource.ActiveAttempt, resource.ID).Take(&publication).Error)
		require.Equal(t, "published", publication.Status)
		require.Equal(t, resource.Generation, publication.Generation)
	}
	// Revoke only the clone's connection via the normal service. The original
	// test-site and provider permissions remain unchanged.
	require.NoError(t, personal.Disconnect(ctx))
	_, err = svc.bindFeishuSource(ctx, source)
	require.Error(t, err)
	afterDisconnect, e := feishusynccontrol.RecoveryFactsFromDatabase(ctx, db, source.ID)
	require.NoError(t, e)
	require.False(t, afterDisconnect.Eligible)
	// Disconnect is not itself a persistent source-state revocation in the
	// existing product. Assert its real binding/approval semantics above rather
	// than invent a read-denial result. Exercise retained source-state fences as
	// explicit isolated DB fault injections, never on the test-site/production DB.
	require.NoError(t, db.Model(&types.DataSource{}).Where("id=?", source.ID).Update("legacy_access_state", "hold").Error)
	for _, resource := range resources {
		strict := types.WithCaller(ctx, types.Caller{UserID: ref.OwnerUserID, TenantID: 0, Role: types.TenantRoleViewer})
		require.Error(t, types.CheckLegacySourceRead(db.WithContext(strict), resource.KnowledgeID, kb.ID))
	}
	require.NoError(t, db.Model(&types.DataSource{}).Where("id=?", source.ID).Update("legacy_access_state", "revoked").Error)
	for _, resource := range resources {
		_, err = ks.repo.GetKnowledgeByID(ctx, tenant.ID, resource.KnowledgeID)
		require.Error(t, err)
	}
	t.Log("real Feishu fetch, native parsing, configured embedding, atomic publication and clone-only normal disconnect verified; real Redis recovery state and queue preservation verified; inventory is an isolated fixture")
}
