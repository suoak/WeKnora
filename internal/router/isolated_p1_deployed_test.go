//go:build sqlite_fts5 && knowhub_isolated_p1

package router

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	pgdriver "github.com/Tencent/WeKnora/internal/application/repository/retriever/postgres"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/config"
	projectdb "github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/mcpserver"
	"github.com/Tencent/WeKnora/internal/middleware"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type p1MetadataSink struct {
	mu   sync.Mutex
	path string
}

func (s *p1MetadataSink) Record(_ context.Context, report types.MCPUsageReport) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(report)
}

// Opt-in deployed fixture: real migrations, repositories, auth and read tools.
// Only this dedicated disposable DB/loopback port is permitted. No production
// connection, pre-existing credentials, external model or existing files.
func TestIsolatedP1DeployedFixture(t *testing.T) {
	const dsn = "host=127.0.0.1 port=18643 user=postgres dbname=knowhub_isolated_p1_20261111 sslmode=disable"
	dir := os.Getenv("KNOWHUB_ISOLATED_P1_DIR")
	require.NotEmpty(t, dir)
	require.Equal(t, "knowhub-isolated-p1-20261111", filepath.Base(dir))
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(filepath.Join(cwd, "../..")))
	err = projectdb.RunMigrationsWithOptions("postgresql://postgres@127.0.0.1:18643/knowhub_isolated_p1_20261111?sslmode=disable", projectdb.MigrationOptions{})
	require.NoError(t, os.Chdir(cwd))
	require.NoError(t, err)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	defer pool.Close()
	f := fixture.Seed(t, db)
	f.PreparePublished(t)
	driver := pgdriver.NewPostgresRetrieveEngineRepository(db)
	vector := []float32{1, 0, 0}
	var chunks []*types.Chunk
	require.NoError(t, db.Find(&chunks).Error)
	for _, c := range chunks {
		require.NoError(t, driver.Save(context.Background(), &types.IndexInfo{SourceID: c.ID, SourceType: types.ChunkSourceType, ChunkID: c.ID, KnowledgeID: c.KnowledgeID, KnowledgeBaseID: c.KnowledgeBaseID, Content: c.Content, IsEnabled: true}, map[string]any{"embedding": map[string][]float32{c.ID: vector}}))
	}
	// A controlled local wire fixture exercises the actual embedding adapter;
	// it never contacts a vendor and deliberately records no request payload.
	modelListener, err := net.Listen("tcp", "127.0.0.1:18645")
	require.NoError(t, err)
	var modelCalls atomic.Int64
	var slowActive atomic.Int64
	model := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/metrics" {
			_ = json.NewEncoder(w).Encode(map[string]int64{"slow_active": slowActive.Load()})
			return
		}
		modelCalls.Add(1)
		input, _ := io.ReadAll(io.LimitReader(r.Body, 16384))
		if strings.Contains(string(input), "P1_SLOW_QUERY") {
			slowActive.Add(1)
			defer slowActive.Add(-1)
			select {
			case <-r.Context().Done():
				return
			case <-time.After(35 * time.Second):
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"index": 0, "embedding": vector}}, "usage": map[string]int{"prompt_tokens": 1, "total_tokens": 1}})
	}), ReadHeaderTimeout: 5 * time.Second}
	go model.Serve(modelListener)
	defer model.Close()
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	for _, m := range []struct {
		id     string
		tenant uint64
	}{{"synthetic", 99}, {"model-public-two", 1}, {"model-private-home", 1}} {
		require.NoError(t, db.Create(&types.Model{ID: m.id, TenantID: m.tenant, Name: "synthetic-local-3d", Type: types.ModelTypeEmbedding, Source: types.ModelSourceRemote, Status: types.ModelStatusActive, Parameters: types.ModelParameters{BaseURL: "http://127.0.0.1:18645/v1", Provider: "openai", EmbeddingParameters: types.EmbeddingParameters{Dimension: 3}}}).Error)
	}
	require.NoError(t, db.Exec(`UPDATE tenants SET retriever_engines='[{"retriever_type":"keywords","retriever_engine_type":"postgres"},{"retriever_type":"vector","retriever_engine_type":"postgres"}]' WHERE id IN (1,99)`).Error)
	types.EnableLegacyReadGuards(db)
	wikis := repository.NewWikiPageRepository(db)
	for _, kb := range []string{"public-two", "private-home"} {
		ctx, err := repository.BeginWikiMCPGeneration(context.Background(), wikis, kb)
		require.NoError(t, err)
		require.NoError(t, wikis.Create(ctx, &types.WikiPage{ID: "p1-wiki-" + kb, TenantID: 1, KnowledgeBaseID: kb, Slug: "concept/verified", Title: "PUBLIC verified", Content: "PUBLIC synthetic Wiki https://storage.invalid/file?sig=SIGNED_LINK_CANARY file:///etc/passwd", PageType: types.WikiPageTypeConcept, Status: types.WikiPageStatusPublished, SourceRefs: types.StringArray{kb + "-doc|synthetic source"}, Version: 1}))
	}
	on := true
	cfg := &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &on}, PersonalMCP: &config.PersonalMCPConfig{Enabled: true, PolicyReference: "ISOLATED_P1_SYNTHETIC_ONLY"}}
	keyRepo := repository.NewTenantAPIKeyRepository(db)
	keys := service.NewTenantAPIKeyService(keyRepo)
	require.NoError(t, db.Exec("SELECT setval(pg_get_serial_sequence('tenant_api_keys','id'),(SELECT max(id) FROM tenant_api_keys))").Error)
	makeKey := func(user string, tenant uint64) string {
		row, err := keys.CreateAPIKey(fixture.Web(user, tenant), interfaces.TenantAPIKeyCreateRequest{ScopeType: types.APIKeyScopeUserMCP, OwnerUserID: user, Name: "isolated-p1-synthetic", ClientType: types.MCPClientGeneric, Capabilities: []string{"retrieve", "enterprise_retrieve"}, TenantScopes: []types.APIKeyTenantScope{{TenantID: tenant, KBScopeMode: types.APIKeyKBScopeAll}}})
		require.NoError(t, err)
		return row.Token
	}
	a, c := makeKey("A", 1), makeKey("C", 99)
	members := documentTestMembers{repo: repository.NewTenantMemberRepository(db)}
	registry := retriever.NewRetrieveEngineRegistry(nil, nil)
	require.NoError(t, registry.Register(retriever.NewKVHybridRetrieveEngine(driver, types.PostgresRetrieverEngineType)))
	knowledge := service.NewPortalPersonalKnowledge(cfg, db, members, keyRepo, repository.NewKnowledgeBaseRepository(db), repository.NewTenantRepository(db), repository.NewKnowledgeRepository(db), repository.NewChunkRepository(db), service.NewModelService(repository.NewModelRepository(db), repository.NewKnowledgeBaseRepository(db), nil, nil, nil, nil), registry, nil, nil, nil)
	require.NotNil(t, knowledge)
	g := gin.New()
	g.ContextWithFallback = true
	g.RedirectTrailingSlash = false
	g.RedirectFixedPath = false
	g.Use(middleware.RequestID(), middleware.Logger())
	personal := mcpserver.NewConfiguredPersonalServer(cfg, knowledge)
	personal.SetUsageRecorder(&p1MetadataSink{path: filepath.Join(dir, "usage-metadata.jsonl")})
	RegisterPersonalMCPRoutes(g, personal, documentTestTenants{repo: repository.NewTenantRepository(db)}, documentTestUsers{repo: repository.NewUserRepository(db)}, members, keys, cfg)
	var writeHandlers atomic.Int64
	// Tripwires model non-MCP write dispatch. No write implementation is wired
	// into the personal handler; the acceptance suite checks they stay at zero.
	g.NoRoute(func(c *gin.Context) { writeHandlers.Add(1); c.AbortWithStatus(418) })
	listener, err := net.Listen("tcp", "127.0.0.1:18644")
	require.NoError(t, err)
	app := &http.Server{Handler: g, ReadHeaderTimeout: 5 * time.Second, MaxHeaderBytes: 16 << 10}
	go app.Serve(listener)
	defer app.Close()
	private := filepath.Join(dir, "private")
	require.NoError(t, os.MkdirAll(private, 0700))
	runtime, err := json.Marshal(map[string]string{"key_a": a, "key_c": c, "http": "http://127.0.0.1:18644/api/v1/mcp/personal", "https": "https://localhost:8443/api/v1/mcp/personal"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(private, "clients.json"), runtime, 0600))
	// Snapshot content, storage/resource and task/session tables before clients.
	tables := []string{"knowledges", "chunks", "wiki_pages", "sessions", "messages", "resources", "resource_bindings"}
	snapshot := func() map[string]string {
		counts := map[string]string{}
		for _, name := range tables {
			var digest string
			// Test-only content digest detects updates as well as inserts/deletes;
			// no row text is written to evidence.
			require.NoError(t, db.Raw("SELECT count(*)::text || ':' || COALESCE(md5(string_agg(md5(row_to_json(t)::text), '' ORDER BY id)), '') FROM "+name+" t").Row().Scan(&digest))
			counts[name] = digest
		}
		return counts
	}
	before := snapshot()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ready.json"), []byte(`{"ready":true,"synthetic":true}`), 0600))
	deadline := time.Now().Add(30 * time.Minute)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "stop-fixture")); err == nil {
			after := snapshot()
			result, err := json.Marshal(map[string]any{"before_counts": before, "after_counts": after, "write_handler_calls": writeHandlers.Load(), "model_calls": modelCalls.Load()})
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "side-effects.json"), result, 0600))
			require.Equal(t, before, after)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("bounded P1 fixture timeout")
}
