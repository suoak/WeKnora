//go:build anydoc

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	files "github.com/Tencent/WeKnora/internal/application/service/file"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Synthetic Feishu and model responses, real identity fences, parser, database,
// vectors and publisher. This is independent of the trusted-receipt handoff test.
func TestFeishuV2NativeReferenceUpdatesPreservePublishedVersion(t *testing.T) {
	runV2NativeReferencePipeline(t, false)
}

func TestFeishuV2NativeFirstCreateHandoffAndIncrementalPublication(t *testing.T) {
	runV2NativeReferencePipeline(t, true)
}

type v2PipelineModel struct{ legacyLiveModel }

func (s *v2PipelineModel) GetEmbeddingModel(ctx context.Context, id string) (embedding.Embedder, error) {
	var model types.Model
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&model).Error; err != nil {
		return nil, err
	}
	return s.GetEmbeddingModelForTenant(ctx, id, model.TenantID)
}

func runV2NativeReferencePipeline(t *testing.T, firstCreate bool) {
	t.Helper()
	dsn := feishulegacy.PrivateDSN(t)
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	t.Chdir(root)
	t.Setenv("SYSTEM_AES_KEY", "01234567890123456789012345678901")
	t.Setenv("FEISHU_ACCESS_V2_ENABLED", "1")
	t.Setenv("FEISHU_PERSONAL_MVP_FIRST_CREATE", "1")
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	t.Setenv("RETRIEVE_DRIVER", "postgres")
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	require.NoError(t, database.RunMigrations(dsn))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	db = types.EnablePrivateKBGuards(db)
	var revision atomic.Int64
	var modelCalls atomic.Int32
	var rejectModel atomic.Bool
	revision.Store(1792800000)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" && r.Header.Get("Authorization") != "Bearer synthetic-user-token" {
			http.Error(w, "wrong identity", http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/open-apis/drive/v1/metas/batch_query":
			_, _ = fmt.Fprintf(w, `{"code":0,"data":{"metas":[{"doc_type":"file",`+
				`"doc_token":"real","title":"controlled.md","latest_modify_time":"%d"}]}}`, revision.Load())
		case "/open-apis/drive/v1/files/real/download":
			_, _ = fmt.Fprintf(w, "# Controlled source\n\nv2canary source revision %d.", revision.Load())
		case "/v1/embeddings":
			modelCalls.Add(1)
			if rejectModel.Load() {
				http.Error(w, "synthetic model refusal", http.StatusBadRequest)
				return
			}
			var in struct {
				Input []string `json:"input"`
			}
			if json.NewDecoder(r.Body).Decode(&in) != nil {
				http.Error(w, "invalid model request", 400)
				return
			}
			data := []any{}
			for i := range in.Input {
				vector := make([]float32, 1024)
				vector[0] = 1
				data = append(data, map[string]any{"object": "embedding", "index": i, "embedding": vector})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data, "model": "synthetic"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(remote.Close)
	tenant := &types.Tenant{ID: 809903, Name: "synthetic-v2-native", Status: "active"}
	require.NoError(t, db.Create(tenant).Error)
	user := &types.User{
		ID: uuid.NewString(), Username: "v2-native", Email: "v2-native@example.invalid",
		PasswordHash: "synthetic-no-login", TenantID: tenant.ID, IsActive: true,
	}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Create(&types.TenantMember{
		UserID: user.ID, TenantID: tenant.ID,
		Role: types.TenantRoleAdmin, Status: types.TenantMemberStatusActive,
	}).Error)
	model := &types.Model{
		ID: uuid.NewString(), TenantID: tenant.ID, Name: "synthetic", Type: types.ModelTypeEmbedding,
		Source: types.ModelSourceRemote,
	}
	model.Parameters.BaseURL = remote.URL + "/v1"
	model.Parameters.Provider = "generic"
	model.Parameters.EmbeddingParameters.Dimension = 1024
	require.NoError(t, db.Create(model).Error)
	kb := &types.KnowledgeBase{
		ID: uuid.NewString(), TenantID: tenant.ID, CreatorID: user.ID, Name: "private-v2",
		Type: "document", EmbeddingModelID: model.ID,
		IndexingStrategy: types.IndexingStrategy{VectorEnabled: true, KeywordEnabled: true},
	}
	require.NoError(t, db.Create(kb).Error)
	require.NoError(t, db.Exec("INSERT INTO feishu_private_kbs(kb_id,owner_id) VALUES(?,?)", kb.ID, user.ID).Error)
	ks := &knowledgeService{
		config: &config.Config{}, repo: repository.NewKnowledgeRepository(db),
		kbService: &legacyLiveKB{db: db}, modelService: &v2PipelineModel{legacyLiveModel{db: db}},
		tenantService: &legacyNoCloud{}, tenantRepo: repository.NewTenantRepository(db),
		chunkRepo: repository.NewChunkRepository(db), task: &dispatchQueue{}, graphEngine: parentChildGraphRepo{},
		fileSvc: files.NewLocalFileService(t.TempDir(), ""),
	}
	s := NewFeishuPersonalService(db, repository.NewSystemSettingRepository(db), ks, ks.kbService, nil)
	require.NoError(t, EnablePersonalRecovery(s, ks, nil, t.TempDir()))
	s.newClient = func(cfg *core.Config) *core.Client { cfg.BaseURL = remote.URL; return core.NewClient(cfg) }
	ctx := types.WithCaller(
		context.Background(),
		types.Caller{TenantID: tenant.ID, UserID: user.ID, Role: types.TenantRoleAdmin},
	)
	ctx = context.WithValue(ctx, types.UserIDContextKey, user.ID)
	ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: user.ID})
	ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
	grant, err := access.ResolveKB(
		ctx,
		access.KBRequest{Caller: types.CallerFromContext(ctx)},
		kb,
		types.OrgRoleEditor,
		nil,
		nil,
	)
	require.NoError(t, err)
	ctx = grant.Context(ctx)
	require.NoError(t, s.SaveApp(ctx, &FeishuAppConfig{
		AppID: "cli_test", Secret: "synthetic-only", Enabled: true,
		RedirectURI:  "https://test.invalid/feishu/callback",
		Capabilities: &types.FeishuAccessCapabilities{PersonalEnabled: true},
	}))
	cipher, err := sealFeishu(
		feishuTokens{UserID: user.ID, Access: "synthetic-user-token", Refresh: "synthetic-refresh"},
	)
	require.NoError(t, err)
	require.NoError(t, db.Create(&types.FeishuConnection{
		UserID: user.ID, AppID: "cli_test", OpenID: "synthetic-open",
		Tokens: cipher, ExpiresAt: time.Now().Add(time.Hour), RefreshExpiresAt: time.Now().Add(2 * time.Hour),
	}).Error)
	require.NoError(t, db.Create(&types.FeishuPersonalGrantEpoch{UserID: user.ID, Epoch: uuid.NewString()}).Error)
	_, _, credential, err := s.ResolveCredentialReference(ctx, types.FeishuUserOAuth, "")
	require.NoError(t, err)
	link := "https://test.feishu.cn/file/real"
	cfg := &types.DataSourceConfig{
		Credentials: map[string]interface{}{}, ResourceIDs: []string{"links"},
		Settings: map[string]interface{}{"links": []string{link}, "credential_reference": types.FeishuSourceReference{
			Version: 1, OwnerUserID: user.ID, Kind: credential.Kind, ConnectionID: credential.ConnectionID,
			AppID: credential.AppID, IdentityVersion: credential.ConfigVersion,
		}},
	}
	blob, err := cfg.ToJSON()
	require.NoError(t, err)
	ds := &types.DataSource{
		ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID,
		Name: "v2-native", Type: types.ConnectorTypeFeishu, Status: types.DataSourceStatusActive, Config: blob,
	}
	expectedKnowledgeID := ""
	if firstCreate {
		f := &personalRecoveryFixture{t: t, db: db, s: s, p: s.recovery, ks: ks, ctx: ctx}
		m, e := s.ImportWithIdentity(ctx, kb.ID, link, types.FeishuUserOAuth)
		require.NoError(t, e)
		require.NoError(t, f.p.Process(ctx, f.payload(m.AttemptID)))
		var k types.Knowledge
		require.NoError(t, db.WithContext(ctx).Where("id = ?", m.LogicalKnowledgeID).First(&k).Error)
		op, e := ks.repo.(interfaces.KnowledgeDispatchRepository).GetKnowledgeDispatch(ctx, tenant.ID, k.ID)
		require.NoError(t, e)
		var intent types.KnowledgeDispatch
		require.NoError(t, json.Unmarshal(op.Payload, &intent))
		payload := intent.Task
		no := false
		require.NoError(t, k.SetProcessOverrides(&types.KnowledgeProcessOverrides{SummaryEnabled: &no}))
		require.NoError(t, ks.repo.UpdateKnowledge(ctx, &k))
		body, e := json.Marshal(payload)
		require.NoError(t, e)
		require.NoError(t, ks.ProcessDocument(ctx, asynq.NewTask(types.TypeDocumentProcess, body)))
		post := NewKnowledgePostProcessService(ks.repo, ks.kbService, nil, ks.chunkRepo, ks.task,
			repository.NewTaskPendingOpsRepository(db), nil, nil)
		body, e = json.Marshal(types.KnowledgePostProcessPayload{
			TenantID: tenant.ID, KnowledgeBaseID: kb.ID,
			KnowledgeID: k.ID, Attempt: payload.Attempt,
		})
		require.NoError(t, e)
		require.NoError(t, post.Handle(ctx, asynq.NewTask(types.TypeKnowledgePostProcess, body)))
		s.dataSources = &v2HandoffSources{db: db}
		ds, e = s.EnableImportSource(ctx, kb.ID, m.ID, "")
		require.NoError(t, e)
		require.Equal(t, types.DataSourceStatusActive, ds.Status)
		expectedKnowledgeID = k.ID
		revision.Add(60)
	} else {
		require.NoError(t, db.Create(ds).Error)
	}
	log := &types.SyncLog{
		ID: uuid.NewString(), DataSourceID: ds.ID, TenantID: tenant.ID,
		Status: types.SyncLogStatusRunning, ExecutionGeneration: 1, ExecutionClaimed: true,
	}
	require.NoError(t, db.Create(log).Error)
	ctx = types.WithSyncExecution(
		ctx,
		types.SyncExecution{DataSourceID: ds.ID, SyncLogID: log.ID, TenantID: tenant.ID, Generation: 1},
	)
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)
	ingest := &DataSourceService{knowledgeService: ks, feishuIdentity: s}
	cycle := func() (context.Context, *types.FetchedItem) {
		t.Helper()
		bound, e := ingest.bindFeishuSource(types.WithLegacyReceiptSet(ctx), ds)
		require.NoError(t, e)
		client := s.client(&core.Config{AppID: "cli_test", AppSecret: "synthetic-only"})
		resource, e := core.ParsePersonalResourceURL(link)
		require.NoError(t, e)
		resource, e = client.ResolveBoundResource(bound, resource)
		require.NoError(t, e)
		items, e := (core.ReferenceLinkOps{}).Fetch(bound, client, resource, "links", false)
		require.NoError(t, e)
		require.Len(t, items, 1)
		return bound, items[0]
	}
	bound, item := cycle()
	updated, err := ingest.ingestItem(bound, ds, item, nil)
	require.NoError(t, err)
	require.Equal(t, firstCreate, updated)
	receipts := types.LegacyReceipts(bound)
	require.Len(t, receipts, 1)
	id := receipts[0].KnowledgeID
	if expectedKnowledgeID != "" {
		require.Equal(t, expectedKnowledgeID, id, "first-create, handoff and changed update retain one Knowledge ID")
	}
	require.Positive(t, receipts[0].Vectors)
	require.Equal(t, receipts[0].Vectors, receipts[0].BM25Rows)
	calls := modelCalls.Load()
	_, err = ingest.ingestItem(bound, ds, item, nil)
	var duplicate *types.DuplicateKnowledgeError
	require.ErrorAs(t, err, &duplicate)
	require.Equal(t, calls, modelCalls.Load(), "unchanged revision must not re-embed")
	revision.Add(60)
	bound, item = cycle()
	updated, err = ingest.ingestItem(bound, ds, item, nil)
	require.NoError(t, err)
	require.True(t, updated)
	require.Equal(t, id, types.LegacyReceipts(bound)[0].KnowledgeID)
	snapshot := func() string {
		t.Helper()
		var out string
		require.NoError(t, db.WithContext(ctx).Raw(`SELECT jsonb_build_object('knowledge',
			(SELECT to_jsonb(k) FROM knowledges k WHERE id=?), 'chunks',
			(SELECT jsonb_agg(to_jsonb(c) ORDER BY c.id) FROM chunks c WHERE knowledge_id=?), 'embeddings',
			(SELECT jsonb_agg(to_jsonb(e) ORDER BY to_jsonb(e)::text) FROM embeddings e WHERE knowledge_id=?))::text`,
			id, id, id).Scan(&out).Error)
		return out
	}
	before := snapshot()
	revision.Add(60)
	bound, item = cycle()
	rejectModel.Store(true)
	_, err = ingest.ingestItem(bound, ds, item, nil)
	require.Error(t, err)
	require.JSONEq(t, before, snapshot(), "candidate failure preserves knowledge, chunks, vectors and BM25 rows")
	calls = modelCalls.Load()
	require.NoError(t, s.Disconnect(ctx))
	require.Error(t, types.CheckFeishuSourceRead(bound))
	_, err = ingest.bindFeishuSource(ctx, ds)
	require.Error(t, err)
	require.Equal(t, calls, modelCalls.Load(), "revoked identity cannot consume model calls")
}
