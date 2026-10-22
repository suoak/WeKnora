//go:build sqlite_fts5 && knowhub_m6_postgres

package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	pgdriver "github.com/Tencent/WeKnora/internal/application/repository/retriever/postgres"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/config"
	projectdb "github.com/Tencent/WeKnora/internal/database"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type reviewAuthorityM7 struct{}

type portalSQLTraceM7 struct {
	logger.Interface
	mu   sync.Mutex
	last string
}

func (l *portalSQLTraceM7) Trace(_ context.Context, _ time.Time, fc func() (string, int64), err error) {
	if err != nil {
		return
	}
	query, _ := fc()
	if strings.Contains(query, "WITH lexical AS MATERIALIZED") || strings.Contains(query, "WITH authorized AS MATERIALIZED") || strings.Contains(query, "paradedb.score(id) as score") {
		l.mu.Lock()
		l.last = query
		l.mu.Unlock()
	}
}

func (reviewAuthorityM7) AuthorizeEnterprisePolicyChange(_ context.Context, actor string, _ types.EnterprisePolicyTarget, action string) error {
	if actor == "owner" && action == "propose" || actor == "reviewer" && (action == "approve" || action == "revoke") {
		return nil
	}
	return access.ErrForbidden
}

func TestPortalM7RealPostgresScale(t *testing.T) {
	require.Equal(t, "host=/work/pgsocket user=postgres dbname=knowhub_m6 sslmode=disable", os.Getenv("KNOWHUB_M6_POSTGRES_DSN"))
	admin, err := gorm.Open(postgres.Open(os.Getenv("KNOWHUB_M6_POSTGRES_DSN")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, admin.Exec("CREATE DATABASE knowhub_m7").Error)
	defer func() { sql, _ := admin.DB(); sql.Close() }()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(filepath.Clean(filepath.Join(cwd, "../.."))))
	err = projectdb.RunMigrationsWithOptions("postgresql://postgres@/knowhub_m7?host="+url.QueryEscape("/work/pgsocket")+"&sslmode=disable", projectdb.MigrationOptions{})
	require.NoError(t, os.Chdir(cwd))
	require.NoError(t, err)
	db, err := gorm.Open(postgres.Open("host=/work/pgsocket user=postgres dbname=knowhub_m7 sslmode=disable"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	defer func() { sql, _ := db.DB(); sql.Close() }()
	f := fixture.Seed(t, db)
	f.PreparePublished(t)
	a := fixture.Key("A", 1, 1, true)
	trace := &portalSQLTraceM7{Interface: logger.Default.LogMode(logger.Silent)}
	driver := pgdriver.NewPostgresRetrieveEngineRepository(db.Session(&gorm.Session{Logger: trace}))
	s := &service.PortalMCPKnowledge{Knowledge: repository.NewKnowledgeRepository(db), Chunks: repository.NewChunkRepository(db), Engine: func(context.Context, access.PortalKBDescriptor) (access.DocumentRetriever, error) { return driver, nil }}
	// The same high dimension is used for every indexed row and query. This is
	// a scale/SQL test, not a claim of semantic quality of a production model.
	const dimension = 1024
	vector := make([]float32, dimension)
	vector[0] = 1
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		require.NotContains(t, fmt.Sprint(payload), fixture.Canary)
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []any{map[string]any{"object": "embedding", "index": 0, "embedding": vector}}}))
	}))
	defer upstream.Close()
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	secutils.ResetSSRFWhitelistForTest()
	defer secutils.ResetSSRFWhitelistForTest()
	var kb types.KnowledgeBase
	require.NoError(t, db.Where("id='public-kb'").Take(&kb).Error)
	require.NoError(t, db.Create(&types.Model{ID: kb.EmbeddingModelID, TenantID: 99, Name: "synthetic-1024", Type: types.ModelTypeEmbedding, Source: types.ModelSourceRemote, Status: types.ModelStatusActive, Parameters: types.ModelParameters{BaseURL: upstream.URL + "/v1", Provider: "openai", EmbeddingParameters: types.EmbeddingParameters{Dimension: dimension}}}).Error)
	for _, id := range []string{"public-two", "private-home"} {
		require.NoError(t, db.Create(&types.Model{ID: "model-" + id, TenantID: 1, Name: "synthetic-1024-" + id, Type: types.ModelTypeEmbedding, Source: types.ModelSourceRemote, Status: types.ModelStatusActive, Parameters: types.ModelParameters{BaseURL: upstream.URL + "/v1", Provider: "openai", EmbeddingParameters: types.EmbeddingParameters{Dimension: dimension}}}).Error)
		var c types.Chunk
		require.NoError(t, db.Where("knowledge_base_id=?", id).First(&c).Error)
		require.NoError(t, driver.Save(context.Background(), &types.IndexInfo{SourceID: c.ID, SourceType: types.ChunkSourceType, ChunkID: c.ID, KnowledgeID: c.KnowledgeID, KnowledgeBaseID: c.KnowledgeBaseID, Content: c.Content, IsEnabled: true}, map[string]any{"embedding": map[string][]float32{c.ID: vector}}))
	}
	models := service.NewModelService(repository.NewModelRepository(db), repository.NewKnowledgeBaseRepository(db), nil, nil, nil, nil)
	registry := retriever.NewRetrieveEngineRegistry(nil, nil)
	require.NoError(t, registry.Register(retriever.NewKVHybridRetrieveEngine(driver, types.PostgresRetrieverEngineType)))
	require.NoError(t, db.Exec(`UPDATE tenants SET retriever_engines='[{"retriever_type":"keywords","retriever_engine_type":"postgres"},{"retriever_type":"vector","retriever_engine_type":"postgres"}]' WHERE id IN (1,99)`).Error)
	s = service.NewPortalPersonalKnowledge(&config.Config{PersonalMCP: &config.PersonalMCPConfig{Enabled: true, PolicyReference: "ISOLATED_PUBLISHED_DYNAMIC_SCALE"}}, db, documentTestMembers{repo: repository.NewTenantMemberRepository(db)}, repository.NewTenantAPIKeyRepository(db), repository.NewKnowledgeBaseRepository(db), repository.NewTenantRepository(db), repository.NewKnowledgeRepository(db), repository.NewChunkRepository(db), models, registry, nil, nil, nil)
	require.NotNil(t, s)
	resolver := s.Resolver

	for _, count := range []int{128, 257, 512, 864, 1001} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			require.NoError(t, db.Exec("DELETE FROM document_subject_grants WHERE knowledge_id IN (SELECT id FROM knowledges WHERE knowledge_base_id='public-kb')").Error)
			require.NoError(t, db.Exec("DELETE FROM knowledge_access_policies WHERE knowledge_id IN (SELECT id FROM knowledges WHERE knowledge_base_id='public-kb')").Error)
			require.NoError(t, db.Exec("DELETE FROM chunks WHERE knowledge_base_id='public-kb'").Error)
			require.NoError(t, db.Exec("DELETE FROM knowledges WHERE knowledge_base_id='public-kb'").Error)
			require.NoError(t, db.Exec("DELETE FROM embeddings WHERE knowledge_base_id='public-kb'").Error)
			require.NoError(t, db.Exec("DELETE FROM kb_publication_policies WHERE knowledge_base_id='public-kb'").Error)
			for i := 0; i < count; i++ {
				id := fmt.Sprintf("scale-%d-%04d", count, i)
				text := fmt.Sprintf("PUBLIC scale unique%04d", i)
				require.NoError(t, db.Exec("INSERT INTO knowledges(id,tenant_id,knowledge_base_id,type,title,source,file_name,parse_status,enable_status,channel) VALUES(?,99,'public-kb','file',?,'synthetic-local',?,'completed','enabled','web')", id, text, id+".txt").Error)
				require.NoError(t, db.Exec("INSERT INTO chunks(id,tenant_id,knowledge_base_id,knowledge_id,content,chunk_type,content_revision,is_enabled,chunk_index,start_at,end_at) VALUES(?,99,'public-kb',?,?,'text',1,true,0,0,100)", id+"-chunk", id, text).Error)
				stored := make([]float32, dimension)
				stored[0] = 0.5
				stored[1] = 1 + float32(i)/float32(count)
				if i == count-1 {
					stored[0] = 1
					stored[1] = 0
				}
				require.NoError(t, driver.Save(context.Background(), &types.IndexInfo{SourceID: id + "-chunk", SourceType: types.ChunkSourceType, ChunkID: id + "-chunk", KnowledgeID: id, KnowledgeBaseID: "public-kb", Content: text, IsEnabled: true}, map[string]any{"embedding": map[string][]float32{id + "-chunk": stored}}))
			}
			var approvals int64
			require.NoError(t, db.Table("kb_publication_policies").Where("state='approved'").Count(&approvals).Error)
			require.Zero(t, approvals, "Published discovery must not require or create approvals")
			start := time.Now()
			targets, e := resolver.Resolve(a, []string{"public-kb"})
			require.NoError(t, e)
			require.Len(t, targets, 1)
			resolveMS := float64(time.Since(start).Microseconds()) / 1000
			require.Len(t, targets[0].DocumentIDs(), count, "whole KB is not silently truncated")
			for _, mode := range []string{"keyword", "semantic", "hybrid", "grep"} {
				times := []float64{}
				for sample := 0; sample < 6; sample++ {
					start := time.Now()
					result, e := s.Search(a, []string{"public-kb"}, fmt.Sprintf("unique%04d", count-1), mode, 10)
					require.NoError(t, e)
					require.Equal(t, "complete", result.Status)
					require.NotEmpty(t, result.Results)
					if mode == "keyword" || mode == "grep" || mode == "semantic" || mode == "hybrid" {
						require.Equal(t, fmt.Sprintf("scale-%d-%04d", count, count-1), result.Results[0].DocumentID, "last document must be searchable; no first-256 truncation")
					}
					for _, hit := range result.Results {
						require.NotContains(t, hit.Text, fixture.Canary)
						require.Equal(t, uint64(99), hit.TenantID)
					}
					times = append(times, float64(time.Since(start).Microseconds())/1000)
				}
				sort.Float64s(times)
				t.Logf("M7_METRIC documents=%d dimension=%d mode=%s samples=%d p50_ms=%.3f p95_ms=%.3f error_rate=0 resolve_ms=%.3f", count, dimension, mode, len(times), times[len(times)/2], times[len(times)-1], resolveMS)
				trace.mu.Lock()
				query := trace.last
				trace.mu.Unlock()
				require.NotEmpty(t, query)
				var plan string
				require.NoError(t, db.Raw("EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query).Row().Scan(&plan))
				var parsed any
				require.NoError(t, json.Unmarshal([]byte(plan), &parsed))
				directory := os.Getenv("KNOWHUB_M7_PLAN_DIR")
				require.True(t, strings.HasPrefix(directory, "/work/results/") && !strings.Contains(directory, ".."))
				require.NoError(t, os.MkdirAll(directory, 0755))
				require.NoError(t, os.WriteFile(filepath.Join(directory, fmt.Sprintf("%d-%s.json", count, mode)), []byte(plan), 0600))
			}
			if count == 1001 {
				var wg sync.WaitGroup
				errs := make(chan error, 4)
				for i := 0; i < 4; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						result, e := s.Search(a, []string{"public-kb"}, "unique1000", "hybrid", 10)
						if e == nil && (result == nil || result.Status != "complete") {
							e = fmt.Errorf("incomplete concurrent result")
						}
						errs <- e
					}()
				}
				wg.Wait()
				close(errs)
				for e := range errs {
					require.NoError(t, e)
				}
			}
			multiTimes := []float64{}
			for sample := 0; sample < 6; sample++ {
				start := time.Now()
				multi, e := s.Search(a, nil, "PUBLIC", "hybrid", 30)
				require.NoError(t, e)
				require.Equal(t, "complete", multi.Status)
				require.Equal(t, 3, multi.Searched)
				found := map[string]bool{}
				for _, hit := range multi.Results {
					found[hit.KBID] = true
					require.NotContains(t, hit.Text, fixture.Canary)
				}
				require.True(t, found["public-kb"] && found["public-two"] && found["private-home"])
				multiTimes = append(multiTimes, float64(time.Since(start).Microseconds())/1000)
			}
			sort.Float64s(multiTimes)
			t.Logf("M7_MULTI_METRIC documents=%d dimension=%d kbs=3 tenants=2 samples=6 p50_ms=%.3f p95_ms=%.3f error_rate=0", count, dimension, multiTimes[len(multiTimes)/2], multiTimes[len(multiTimes)-1])
			execution, e := targets[0].SearchContext(a)
			require.NoError(t, e)
			_, e = access.GuardPortalRetrieval(execution, types.PostgresRetrieverEngineType, types.RetrieveParams{TopK: 10, KnowledgeIDs: []string{"sensitive"}})
			require.Error(t, e)
			cancel, cancelFn := context.WithCancel(a)
			cancelFn()
			_, e = s.Search(cancel, []string{"public-kb"}, "PUBLIC", "hybrid", 10)
			require.Error(t, e)
			require.NoError(t, f.Store.SetPortalKBPublicBlock(context.Background(), 99, "public-kb", "owner", "synthetic sensitive whole-KB exception", 0, true))
			_, e = s.Search(a, []string{"public-kb"}, "PUBLIC", "hybrid", 10)
			require.Error(t, e, "one explicit deny HOLDS whole public KB")
		})
	}
}
