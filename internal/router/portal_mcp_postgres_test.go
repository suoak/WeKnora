//go:build sqlite_fts5 && knowhub_m6_postgres

package router

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	pgdriver "github.com/Tencent/WeKnora/internal/application/repository/retriever/postgres"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	projectdb "github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/mcpserver"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/embedding"
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

// No environment skip: this command must use the disposable volume Unix socket.
type portalTestEmployee struct{}

func (portalTestEmployee) IsEnterpriseEmployee(_ context.Context, user string) (bool, error) {
	return user == "A", nil
}

type portalHookDriver struct {
	access.DocumentRetriever
	before func()
}

func (d portalHookDriver) Retrieve(ctx context.Context, p types.RetrieveParams) ([]*types.RetrieveResult, error) {
	d.before()
	return d.DocumentRetriever.Retrieve(ctx, p)
}

func TestPortalM6RealPostgresPersonal(t *testing.T) {
	dsn := os.Getenv("KNOWHUB_M6_POSTGRES_DSN")
	require.Equal(t, "host=/work/pgsocket user=postgres dbname=knowhub_m6 sslmode=disable", dsn)
	cwd, err := os.Getwd()
	require.NoError(t, err)
	root := filepath.Clean(filepath.Join(cwd, "../.."))
	require.NoError(t, os.Chdir(root))
	migrationURL := "postgresql://postgres@/knowhub_m6?host=" + url.QueryEscape("/work/pgsocket") + "&sslmode=disable"
	migrationErr := projectdb.RunMigrationsWithOptions(migrationURL, projectdb.MigrationOptions{})
	require.NoError(t, os.Chdir(cwd))
	require.NoError(t, migrationErr)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	defer sqlDB.Close()
	f := fixture.Seed(t, db)
	resolver := f.Portal(t)
	s := &service.PortalMCPKnowledge{Resolver: resolver, Knowledge: repository.NewKnowledgeRepository(db), Chunks: repository.NewChunkRepository(db)}
	driver := pgdriver.NewPostgresRetrieveEngineRepository(db)
	s.Engine = func(context.Context, access.PortalKBDescriptor) (access.DocumentRetriever, error) { return driver, nil }
	// Index all canaries too, plus a deliberately wrong-owner row and stale body.
	var chunks []*types.Chunk
	require.NoError(t, db.Find(&chunks).Error)
	for _, c := range chunks {
		vector := []float32{1, 0, 0}
		if c.KnowledgeBaseID == "public-two" {
			vector = []float32{1, 0, 0, 0}
		}
		require.NoError(t, driver.Save(context.Background(), &types.IndexInfo{SourceID: c.ID, SourceType: types.ChunkSourceType, ChunkID: c.ID, KnowledgeID: c.KnowledgeID, KnowledgeBaseID: c.KnowledgeBaseID, Content: c.Content, IsEnabled: true}, map[string]any{"embedding": map[string][]float32{c.ID: vector}}))
	}
	require.NoError(t, driver.Save(context.Background(), &types.IndexInfo{SourceID: "forged-owner", SourceType: types.ChunkSourceType, ChunkID: "sensitive-chunk", KnowledgeID: "public-doc", KnowledgeBaseID: "public-kb", Content: fixture.Canary, IsEnabled: true}, map[string]any{"embedding": map[string][]float32{"forged-owner": {1, 0, 0}}}))
	require.NoError(t, driver.Save(context.Background(), &types.IndexInfo{SourceID: "stale-copy", SourceType: types.ChunkSourceType, ChunkID: "public-doc-chunk", KnowledgeID: "public-doc", KnowledgeBaseID: "public-kb", Content: fixture.Canary, IsEnabled: true}, map[string]any{"embedding": map[string][]float32{"stale-copy": {1, 0, 0}}}))

	var spyMu sync.Mutex
	wire := map[string][]string{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, e := io.ReadAll(r.Body)
		if e != nil {
			w.WriteHeader(500)
			return
		}
		spyMu.Lock()
		wire[r.URL.Path] = append(wire[r.URL.Path], string(raw))
		spyMu.Unlock()
		if strings.Contains(string(raw), fixture.Canary) {
			t.Errorf("unauthorized canary reached model wire path=%s", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/embeddings"):
			dim := "[1,0,0]"
			if strings.Contains(string(raw), "model-public-two") {
				dim = "[1,0,0,0]"
			}
			_, _ = io.WriteString(w, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":`+dim+`}],"usage":{"prompt_tokens":1,"total_tokens":1}}`)
		case strings.HasSuffix(r.URL.Path, "/rerank"):
			_, _ = io.WriteString(w, `{"results":[{"index":0,"relevance_score":0.9}]}`)
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			_, _ = io.WriteString(w, `{"id":"synthetic","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"synthetic-safe"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	// Allow only the controlled loopback spy using the existing SSRF test hook.
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	secutils.ResetSSRFWhitelistForTest()
	defer secutils.ResetSSRFWhitelistForTest()
	s.Embed = func(ctx context.Context, kb access.PortalKBDescriptor, q string) ([]float32, error) {
		dim := 3
		if kb.ID == "public-two" {
			dim = 4
		}
		model, e := embedding.NewEmbedder(embedding.Config{Source: types.ModelSourceRemote, Provider: "openai", BaseURL: upstream.URL + "/v1", APIKey: "synthetic-no-secret", ModelID: kb.EmbeddingModelID, ModelName: kb.EmbeddingModelID, Dimensions: dim}, nil, nil)
		if e != nil {
			return nil, e
		}
		return model.Embed(ctx, q)
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
			b, e := json.Marshal(hits)
			if e != nil {
				return e
			}
			_, e = llm.Chat(ctx, []chat.Message{{Role: "user", Content: string(b)}}, nil)
			return e
		},
	}
	a := fixture.Key("A", 1, 1, true)
	t.Run("real-keyword-vector-hybrid-grep-multitenant", func(t *testing.T) {
		for _, mode := range []string{"keyword", "semantic", "hybrid", "grep"} {
			result, e := s.Search(a, nil, "PUBLIC", mode, 30)
			require.NoError(t, e)
			require.Equal(t, "complete", result.Status)
			require.Equal(t, 3, result.Searched)
			require.Zero(t, result.Failed)
			require.Len(t, result.Results, 3)
			owners := map[uint64]bool{}
			seen := map[string]bool{}
			for _, h := range result.Results {
				require.NotContains(t, h.Text, fixture.Canary)
				require.NotEmpty(t, h.Space)
				require.NotEmpty(t, h.KBName)
				require.NotEmpty(t, h.Title)
				require.True(t, strings.HasPrefix(h.Reference, "knowhub:tenant:"))
				owners[h.TenantID] = true
				key := h.Reference + ":" + h.ChunkID
				require.False(t, seen[key])
				seen[key] = true
			}
			require.Len(t, owners, 2)
			t.Logf("REAL PostgreSQL mode=%s owners=2 public+private=3 RRF_dedup=PASS", mode)
		}
		spyMu.Lock()
		defer spyMu.Unlock()
		require.NotEmpty(t, wire["/v1/embeddings"])
		require.NotEmpty(t, wire["/v1/rerank"])
		require.NotEmpty(t, wire["/v1/chat/completions"])
		for path, bodies := range wire {
			for _, body := range bodies {
				require.NotContains(t, body, fixture.Canary)
			}
			t.Logf("REAL model wire path=%s calls=%d canary_absent=PASS", path, len(bodies))
		}
	})
	t.Run("driver-precandidate-owner-empty-limits", func(t *testing.T) {
		targets, e := resolver.Resolve(a, []string{"public-kb"})
		require.NoError(t, e)
		ctx, e := targets[0].SearchContext(a)
		require.NoError(t, e)
		for _, kind := range []types.RetrieverType{types.KeywordsRetrieverType, types.VectorRetrieverType} {
			params := types.RetrieveParams{TopK: 100, Query: "SECRET", Embedding: []float32{1, 0, 0}, RetrieverType: kind, Threshold: -1}
			rows, e := driver.Retrieve(ctx, params)
			require.NoError(t, e)
			for _, g := range rows {
				for _, row := range g.Results {
					require.Equal(t, "public-doc", row.KnowledgeID)
					require.NotContains(t, row.Content, fixture.Canary)
				}
			}
			if kind == types.KeywordsRetrieverType {
				require.Empty(t, rows[0].Results, "canonical WHERE excludes forged/stale index candidates before BM25 output")
			}
			params.TopK = 101
			_, e = driver.Retrieve(ctx, params)
			require.Error(t, e)
			params.TopK = 5
			params.KnowledgeBaseIDs = []string{"private-kb"}
			_, e = driver.Retrieve(ctx, params)
			require.Error(t, e)
			params.KnowledgeBaseIDs = nil
			params.KnowledgeIDs = []string{"sensitive"}
			_, e = driver.Retrieve(ctx, params)
			require.Error(t, e)
		}
		require.NoError(t, db.Exec("UPDATE tenant_members SET status='left' WHERE user_id='A' AND tenant_id=1").Error)
		defer db.Exec("UPDATE tenant_members SET status='active' WHERE user_id='A' AND tenant_id=1")
		calls := 0
		before := s.Engine
		defer func() { s.Engine = before }()
		s.Engine = func(context.Context, access.PortalKBDescriptor) (access.DocumentRetriever, error) {
			calls++
			return driver, nil
		}
		result, e := s.Search(a, nil, "PUBLIC", "hybrid", 10)
		require.Error(t, e, "revoked home identity is denied before retrieval")
		require.Nil(t, result)
		require.Zero(t, calls)
		require.NoError(t, db.Exec("UPDATE tenant_portal_configs SET status='draft'").Error)
		result, e = s.Search(a, nil, "PUBLIC", "hybrid", 10)
		require.NoError(t, e)
		require.Empty(t, result.Results)
		require.Zero(t, calls, "empty allow-set never invokes an index")
		require.NoError(t, db.Exec("UPDATE tenant_portal_configs SET status='published'").Error)
		s.Engine = before
		require.NoError(t, db.Exec("UPDATE tenant_members SET status='active' WHERE user_id='A' AND tenant_id=1").Error)
	})
	t.Run("partial-failure-explicit", func(t *testing.T) {
		original := s.Engine
		s.Engine = func(ctx context.Context, kb access.PortalKBDescriptor) (access.DocumentRetriever, error) {
			if kb.ID == "public-two" {
				return nil, errors.New("synthetic backend timeout")
			}
			return original(ctx, kb)
		}
		result, e := s.Search(a, nil, "PUBLIC", "hybrid", 10)
		require.NoError(t, e)
		require.Equal(t, "partial", result.Status)
		require.Equal(t, 1, result.Failed)
		require.Equal(t, 2, result.Searched)
		s.Engine = original
	})
	t.Run("tenant-kb-document-and-source-state", func(t *testing.T) {
		for _, mutation := range []string{"UPDATE tenants SET status='disabled' WHERE id=99", "UPDATE tenants SET deleted_at=CURRENT_TIMESTAMP WHERE id=99", "UPDATE knowledge_bases SET deleted_at=CURRENT_TIMESTAMP WHERE id='public-kb'", "UPDATE knowledge_bases SET is_temporary=true WHERE id='public-kb'", "UPDATE knowledges SET enable_status='disabled' WHERE id='public-doc'", "UPDATE knowledge_access_policies SET source_acl_state='unknown' WHERE knowledge_id='public-doc'"} {
			tx := db.Begin()
			require.NoError(t, tx.Error)
			require.NoError(t, tx.Exec(mutation).Error)
			// Admission reads use the same isolated transaction, with rollback at
			// the fixture boundary; no stale target list is injected.
			store := repository.NewEnterpriseAccessRepository(tx)
			// Disabled/temp targets are directory excluded; all four mutations
			// must also fail the original whole-KB public evaluator.
			list, e := store.ListPortalMCPCandidates(a)
			require.NoError(t, e)
			if strings.HasPrefix(mutation, "UPDATE tenants") || strings.HasPrefix(mutation, "UPDATE knowledge_bases") {
				for _, kb := range list {
					require.NotEqual(t, "public-kb", kb.ID)
				}
			}
			public := access.NewEnterprisePublicReader(access.EnterprisePublicOptions{Enabled: true}, store, portalTestEmployee{}, nil)
			_, e = public.Authorize(a, "public-kb")
			require.Error(t, e, mutation)
			require.NoError(t, tx.Rollback().Error)
		}
	})
	t.Run("revocation-before-next-model-boundary", func(t *testing.T) {
		before := s.Consumers
		spyMu.Lock()
		llmBefore := len(wire["/v1/chat/completions"])
		spyMu.Unlock()
		s.Consumers = []func(context.Context, []service.PortalMCPHit) error{func(ctx context.Context, hits []service.PortalMCPHit) error {
			return db.Exec("UPDATE tenant_portal_configs SET status='draft' WHERE tenant_id=99").Error
		}, before[1]}
		_, e := s.Search(a, nil, "PUBLIC", "hybrid", 10)
		require.Error(t, e)
		spyMu.Lock()
		require.Equal(t, llmBefore, len(wire["/v1/chat/completions"]), "revocation blocks next network consumer")
		spyMu.Unlock()
		s.Consumers = before
		require.NoError(t, db.Exec("UPDATE tenant_portal_configs SET status='published' WHERE tenant_id=99").Error)
	})
	t.Run("concurrent-principals-and-query-time-revocation", func(t *testing.T) {
		var wg sync.WaitGroup
		failures := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				ctx := a
				if i%2 == 1 {
					ctx = fixture.Key("C", 99, 2, false)
				}
				result, e := s.Search(ctx, nil, "PUBLIC", "hybrid", 30)
				if e != nil {
					failures <- e
					return
				}
				want := 3
				if i%2 == 1 {
					want = 1
				}
				if len(result.Results) != want {
					failures <- fmt.Errorf("principal %d got %d results want %d", i, len(result.Results), want)
					return
				}
				for _, h := range result.Results {
					if strings.Contains(h.Text, fixture.Canary) || i%2 == 1 && h.TenantID != 99 {
						failures <- fmt.Errorf("principal isolation failure %d", i)
						return
					}
				}
			}(i)
		}
		wg.Wait()
		close(failures)
		for e := range failures {
			require.NoError(t, e)
		}
		original := s.Engine
		entered := make(chan struct{})
		resume := make(chan struct{})
		done := make(chan error, 1)
		var once sync.Once
		s.Engine = func(ctx context.Context, kb access.PortalKBDescriptor) (access.DocumentRetriever, error) {
			if kb.ID != "public-kb" {
				return original(ctx, kb)
			}
			return portalHookDriver{DocumentRetriever: driver, before: func() { once.Do(func() { close(entered); <-resume }) }}, nil
		}
		defer func() {
			s.Engine = original
			db.Exec("UPDATE tenant_portal_configs SET status='published' WHERE tenant_id=99")
		}()
		spyMu.Lock()
		before := len(wire["/v1/chat/completions"])
		spyMu.Unlock()
		go func() { _, e := s.Search(a, nil, "PUBLIC", "keyword", 30); done <- e }()
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			t.Fatal("search did not reach blocked real driver")
		}
		require.NoError(t, db.Exec("UPDATE tenant_portal_configs SET status='draft' WHERE tenant_id=99").Error)
		close(resume)
		select {
		case e := <-done:
			require.Error(t, e)
		case <-time.After(10 * time.Second):
			t.Fatal("revoked search did not terminate")
		}
		spyMu.Lock()
		require.Equal(t, before, len(wire["/v1/chat/completions"]), "query-time withdrawal blocks model consumer")
		spyMu.Unlock()
	})
	t.Run("real-personal-go-mcp-and-credentials", func(t *testing.T) {
		tokens := map[uint64]string{}
		require.NoError(t, db.Exec("INSERT INTO tenant_api_keys(id,tenant_id,scope_type,name,key_hash,api_key,capabilities,full_access) VALUES(9,99,'tenant','synthetic-machine','synthetic-machine-hash','synthetic-no-real-key','[]',true)").Error)
		for _, id := range []uint64{1, 2, 3, 4, 8, 9} {
			var raw [32]byte
			_, e := rand.Read(raw[:])
			require.NoError(t, e)
			token := "mcp_" + hex.EncodeToString(raw[:])
			hash := sha256.Sum256([]byte(token))
			require.NoError(t, db.Exec("UPDATE tenant_api_keys SET key_hash=? WHERE id=?", hex.EncodeToString(hash[:]), id).Error)
			tokens[id] = token
		}
		on := true
		cfg := &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &on}}
		engine := gin.New()
		RegisterPersonalMCPRoutes(engine, mcpserver.NewPersonalServer(true, s), documentTestTenants{repo: repository.NewTenantRepository(db)}, documentTestUsers{repo: repository.NewUserRepository(db)}, documentTestMembers{repo: repository.NewTenantMemberRepository(db)}, service.NewTenantAPIKeyService(repository.NewTenantAPIKeyRepository(db)), cfg)
		endpoint := httptest.NewServer(engine)
		defer endpoint.Close()
		client := &http.Client{Timeout: 15 * time.Second}
		rpc := func(key uint64, method string, params any) (int, string) {
			data, e := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 17, "method": method, "params": params})
			require.NoError(t, e)
			req, e := http.NewRequest("POST", endpoint.URL+"/api/v1/mcp/personal", bytes.NewReader(data))
			require.NoError(t, e)
			req.Header.Set("Authorization", "Bearer "+tokens[key])
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			req.Header.Set("X-User-ID", "D")
			req.Header.Set("X-From-MCP", "false")
			response, e := client.Do(req)
			require.NoError(t, e)
			defer response.Body.Close()
			body, e := io.ReadAll(response.Body)
			require.NoError(t, e)
			require.NotContains(t, string(body), fixture.Canary)
			return response.StatusCode, string(body)
		}
		status, body := rpc(1, "initialize", map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "synthetic", "version": "1"}})
		require.Equal(t, 200, status, body)
		status, body = rpc(1, "tools/list", map[string]any{})
		require.Equal(t, 200, status, body)
		for _, tool := range []string{"list_knowledge_bases", "search_knowledge", "grep_chunks", "list_documents", "read_document", "wiki_search", "wiki_read_page", "wiki_index"} {
			require.Contains(t, body, tool)
		}
		for _, tool := range []string{"ask", "delete_document", "add_document"} {
			require.NotContains(t, body, `"name":"`+tool+`"`)
		}
		for _, call := range []struct {
			name string
			args map[string]any
		}{
			{"list_knowledge_bases", map[string]any{}}, {"search_knowledge", map[string]any{"query": "PUBLIC"}}, {"grep_chunks", map[string]any{"query": "PUBLIC"}}, {"list_documents", map[string]any{"knowledge_base_id": "public-kb"}}, {"read_document", map[string]any{"knowledge_id": "public-doc"}},
		} {
			status, body = rpc(1, "tools/call", map[string]any{"name": call.name, "arguments": call.args})
			require.Equal(t, 200, status, body)
			require.NotContains(t, body, `"isError":true`)
			t.Logf("real personal Go MCP tools/call %s PASS", call.name)
		}
		for _, call := range []struct {
			name string
			args map[string]any
		}{{"read_document", map[string]any{"knowledge_id": "sensitive"}}, {"search_knowledge", map[string]any{"query": "PUBLIC", "knowledge_base_ids": []string{"private-kb"}}}, {"ask", map[string]any{}}, {"delete_document", map[string]any{"knowledge_id": "public-doc"}}, {"wiki_read_page", map[string]any{"knowledge_base_id": "public-kb"}}} {
			status, body = rpc(1, "tools/call", map[string]any{"name": call.name, "arguments": call.args})
			require.Equal(t, 200, status, body)
			require.True(t, strings.Contains(body, `"isError":true`) || strings.Contains(body, `"error"`), body)
		}
		status, body = rpc(3, "tools/call", map[string]any{"name": "read_document", "arguments": map[string]any{"knowledge_id": "public-doc"}})
		require.Equal(t, 200, status, body)
		require.Contains(t, body, `"isError":true`)
		status, body = rpc(2, "tools/call", map[string]any{"name": "read_document", "arguments": map[string]any{"knowledge_id": "public-doc"}})
		require.Equal(t, 200, status, body)
		require.NotContains(t, body, `"isError":true`)
		status, body = rpc(2, "tools/call", map[string]any{"name": "read_document", "arguments": map[string]any{"knowledge_id": "sensitive"}})
		require.Equal(t, 200, status, body)
		require.Contains(t, body, `"isError":true`)
		status, _ = rpc(9, "tools/list", map[string]any{})
		require.Equal(t, 401, status, "tenant full_access machine cannot become an employee")
		require.NoError(t, db.Exec("UPDATE tenant_api_keys SET revoked_at=CURRENT_TIMESTAMP WHERE id=1").Error)
		status, _ = rpc(1, "tools/call", map[string]any{"name": "read_document", "arguments": map[string]any{"knowledge_id": "public-doc"}})
		require.Equal(t, 401, status)
		status, _ = rpc(8, "tools/list", map[string]any{})
		require.Equal(t, 401, status)
		require.NoError(t, db.Exec("UPDATE tenant_api_keys SET revoked_at=NULL WHERE id=1").Error)
		var memberships int64
		require.NoError(t, db.Table("tenant_members").Where("user_id='A' AND tenant_id=99").Count(&memberships).Error)
		require.Zero(t, memberships)
	})
	t.Run("portal-draft-archive-and-private-survival", func(t *testing.T) {
		require.NoError(t, db.Exec("UPDATE tenant_portal_configs SET status='archived' WHERE tenant_id=99").Error)
		_, e := s.ReadDocument(a, "public-doc")
		require.Error(t, e, "a previously issued controlled reference does not preserve public read access")
		private, e := s.ReadDocument(a, "private-home-doc")
		require.NoError(t, e)
		require.NotEmpty(t, private, "independent private access survives public withdrawal")
		result, e := s.Search(a, nil, "PUBLIC", "hybrid", 30)
		require.NoError(t, e)
		require.Len(t, result.Results, 2)
		for _, h := range result.Results {
			require.Equal(t, uint64(1), h.TenantID)
		}
		require.NoError(t, db.Exec("UPDATE tenant_members SET status='left' WHERE user_id='A' AND tenant_id=1").Error)
		result, e = s.Search(a, nil, "PUBLIC", "hybrid", 30)
		require.Error(t, e, "published public target cannot outlive authenticated membership")
		require.Nil(t, result)
		require.NoError(t, db.Exec("UPDATE tenant_members SET status='active' WHERE user_id='A' AND tenant_id=1").Error)
	})
	var extensions []struct {
		Extname    string
		Extversion string
	}
	require.NoError(t, db.Raw("SELECT extname,extversion FROM pg_extension WHERE extname IN ('vector','pg_search')").Scan(&extensions).Error)
	t.Logf("real isolated extensions=%+v; formal migration 3036; no production writes", extensions)
}
