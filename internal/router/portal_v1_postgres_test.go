//go:build sqlite_fts5 && knowhub_m6_postgres

package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestPortalV1LinksAndIndexDimensionsPostgres(t *testing.T) {
	dsn := os.Getenv("KNOWHUB_M6_POSTGRES_DSN")
	require.Equal(t, "host=/work/pgsocket user=postgres dbname=knowhub_m6 sslmode=disable", dsn)
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	pool, _ := admin.DB()
	defer pool.Close()
	require.NoError(t, admin.Exec("CREATE DATABASE knowhub_v1_dimensions").Error)
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(filepath.Join(cwd, "../..")))
	err = projectdb.RunMigrationsWithOptions("postgresql://postgres@/knowhub_v1_dimensions?host="+url.QueryEscape("/work/pgsocket")+"&sslmode=disable", projectdb.MigrationOptions{})
	require.NoError(t, os.Chdir(cwd))
	require.NoError(t, err)
	db, err := gorm.Open(postgres.Open(strings.Replace(dsn, "dbname=knowhub_m6", "dbname=knowhub_v1_dimensions", 1)), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	dbpool, _ := db.DB()
	defer dbpool.Close()
	f := fixture.Seed(t, db)
	f.PreparePublished(t)
	dimension := 1024
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		body, _ := json.Marshal(payload)
		require.NotContains(t, string(body), "V1_URL_SECRET")
		require.NotContains(t, string(body), fixture.Canary)
		v := make([]float32, dimension)
		v[0] = 1
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"index": 0, "embedding": v}}}))
	}))
	defer model.Close()
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	secutils.ResetSSRFWhitelistForTest()
	defer secutils.ResetSSRFWhitelistForTest()
	require.NoError(t, db.Create(&types.Model{ID: "synthetic", TenantID: 99, Name: "synthetic-controlled", Type: types.ModelTypeEmbedding, Source: types.ModelSourceRemote, Status: types.ModelStatusActive, Parameters: types.ModelParameters{BaseURL: model.URL + "/v1", Provider: "openai", EmbeddingParameters: types.EmbeddingParameters{Dimension: 1024}}}).Error)
	require.NoError(t, db.Exec(`UPDATE tenants SET retriever_engines='[{"retriever_type":"keywords","retriever_engine_type":"postgres"},{"retriever_type":"vector","retriever_engine_type":"postgres"}]' WHERE id=99`).Error)
	driver := pgdriver.NewPostgresRetrieveEngineRepository(db)
	registry := retriever.NewRetrieveEngineRegistry(nil, nil)
	require.NoError(t, registry.Register(retriever.NewKVHybridRetrieveEngine(driver, types.PostgresRetrieverEngineType)))
	s := service.NewPortalPersonalKnowledge(&config.Config{PersonalMCP: &config.PersonalMCPConfig{Enabled: true, PolicyReference: "SYNTHETIC_V1"}}, db, documentTestMembers{repo: repository.NewTenantMemberRepository(db)}, repository.NewTenantAPIKeyRepository(db), repository.NewKnowledgeBaseRepository(db), repository.NewTenantRepository(db), repository.NewKnowledgeRepository(db), repository.NewChunkRepository(db), service.NewModelService(repository.NewModelRepository(db), repository.NewKnowledgeBaseRepository(db), nil, nil, nil, nil), registry, nil, nil, nil)
	text := "PUBLIC ordinary [guide](https://docs.example.test/guide) https://objects.example.test/file?token=V1_URL_SECRET ![image](/files/V1_URL_SECRET)"
	require.NoError(t, db.Exec("UPDATE chunks SET content=? WHERE knowledge_id='public-doc'", text).Error)
	var chunk types.Chunk
	require.NoError(t, db.Where("knowledge_id='public-doc'").First(&chunk).Error)
	index := func(dim int) {
		require.NoError(t, db.Exec("DELETE FROM embeddings WHERE knowledge_base_id='public-kb'").Error)
		v := make([]float32, dim)
		v[0] = 1
		require.NoError(t, driver.Save(context.Background(), &types.IndexInfo{SourceID: chunk.ID, SourceType: types.ChunkSourceType, ChunkID: chunk.ID, KnowledgeID: chunk.KnowledgeID, KnowledgeBaseID: chunk.KnowledgeBaseID, Content: text, IsEnabled: true}, map[string]any{"embedding": map[string][]float32{chunk.ID: v}}))
	}
	ctx := fixture.Key("A", 1, 1, true)
	index(1024)
	for _, mode := range []string{"keyword", "semantic", "hybrid", "grep"} {
		r, e := s.Search(ctx, []string{"public-kb"}, "PUBLIC", mode, 10)
		require.NoError(t, e)
		require.Equal(t, "complete", r.Status)
		require.NotEmpty(t, r.Results)
		for _, h := range r.Results {
			require.NotContains(t, h.Text, "V1_URL_SECRET")
			require.Contains(t, h.Text, "ordinary guide")
		}
	}
	for _, legacy := range []int{798, 3584} {
		index(legacy)
		r, e := s.Search(ctx, []string{"public-kb"}, "PUBLIC", "semantic", 10)
		require.NoError(t, e)
		require.Equal(t, "failed", r.Status)
		require.Empty(t, r.Results)
		require.Equal(t, 1, r.Failed)
	}
	require.NoError(t, db.Exec("DELETE FROM embeddings WHERE knowledge_base_id='public-kb'").Error)
	r, e := s.Search(ctx, []string{"public-kb"}, "PUBLIC", "semantic", 10)
	require.NoError(t, e)
	require.Equal(t, "failed", r.Status)
	index(1024)
	dimension = 798
	r, e = s.Search(ctx, []string{"public-kb"}, "PUBLIC", "hybrid", 10)
	require.NoError(t, e)
	require.Equal(t, "failed", r.Status)
	require.Empty(t, r.Results)
}
