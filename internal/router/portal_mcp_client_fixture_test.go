//go:build sqlite_fts5 && knowhub_m7_clients

package router

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	pgdriver "github.com/Tencent/WeKnora/internal/application/repository/retriever/postgres"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/config"
	projectdb "github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/mcpserver"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Explicit manual-client fixture, never included in ordinary Go tests. Its
// server listens only on test-host loopback and is reached through pinned SSH.
// Secrets go to a private runtime file outside source/evidence, never stdout.
func TestPortalM7ClientFixture(t *testing.T) {
	dir := os.Getenv("KNOWHUB_CLIENT_DIRECTORY")
	require.Equal(t, "/client", dir)
	dsn := os.Getenv("KNOWHUB_M6_POSTGRES_DSN")
	require.Equal(t, "host=/work/pgsocket user=postgres dbname=knowhub_m6 sslmode=disable", dsn)
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(filepath.Clean(filepath.Join(cwd, "../.."))))
	err = projectdb.RunMigrationsWithOptions("postgresql://postgres@/knowhub_m6?host="+url.QueryEscape("/work/pgsocket")+"&sslmode=disable", projectdb.MigrationOptions{})
	require.NoError(t, os.Chdir(cwd))
	require.NoError(t, err)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	defer func() { sql, _ := db.DB(); sql.Close() }()
	f := fixture.Seed(t, db)
	f.PreparePublished(t)
	// A second independent private space exercises the personal page's default
	// union of all server-authorized scopes, without publishing that space.
	require.NoError(t, db.Exec("INSERT INTO tenants(id,name,business) VALUES(7,'synthetic-private-other','synthetic')").Error)
	require.NoError(t, db.Exec("INSERT INTO tenant_members(user_id,tenant_id,role,status) VALUES('A',7,'viewer','active')").Error)
	require.NoError(t, db.Exec("INSERT INTO knowledge_bases(id,name,tenant_id,type,embedding_model_id,summary_model_id) VALUES('private-other','private-other',7,'document','model-private-other','synthetic')").Error)
	require.NoError(t, db.Exec("INSERT INTO knowledges(id,tenant_id,knowledge_base_id,type,title,source,file_name,channel,parse_status,enable_status) VALUES('private-other-doc',7,'private-other','file','PUBLIC_PRIVATE_OTHER','synthetic-local','private.txt','web','completed','enabled')").Error)
	require.NoError(t, db.Exec("INSERT INTO chunks(id,tenant_id,knowledge_base_id,knowledge_id,content,chunk_type,content_revision,is_enabled,chunk_index,start_at,end_at) VALUES('private-other-chunk',7,'private-other','private-other-doc','PUBLIC_PRIVATE_OTHER','text',1,true,0,0,20)").Error)
	driver := pgdriver.NewPostgresRetrieveEngineRepository(db)
	vector := make([]float32, 1024)
	vector[0] = 1
	var chunks []*types.Chunk
	require.NoError(t, db.Find(&chunks).Error)
	for _, c := range chunks {
		require.NoError(t, driver.Save(context.Background(), &types.IndexInfo{SourceID: c.ID, SourceType: types.ChunkSourceType, ChunkID: c.ID, KnowledgeID: c.KnowledgeID, KnowledgeBaseID: c.KnowledgeBaseID, Content: c.Content, IsEnabled: true}, map[string]any{"embedding": map[string][]float32{c.ID: vector}}))
	}
	s := &service.PortalMCPKnowledge{Knowledge: repository.NewKnowledgeRepository(db), Chunks: repository.NewChunkRepository(db)}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		require.NotContains(t, string(mustJSONM7(payload)), fixture.Canary)
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []any{map[string]any{"object": "embedding", "index": 0, "embedding": vector}}}))
	}))
	defer upstream.Close()
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	secutils.ResetSSRFWhitelistForTest()
	defer secutils.ResetSSRFWhitelistForTest()
	for _, m := range []struct {
		id     string
		tenant uint64
	}{{"synthetic", 99}, {"model-public-two", 1}, {"model-private-home", 1}, {"model-private-other", 7}} {
		require.NoError(t, db.Create(&types.Model{ID: m.id, TenantID: m.tenant, Name: "synthetic-1024", Type: types.ModelTypeEmbedding, Source: types.ModelSourceRemote, Status: types.ModelStatusActive, Parameters: types.ModelParameters{BaseURL: upstream.URL + "/v1", Provider: "openai", EmbeddingParameters: types.EmbeddingParameters{Dimension: 1024}}}).Error)
	}
	require.NoError(t, db.Exec(`UPDATE tenants SET retriever_engines='[{"retriever_type":"keywords","retriever_engine_type":"postgres"},{"retriever_type":"vector","retriever_engine_type":"postgres"}]' WHERE id IN (1,7,99)`).Error)
	registry := retriever.NewRetrieveEngineRegistry(nil, nil)
	require.NoError(t, registry.Register(retriever.NewKVHybridRetrieveEngine(driver, types.PostgresRetrieverEngineType)))
	service.BindPortalMCPAdapters(s, repository.NewKnowledgeBaseRepository(db), repository.NewTenantRepository(db), service.NewModelService(repository.NewModelRepository(db), repository.NewKnowledgeBaseRepository(db), nil, nil, nil, nil), registry, nil)
	require.NoError(t, db.Exec("SELECT setval(pg_get_serial_sequence('tenant_api_keys','id'),(SELECT max(id) FROM tenant_api_keys))").Error)
	keys := service.NewTenantAPIKeyService(repository.NewTenantAPIKeyRepository(db))
	expires := time.Now().Add(40 * time.Minute)
	created, err := keys.CreateAPIKey(fixture.Web("A", 1), interfaces.TenantAPIKeyCreateRequest{ScopeType: types.APIKeyScopeUserMCP, OwnerUserID: "A", Name: "synthetic-real-client", ClientType: types.MCPClientGeneric, Capabilities: []string{"retrieve", "enterprise_retrieve"}, ExpiresAt: &expires, TenantScopes: []types.APIKeyTenantScope{{TenantID: 1, KBScopeMode: types.APIKeyKBScopeAll}}})
	require.NoError(t, err)
	other, err := keys.CreateAPIKey(fixture.Web("C", 99), interfaces.TenantAPIKeyCreateRequest{ScopeType: types.APIKeyScopeUserMCP, OwnerUserID: "C", Name: "synthetic-other-employee", ClientType: types.MCPClientGeneric, Capabilities: []string{"retrieve", "enterprise_retrieve"}, ExpiresAt: &expires, TenantScopes: []types.APIKeyTenantScope{{TenantID: 99, KBScopeMode: types.APIKeyKBScopeAll}}})
	require.NoError(t, err)
	on := true
	cfg := &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &on}, MCPPublicURL: "http://127.0.0.1:14881", PersonalMCP: &config.PersonalMCPConfig{Enabled: true, PolicyReference: "ISOLATED_PUBLISHED_DEFAULT_USER_PRODUCT_RULE", ExceptionsEnabled: true, SystemAdminReview: true}}
	s = service.NewPortalPersonalKnowledge(cfg, db, documentTestMembers{repo: repository.NewTenantMemberRepository(db)}, repository.NewTenantAPIKeyRepository(db), repository.NewKnowledgeBaseRepository(db), repository.NewTenantRepository(db), repository.NewKnowledgeRepository(db), repository.NewChunkRepository(db), service.NewModelService(repository.NewModelRepository(db), repository.NewKnowledgeBaseRepository(db), nil, nil, nil, nil), registry, nil, nil, nil)
	require.NotNil(t, s)
	g := gin.New()
	gin.SetMode(gin.ReleaseMode)
	protocol, err := os.OpenFile(filepath.Join(dir, "protocol.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	require.NoError(t, err)
	defer protocol.Close()
	var logMu sync.Mutex
	g.Use(func(c *gin.Context) {
		start := time.Now()
		var rpc struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		body, e := io.ReadAll(io.LimitReader(c.Request.Body, 65537))
		if e != nil || len(body) > 65536 {
			c.AbortWithStatus(413)
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		_ = json.Unmarshal(body, &rpc)
		c.Next()
		logMu.Lock()
		defer logMu.Unlock()
		_ = json.NewEncoder(protocol).Encode(map[string]any{"utc": start.UTC(), "http_method": c.Request.Method, "rpc_id": rpc.ID, "rpc_method": rpc.Method, "tool": rpc.Params.Name, "status": c.Writer.Status(), "bytes": c.Writer.Size(), "duration_ms": float64(time.Since(start).Microseconds()) / 1000})
	})
	RegisterPersonalMCPRoutes(g, mcpserver.NewConfiguredPersonalServer(cfg, s), documentTestTenants{repo: repository.NewTenantRepository(db)}, documentTestUsers{repo: repository.NewUserRepository(db)}, documentTestMembers{repo: repository.NewTenantMemberRepository(db)}, keys, cfg)
	jwt := portalM7Management(t, db, g, cfg)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := &http.Server{Handler: g, ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(listener)
	defer server.Close()
	runtime, err := json.Marshal(map[string]any{"url": "http://" + listener.Addr().String() + "/api/v1/mcp/personal", "token": created.Token, "other_token": other.Token, "key_id": created.APIKey.ID, "expires_at": expires, "owner_jwt": jwt["owner"], "reviewer_jwt": jwt["reviewer"], "user_jwt": jwt["A"]})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "connection-runtime.json"), runtime, 0600))
	t.Log("M7_CLIENT_READY: private runtime file created; real PostgreSQL and existing user_mcp authentication")
	deadline := time.Now().Add(30 * time.Minute)
	revoked := false
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "stop-client-fixture")); err == nil {
			return
		}
		if _, err := os.Stat(filepath.Join(dir, "revoke-test-key")); err == nil && !revoked {
			require.NoError(t, keys.(interfaces.UserMCPAPIKeyService).RevokeUserMCPAPIKey(context.Background(), "A", created.APIKey.ID))
			revoked = true
		}
		if _, err := os.Stat(filepath.Join(dir, "withdraw-public-space")); err == nil {
			require.NoError(t, db.Exec("UPDATE tenant_portal_configs SET status='draft' WHERE tenant_id=99").Error)
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("manual client fixture bounded timeout")
}

func mustJSONM7(v any) []byte { b, _ := json.Marshal(v); return b }
