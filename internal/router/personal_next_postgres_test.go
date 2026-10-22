//go:build sqlite_fts5

package router

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	pgdriver "github.com/Tencent/WeKnora/internal/application/repository/retriever/postgres"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/config"
	projectdb "github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/mcpserver"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func (s documentTestMembers) ListByUser(ctx context.Context, user string) ([]*types.TenantMember, error) {
	return s.repo.ListByUser(ctx, user)
}

func TestV1NextPostgresAuthenticatedEightToolsDynamicAndHold(t *testing.T) {
	dsn := feishulegacy.PrivateDSN(t)
	cwd, e := os.Getwd()
	require.NoError(t, e)
	require.NoError(t, os.Chdir(filepath.Join(cwd, "../..")))
	e = projectdb.RunMigrations(dsn)
	require.NoError(t, os.Chdir(cwd))
	require.NoError(t, e)
	db, e := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, e)
	pool, e := db.DB()
	require.NoError(t, e)
	defer pool.Close()
	f := fixture.Seed(t, db)
	f.PreparePublished(t)
	types.EnableLegacyReadGuards(db)
	t.Setenv("SYSTEM_AES_KEY", strings.Repeat("n", 32))
	var revokeDuringEmbedding atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		encoded, _ := json.Marshal(body)
		require.NotContains(t, string(encoded), fixture.Canary)
		require.NotContains(t, string(encoded), "HELD_CANARY")
		if strings.Contains(string(encoded), "REVOKE_DURING_QUERY") && revokeDuringEmbedding.CompareAndSwap(false, true) {
			require.NoError(t, db.Exec(`UPDATE data_sources SET legacy_access_state='revoked' WHERE id='during-revoke'`).Error)
		}
		vector := make([]float32, 1024)
		vector[0] = 1
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"index": 0, "embedding": vector}}}))
	}))
	defer upstream.Close()
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	secutils.ResetSSRFWhitelistForTest()
	defer secutils.ResetSSRFWhitelistForTest()
	for _, m := range []struct {
		id     string
		tenant uint64
	}{{"synthetic", 99}, {"model-public-two", 1}, {"model-private-home", 1}} {
		require.NoError(t, db.Create(&types.Model{ID: m.id, TenantID: m.tenant, Name: "synthetic-1024", Type: types.ModelTypeEmbedding, Source: types.ModelSourceRemote, Status: types.ModelStatusActive, Parameters: types.ModelParameters{BaseURL: upstream.URL + "/v1", Provider: "openai", EmbeddingParameters: types.EmbeddingParameters{Dimension: 1024}}}).Error)
	}
	require.NoError(t, db.Exec(`UPDATE tenants SET retriever_engines='[{"retriever_type":"keywords","retriever_engine_type":"postgres"},{"retriever_type":"vector","retriever_engine_type":"postgres"}]' WHERE id IN(1,99)`).Error)
	driver := pgdriver.NewPostgresRetrieveEngineRepository(db)
	registry := retriever.NewRetrieveEngineRegistry(nil, nil)
	require.NoError(t, registry.Register(retriever.NewKVHybridRetrieveEngine(driver, types.PostgresRetrieverEngineType)))
	var chunks []*types.Chunk
	require.NoError(t, db.Where("knowledge_id IN ?", []string{"public-doc", "public-two-doc", "private-home-doc"}).Find(&chunks).Error)
	vector := make([]float32, 1024)
	vector[0] = 1
	for _, c := range chunks {
		require.NoError(t, driver.Save(context.Background(), &types.IndexInfo{SourceID: c.ID, SourceType: types.ChunkSourceType, ChunkID: c.ID, KnowledgeID: c.KnowledgeID, KnowledgeBaseID: c.KnowledgeBaseID, Content: c.Content, IsEnabled: true}, map[string]any{"embedding": map[string][]float32{c.ID: vector}}))
	}
	keys := service.NewTenantAPIKeyService(repository.NewTenantAPIKeyRepository(db))
	require.NoError(t, db.Exec("SELECT setval(pg_get_serial_sequence('tenant_api_keys','id'),(SELECT max(id)FROM tenant_api_keys))").Error)
	create := func(user string, tenant uint64, mode string) *interfaces.TenantAPIKeyCreateResult {
		key, e := keys.CreateAPIKey(context.Background(), interfaces.TenantAPIKeyCreateRequest{PersonalScopeMode: mode, ScopeType: types.APIKeyScopeUserMCP, OwnerUserID: user, Name: "synthetic new personal", Capabilities: []string{"retrieve", "enterprise_retrieve"}, TenantScopes: []types.APIKeyTenantScope{{TenantID: tenant, KBScopeMode: types.APIKeyKBScopeAll}}})
		require.NoError(t, e)
		return key
	}
	a, b, old := create("A", 1, "dynamic_all"), create("B", 99, "dynamic_all"), create("A", 1, "fixed")
	on := true
	cfg := &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &on}, PersonalMCP: &config.PersonalMCPConfig{Enabled: true, PolicyReference: "SYNTHETIC_NEXT"}}
	members := documentTestMembers{repo: repository.NewTenantMemberRepository(db)}
	s := service.NewPortalPersonalKnowledge(cfg, db, members, repository.NewTenantAPIKeyRepository(db), repository.NewKnowledgeBaseRepository(db), repository.NewTenantRepository(db), repository.NewKnowledgeRepository(db), repository.NewChunkRepository(db), service.NewModelService(repository.NewModelRepository(db), repository.NewKnowledgeBaseRepository(db), nil, nil, nil, nil), registry, nil, nil, nil)
	g := gin.New()
	g.Use(legacyReadScopeMiddleware(repository.NewKnowledgeRepository(db)))
	RegisterPersonalMCPRoutes(g, mcpserver.NewConfiguredPersonalServer(cfg, s), documentTestTenants{repo: repository.NewTenantRepository(db)}, documentTestUsers{repo: repository.NewUserRepository(db)}, members, keys, cfg)
	rpc := func(key, method string, args any) (int, string) {
		payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": args})
		r := httptest.NewRequest("POST", "/api/v1/mcp/personal", bytes.NewReader(payload))
		r.Header.Set("Authorization", "Bearer "+key)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		w := httptest.NewRecorder()
		g.ServeHTTP(w, r)
		require.NotContains(t, w.Body.String(), fixture.Canary)
		return w.Code, w.Body.String()
	}
	call := func(key, name string, args any) (int, string) {
		return rpc(key, "tools/call", map[string]any{"name": name, "arguments": args})
	}
	status, body := rpc(a.Token, "tools/list", map[string]any{})
	require.Equal(t, 200, status, body)
	for _, name := range []string{"wiki_search", "wiki_read_page", "wiki_index", "search_knowledge", "read_document", "list_documents", "grep_chunks", "list_knowledge_bases"} {
		require.Contains(t, body, `"name":"`+name+`"`)
	}
	// Actual PG/ParadeDB protected BM25, vectors, Hybrid and grep; one bad KB
	// must not abort the other authorized source or enter model inputs.
	require.NoError(t, db.Exec(`INSERT INTO data_sources(id,tenant_id,knowledge_base_id,name,type,config,status,legacy_access_state)VALUES('held',99,'public-kb','synthetic held','feishu','{}','active','hold')`).Error)
	for _, mode := range []string{"keyword", "semantic", "hybrid", "grep"} {
		name := "search_knowledge"
		if mode == "grep" {
			name = "grep_chunks"
		}
		status, body = call(a.Token, name, map[string]any{"query": "PUBLIC", "mode": mode, "limit": 30})
		require.Equal(t, 200, status, body)
		require.Contains(t, body, "public-two-doc")
		require.NotContains(t, body, `knowledge_id\":\"public-doc`)
		require.Contains(t, body, "partial")
	}
	status, body = call(a.Token, "read_document", map[string]any{"knowledge_id": "public-doc"})
	require.Equal(t, 200, status, body)
	require.Contains(t, body, "isError")
	status, body = call(a.Token, "read_document", map[string]any{"knowledge_id": "public-two-doc"})
	require.Equal(t, 200, status, body)
	require.NotContains(t, body, "isError")
	require.Contains(t, body, "public-two-doc")
	// Never clear a failed source by bypassing its guard in a real request.
	// This controlled synthetic state transition tests a separately healthy KB.
	require.NoError(t, db.Exec("INSERT INTO tenants(id,name,business)VALUES(7,'new-private','synthetic')").Error)
	require.NoError(t, db.Exec("INSERT INTO knowledge_bases(id,tenant_id,name,type,embedding_model_id,summary_model_id)VALUES('next-private',7,'new private','document','synthetic','synthetic')").Error)
	require.NoError(t, db.Create(&types.Knowledge{ID: "next-private-doc", TenantID: 7, KnowledgeBaseID: "next-private", Type: "file", Title: "private A only", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled"}).Error)
	require.NoError(t, db.Create(&types.Chunk{ID: "next-private-chunk", TenantID: 7, KnowledgeBaseID: "next-private", KnowledgeID: "next-private-doc", Content: "private A only", ChunkType: "text", IsEnabled: true}).Error)
	for _, key := range []string{a.Token, b.Token, old.Token} {
		_, body = call(key, "read_document", map[string]any{"knowledge_id": "next-private-doc"})
		require.Contains(t, body, "isError")
	}
	require.NoError(t, db.Exec("INSERT INTO tenant_members(user_id,tenant_id,role,status)VALUES('A',7,'viewer','active')").Error)
	_, body = call(a.Token, "read_document", map[string]any{"knowledge_id": "next-private-doc"})
	require.NotContains(t, body, "isError")
	require.Contains(t, body, "private A only")
	for _, key := range []string{b.Token, old.Token} {
		_, body = call(key, "read_document", map[string]any{"knowledge_id": "next-private-doc"})
		require.Contains(t, body, "isError")
	}
	require.NoError(t, db.Exec("UPDATE tenant_members SET status='inactive' WHERE user_id='A' AND tenant_id=7").Error)
	_, body = call(a.Token, "read_document", map[string]any{"knowledge_id": "next-private-doc"})
	require.Contains(t, body, "isError")
	pages := repository.NewWikiPageRepository(db)
	proof, e := repository.BeginWikiMCPGeneration(context.Background(), pages, "public-two")
	require.NoError(t, e)
	page := &types.WikiPage{ID: "next-wiki", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/verified", Title: "verified concept", Content: "verified ordinary wiki", PageType: types.WikiPageTypeConcept, Status: types.WikiPageStatusPublished, SourceRefs: types.StringArray{"public-two-doc|title"}, Version: 1}
	require.NoError(t, pages.Create(proof, page))
	for _, tool := range []string{"wiki_search", "wiki_read_page", "wiki_index"} {
		args := map[string]any{"query": "verified", "slug": "concept/verified", "knowledge_base_id": "public-two"}
		status, body = call(a.Token, tool, args)
		require.Equal(t, 200, status, body)
		require.NotContains(t, body, "isError")
		require.Contains(t, body, "concept/verified")
	}
	var indexes int64
	require.NoError(t, db.Table("wiki_pages").Where("page_type='index'").Count(&indexes).Error)
	require.Zero(t, indexes)
	require.NoError(t, db.Exec("UPDATE chunks SET content='changed without timestamp' WHERE knowledge_id='public-two-doc'").Error)
	_, body = call(a.Token, "wiki_read_page", map[string]any{"slug": "concept/verified", "knowledge_base_id": "public-two"})
	require.Contains(t, body, "isError")
	require.NotContains(t, body, "verified ordinary wiki")
	// A real model-network boundary changes one source during the request.
	// The read-only MCP guard must discard that attempt and retry once with
	// a fresh scope, preserving the unrelated private KB and partial warning.
	require.NoError(t, db.Exec(`INSERT INTO data_sources(id,tenant_id,knowledge_base_id,name,type,config,status,legacy_access_state)VALUES('during-revoke',1,'public-two','synthetic concurrent revoke','feishu','{}','active','active')`).Error)
	_, body = call(a.Token, "search_knowledge", map[string]any{"query": "REVOKE_DURING_QUERY", "mode": "semantic", "limit": 30})
	require.True(t, revokeDuringEmbedding.Load())
	require.NotContains(t, body, "isError")
	require.Contains(t, body, "private-home")
	require.Contains(t, body, "partial")
	require.NotContains(t, body, `knowledge_id\":\"public-two-doc`)
	require.NoError(t, keys.(interfaces.UserMCPAPIKeyService).RevokeUserMCPAPIKey(context.Background(), "A", a.APIKey.ID))
	status, _ = rpc(a.Token, "tools/list", map[string]any{})
	require.Equal(t, 401, status)
}
