//go:build sqlite_fts5 && knowhub_m6_postgres

package router

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	pgdriver "github.com/Tencent/WeKnora/internal/application/repository/retriever/postgres"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/config"
	projectdb "github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/rerank"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPortalM7PublishedDynamicRealPostgres(t *testing.T) {
	dsn := os.Getenv("KNOWHUB_M6_POSTGRES_DSN")
	require.Equal(t, "host=/work/pgsocket user=postgres dbname=knowhub_m6 sslmode=disable", dsn)
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, admin.Exec("CREATE DATABASE knowhub_m7_dynamic").Error)
	pool, _ := admin.DB()
	defer pool.Close()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(filepath.Clean(filepath.Join(cwd, "../.."))))
	err = projectdb.RunMigrationsWithOptions("postgresql://postgres@/knowhub_m7_dynamic?host="+url.QueryEscape("/work/pgsocket")+"&sslmode=disable", projectdb.MigrationOptions{})
	require.NoError(t, os.Chdir(cwd))
	require.NoError(t, err)
	db, err := gorm.Open(postgres.Open(strings.Replace(dsn, "dbname=knowhub_m6", "dbname=knowhub_m7_dynamic", 1)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	dbpool, _ := db.DB()
	defer dbpool.Close()
	f := fixture.Seed(t, db)
	f.PreparePublished(t)
	driver := pgdriver.NewPostgresRetrieveEngineRepository(db)
	vector := make([]float32, 1024)
	vector[0] = 1
	var mu sync.Mutex
	wire := map[string][]string{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, e := io.ReadAll(r.Body)
		require.NoError(t, e)
		require.NotContains(t, string(raw), fixture.Canary)
		mu.Lock()
		wire[r.URL.Path] = append(wire[r.URL.Path], string(raw))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/embeddings":
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"index": 0, "embedding": vector}}}))
		case "/v1/rerank":
			_, _ = io.WriteString(w, `{"results":[{"index":0,"relevance_score":0.9}]}`)
		case "/v1/chat/completions":
			_, _ = io.WriteString(w, `{"choices":[{"index":0,"message":{"role":"assistant","content":"SYNTHETIC_OK"}}]}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	secutils.ResetSSRFWhitelistForTest()
	defer secutils.ResetSSRFWhitelistForTest()
	for _, m := range []struct {
		id     string
		tenant uint64
	}{{"synthetic", 99}, {"model-public-two", 1}, {"model-private-home", 1}, {"future-model", 3}} {
		if m.tenant == 3 {
			continue
		}
		require.NoError(t, db.Create(&types.Model{ID: m.id, TenantID: m.tenant, Name: "controlled-1024", Type: types.ModelTypeEmbedding, Source: types.ModelSourceRemote, Status: types.ModelStatusActive, Parameters: types.ModelParameters{BaseURL: upstream.URL + "/v1", Provider: "openai", EmbeddingParameters: types.EmbeddingParameters{Dimension: 1024}}}).Error)
	}
	engines := `[{"retriever_type":"keywords","retriever_engine_type":"postgres"},{"retriever_type":"vector","retriever_engine_type":"postgres"}]`
	require.NoError(t, db.Exec("UPDATE tenants SET retriever_engines=? WHERE id IN (1,99)", engines).Error)
	registry := retriever.NewRetrieveEngineRegistry(nil, nil)
	require.NoError(t, registry.Register(retriever.NewKVHybridRetrieveEngine(driver, types.PostgresRetrieverEngineType)))
	models := service.NewModelService(repository.NewModelRepository(db), repository.NewKnowledgeBaseRepository(db), nil, nil, nil, nil)
	on := true
	cfg := &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &on}, PersonalMCP: &config.PersonalMCPConfig{Enabled: true, PolicyReference: "ISOLATED_DYNAMIC"}}
	members := portalM7Members{documentTestMembers{repo: repository.NewTenantMemberRepository(db)}}
	tenants := documentTestTenants{repo: repository.NewTenantRepository(db)}
	users := service.NewUserService(cfg, repository.NewUserRepository(db), repository.NewAuthTokenRepository(db), tenants, members, nil)
	g := gin.New()
	tokens := portalM7Management(t, db, g, cfg)
	kbService := service.NewKnowledgeBaseService(repository.NewKnowledgeBaseRepository(db), repository.NewKnowledgeRepository(db), repository.NewChunkRepository(db), nil, nil, models, nil, registry, nil, repository.NewTenantRepository(db), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	guards := &rbacGuards{cfg: cfg, kbService: kbService}
	group := g.Group("/api/v1")
	group.Use(middleware.Auth(tenants, users, members, nil, cfg), guards.ensureAPIKeyAuthorizer().Middleware(), middleware.ErrorHandler())
	RegisterKnowledgeBaseRoutes(group, handler.NewKnowledgeBaseHandler(cfg, kbService, nil, nil, nil, nil, nil, users, nil, nil, nil), guards)
	request := func(actor, tenant, method, path string, body any) *httptest.ResponseRecorder {
		raw, e := json.Marshal(body)
		require.NoError(t, e)
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+tokens[actor])
		r.Header.Set("Content-Type", "application/json")
		if tenant != "" {
			r.Header.Set("X-Tenant-ID", tenant)
		}
		w := httptest.NewRecorder()
		g.ServeHTTP(w, r)
		return w
	}
	s := service.NewPortalPersonalKnowledge(cfg, db, members, repository.NewTenantAPIKeyRepository(db), repository.NewKnowledgeBaseRepository(db), repository.NewTenantRepository(db), repository.NewKnowledgeRepository(db), repository.NewChunkRepository(db), models, registry, nil, nil, nil)
	require.NotNil(t, s)
	index := func(id string) {
		var c types.Chunk
		require.NoError(t, db.Where("id=?", id).Take(&c).Error)
		require.NoError(t, driver.Save(context.Background(), &types.IndexInfo{SourceID: c.ID, SourceType: types.ChunkSourceType, ChunkID: c.ID, KnowledgeID: c.KnowledgeID, KnowledgeBaseID: c.KnowledgeBaseID, Content: c.Content, IsEnabled: true}, map[string]any{"embedding": map[string][]float32{c.ID: vector}}))
	}
	var chunks []types.Chunk
	require.NoError(t, db.Find(&chunks).Error)
	for _, c := range chunks {
		index(c.ID)
	}
	reranker, err := rerank.NewReranker(&rerank.RerankerConfig{Source: types.ModelSourceRemote, Provider: "jina", BaseURL: upstream.URL + "/v1", APIKey: "synthetic-no-secret", ModelName: "jina-reranker-v2-base-multilingual"})
	require.NoError(t, err)
	llm, err := chat.NewRemoteChat(&chat.ChatConfig{Source: types.ModelSourceRemote, Provider: "openai", BaseURL: upstream.URL + "/v1", APIKey: "synthetic-no-secret", ModelName: "gpt-4o-mini"})
	require.NoError(t, err)
	s.Consumers = []func(context.Context, []service.PortalMCPHit) error{
		func(ctx context.Context, hits []service.PortalMCPHit) error {
			if len(hits) == 0 {
				return nil
			}
			texts := []string{}
			for _, h := range hits {
				texts = append(texts, h.Title+"\n"+h.Text)
			}
			_, e := reranker.Rerank(ctx, "PUBLIC", texts)
			return e
		},
		func(ctx context.Context, hits []service.PortalMCPHit) error {
			if len(hits) == 0 {
				return nil
			}
			raw, e := json.Marshal(hits)
			if e != nil {
				return e
			}
			_, e = llm.Chat(ctx, []chat.Message{{Role: "user", Content: string(raw)}}, nil)
			return e
		},
	}
	a := fixture.Key("A", 1, 1, true)
	search := func(ids []string, query string) *service.PortalMCPSearchResult {
		r, e := s.Search(a, ids, query, "hybrid", 30)
		require.NoError(t, e)
		require.Equal(t, "complete", r.Status)
		require.Zero(t, r.Failed)
		for _, h := range r.Results {
			require.NotContains(t, h.Text, fixture.Canary)
		}
		return r
	}
	initial := search(nil, "PUBLIC")
	require.Equal(t, 3, initial.Searched)
	require.NoError(t, db.Exec("INSERT INTO tenants(id,name,business,retriever_engines) VALUES(3,'third','synthetic',?)", engines).Error)
	require.NoError(t, db.Exec("INSERT INTO tenant_members(user_id,tenant_id,role,status) VALUES('owner',3,'owner','active')").Error)
	require.NoError(t, db.Exec("INSERT INTO tenant_portal_configs(tenant_id,status,display_name,created_by,updated_by) VALUES(3,'draft','third','owner','owner')").Error)
	require.NoError(t, db.Create(&types.Model{ID: "future-model", TenantID: 3, Name: "controlled-1024", Type: types.ModelTypeEmbedding, Source: types.ModelSourceRemote, Status: types.ModelStatusActive, Parameters: types.ModelParameters{BaseURL: upstream.URL + "/v1", Provider: "openai", EmbeddingParameters: types.EmbeddingParameters{Dimension: 1024}}}).Error)
	addKB := func(id string) {
		body := map[string]any{"id": id, "name": id, "tenant_id": 99, "type": "document", "embedding_model_id": "future-model", "summary_model_id": "synthetic"}
		denied := request("A", "1", "POST", "/api/v1/knowledge-bases", body)
		require.Equal(t, http.StatusForbidden, denied.Code, denied.Body.String())
		created := request("owner", "3", "POST", "/api/v1/knowledge-bases", body)
		require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
		var kb types.KnowledgeBase
		require.NoError(t, db.Where("id=?", id).Take(&kb).Error)
		require.Equal(t, uint64(3), kb.TenantID, "existing create service binds server tenant, ignoring body tenant")
		require.Equal(t, "owner", kb.CreatorID)
	}
	addDoc := func(kb, id, channel, status string) {
		require.NoError(t, db.Exec("INSERT INTO knowledges(id,tenant_id,knowledge_base_id,type,title,source,file_name,channel,parse_status,enable_status) VALUES(?,3,?,'file',?,'synthetic-local','synthetic.txt',?,?,'enabled')", id, kb, "PUBLIC "+id, channel, status).Error)
		require.NoError(t, db.Exec("INSERT INTO chunks(id,tenant_id,knowledge_base_id,knowledge_id,content,chunk_type,content_revision,is_enabled,chunk_index,start_at,end_at) VALUES(?,3,?,?,?,'text',1,true,0,0,40)", id+"-chunk", kb, id, "PUBLIC "+id).Error)
		index(id + "-chunk")
	}
	addKB("third-kb")
	addDoc("third-kb", "third-doc", "web", "completed")
	_, err = s.Search(a, []string{"third-kb"}, "PUBLIC", "hybrid", 30)
	require.Error(t, err, "unpublished third space cannot enter model inputs")
	denied := request("A", "1", "POST", "/api/v1/system/admin/portal/spaces/3/publish", nil)
	require.Equal(t, http.StatusForbidden, denied.Code, denied.Body.String())
	published := request("reviewer", "", "POST", "/api/v1/system/admin/portal/spaces/3/publish", nil)
	require.Equal(t, http.StatusOK, published.Code, published.Body.String())
	require.NotEmpty(t, search([]string{"third-kb"}, "third-doc").Results)
	addKB("new-kb")
	require.NotEmpty(t, search([]string{"new-kb"}, "PUBLIC").Status, "empty KB is discovered dynamically")
	addDoc("new-kb", "new-doc", "web", "processing")
	require.Empty(t, search([]string{"new-kb"}, "new-doc").Results)
	require.NoError(t, db.Exec("UPDATE knowledges SET parse_status='completed' WHERE id='new-doc'").Error)
	require.Equal(t, "new-doc", search([]string{"new-kb"}, "new-doc").Results[0].DocumentID)
	addDoc("new-kb", "synced-feishu-shape", "feishu_personal", "completed")
	require.NotEmpty(t, search([]string{"new-kb"}, "synced-feishu-shape").Results, "indexed Feishu output shape is not excluded by channel; actual ingress tested separately")
	require.NoError(t, f.Store.SetPortalKBPublicBlock(context.Background(), 3, "third-kb", "owner", "synthetic sensitive KB", 0, true))
	_, err = s.ReadDocument(a, "third-doc")
	require.Error(t, err)
	withdrawn := request("reviewer", "", "POST", "/api/v1/system/admin/portal/spaces/3/unpublish", nil)
	require.Equal(t, http.StatusOK, withdrawn.Code, withdrawn.Body.String())
	_, err = s.ReadDocument(a, "new-doc")
	require.Error(t, err)
	_, err = s.ReadDocument(a, "private-home-doc")
	require.NoError(t, err)
	require.NoError(t, db.Exec("UPDATE tenant_portal_configs SET status='published' WHERE tenant_id=3").Error)
	old, err := s.Resolve(a, []string{"new-kb"})
	require.NoError(t, err)
	require.NoError(t, db.Exec("INSERT INTO tenants(id,name,business) VALUES(4,'unpublished-owner','synthetic')").Error)
	require.NoError(t, db.Exec("UPDATE knowledge_bases SET tenant_id=4 WHERE id='new-kb'").Error)
	require.NoError(t, db.Exec("UPDATE knowledges SET tenant_id=4 WHERE knowledge_base_id='new-kb'").Error)
	require.NoError(t, db.Exec("UPDATE chunks SET tenant_id=4 WHERE knowledge_base_id='new-kb'").Error)
	require.Error(t, old[0].Revalidate(a))
	_, err = s.ReadDocument(a, "new-doc")
	require.Error(t, err, "moving KB out of Published space invalidates old Chunk references")
	require.NoError(t, db.Exec("UPDATE knowledge_bases SET tenant_id=3 WHERE id='new-kb'").Error)
	require.NoError(t, db.Exec("UPDATE knowledges SET tenant_id=3 WHERE knowledge_base_id='new-kb'").Error)
	require.NoError(t, db.Exec("UPDATE chunks SET tenant_id=3 WHERE knowledge_base_id='new-kb'").Error)
	require.NoError(t, db.Exec("UPDATE tenants SET status='inactive' WHERE id=3").Error)
	_, err = s.ReadDocument(a, "new-doc")
	require.Error(t, err)
	require.NoError(t, db.Exec("UPDATE tenants SET status='active' WHERE id=3").Error)
	old, err = s.Resolve(a, []string{"new-kb"})
	require.NoError(t, err)
	require.NoError(t, db.Exec("UPDATE knowledge_bases SET deleted_at=CURRENT_TIMESTAMP WHERE id='new-kb'").Error)
	require.Error(t, old[0].Revalidate(a))
	_, err = s.ReadDocument(a, "new-doc")
	require.Error(t, err)
	// All real outbound model payloads were recorded; a final result filter
	// cannot erase a leak here. Exact canary check also runs at HTTP receipt.
	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, wire["/v1/embeddings"])
	require.NotEmpty(t, wire["/v1/rerank"])
	require.NotEmpty(t, wire["/v1/chat/completions"])
	for _, requests := range wire {
		for _, raw := range requests {
			require.NotContains(t, raw, fixture.Canary)
		}
	}
	t.Log("M7_DYNAMIC: Published no-approval, future space/KB/index, public exception, withdrawal, old Chunk and private union PASS; Feishu ingress and Nginx parity have separate evidence")
}
