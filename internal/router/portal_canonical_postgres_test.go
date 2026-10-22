//go:build sqlite_fts5

package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPortalCanonicalIndexWritersPostgres(t *testing.T) {
	dsn := feishulegacy.PrivateDSN(t)
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(filepath.Join(cwd, "../..")))
	err = projectdb.RunMigrationsWithOptions(dsn, projectdb.MigrationOptions{})
	require.NoError(t, os.Chdir(cwd))
	require.NoError(t, err)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	defer pool.Close()
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
	// Match real normal/Feishu writers, including Go Unicode TrimSpace and
	// context enrichment. The candidate must be canonicalized BEFORE consumers.
	title := " \u3000PUBLIC current title\u3000 "
	text = "\u3000 " + text + " \u00a0"
	header := "  Current section\nPUBLIC context "
	require.NoError(t, db.Exec("UPDATE knowledges SET title=? WHERE id='public-doc'", title).Error)
	require.NoError(t, db.Exec("UPDATE chunks SET content=?,context_header=? WHERE id=?", text, header, chunk.ID).Error)
	require.NoError(t, db.First(&chunk, "id=?", chunk.ID).Error)
	companionID := chunk.ID + "-canonical-companion"
	companionText := "PUBLIC ordinary guide canonical companion"
	require.NoError(t, db.Exec(`INSERT INTO chunks(id,tenant_id,knowledge_base_id,knowledge_id,content,chunk_type,content_revision,is_enabled,chunk_index,start_at,end_at)
 VALUES(?,99,'public-kb','public-doc',?,'text',1,true,1,0,100)`, companionID, companionText).Error)
	ctx := fixture.Key("A", 1, 1, true)
	vector := make([]float32, 1024)
	vector[0] = 1
	index := func(source, content string) {
		require.NoError(t, db.Exec("DELETE FROM embeddings WHERE knowledge_base_id='public-kb'").Error)
		require.NoError(t, driver.Save(context.Background(), &types.IndexInfo{SourceID: source, SourceType: types.ChunkSourceType, ChunkID: chunk.ID, KnowledgeID: chunk.KnowledgeID, KnowledgeBaseID: chunk.KnowledgeBaseID, Content: content, IsEnabled: true}, map[string]any{"embedding": map[string][]float32{source: vector}}))
		require.NoError(t, driver.Save(context.Background(), &types.IndexInfo{SourceID: companionID, SourceType: types.ChunkSourceType, ChunkID: companionID, KnowledgeID: chunk.KnowledgeID, KnowledgeBaseID: chunk.KnowledgeBaseID, Content: companionText, IsEnabled: true}, map[string]any{"embedding": map[string][]float32{companionID: vector}}))
		var stored []string
		require.NoError(t, db.Table("embeddings").Where("knowledge_base_id=?", chunk.KnowledgeBaseID).Pluck("source_id", &stored).Error)
		require.ElementsMatch(t, []string{source, companionID}, stored)
	}
	for _, form := range []struct{ name, text string }{
		{"historical-bare", text}, {"embedding-context", chunk.EmbeddingContent()},
		{"normal-writer", strings.TrimSpace(title) + "\n" + chunk.EmbeddingContent()},
		{"feishu-writer", title + "\n\n" + chunk.EmbeddingContent()},
	} {
		t.Run(form.name, func(t *testing.T) {
			index(chunk.ID, form.text)
			for _, mode := range []string{"keyword", "semantic", "hybrid", "grep"} {
				hits, e := s.Search(ctx, []string{"public-kb"}, "PUBLIC", mode, 10)
				require.NoError(t, e)
				require.Equal(t, "complete", hits.Status)
				require.NotEmpty(t, hits.Results)
				foundOriginal := false
				for _, h := range hits.Results {
					if h.ChunkID == chunk.ID {
						foundOriginal = true
					}
					require.Contains(t, []string{chunk.ID, companionID}, h.ChunkID)
					require.Contains(t, h.Text, "ordinary guide")
					require.NotContains(t, h.Text, "V1_URL_SECRET")
					require.NotContains(t, h.Text, header)
				}
				require.True(t, foundOriginal, "the exact writer representation must return its original Chunk, not only the companion")
			}
		})
	}
	// Test the real SQL candidate boundary directly: no output-only filtering
	// or hybrid deduplication may conceal a stale or generated-question row.
	candidates := func(kind types.RetrieverType) []*types.IndexWithScore {
		// Each call represents a NEW request. Resource changes invalidate an
		// old grant; resolve the current snapshot before testing stale indices.
		targets, e := s.Resolve(ctx, []string{"public-kb"})
		require.NoError(t, e)
		require.Len(t, targets, 1)
		protected, e := targets[0].SearchContext(ctx)
		require.NoError(t, e)
		result, e := driver.Retrieve(protected, types.RetrieveParams{Query: "PUBLIC", Embedding: vector, KnowledgeBaseIDs: []string{"public-kb"}, TopK: 10, RetrieverType: kind})
		require.NoError(t, e)
		var rows []*types.IndexWithScore
		for _, r := range result {
			require.NoError(t, r.Error)
			rows = append(rows, r.Results...)
		}
		return rows
	}
	for _, kind := range []types.RetrieverType{types.KeywordsRetrieverType, types.VectorRetrieverType} {
		index(chunk.ID, title+"\n\n"+chunk.EmbeddingContent())
		rows := candidates(kind)
		require.Len(t, rows, 2)
		for _, row := range rows {
			if row.ChunkID == chunk.ID {
				require.Equal(t, text, row.Content)
			} else {
				require.Equal(t, companionID, row.ChunkID)
				require.Equal(t, companionText, row.Content)
			}
		}
		if kind == types.KeywordsRetrieverType {
			require.Greater(t, rows[0].Score, float64(0), "retain actual BM25 scoring")
		}
		index("generated-question-id", title+"\n\n"+chunk.EmbeddingContent())
		rejected := candidates(kind)
		require.Len(t, rejected, 1, "synthetic question identity must be filtered BEFORE LIMIT")
		require.Equal(t, companionID, rejected[0].ChunkID)
		index(chunk.ID, "PUBLIC "+fixture.Canary)
		rejected = candidates(kind)
		require.Len(t, rejected, 1, "stale/forged content must be filtered BEFORE LIMIT/model")
		require.Equal(t, companionID, rejected[0].ChunkID)
		require.NotContains(t, rejected[0].Content, fixture.Canary)
		index(chunk.ID, title+"\n\n"+chunk.EmbeddingContent())
		require.NoError(t, db.Exec("UPDATE knowledges SET title='PUBLIC changed title' WHERE id='public-doc'").Error)
		rejected = candidates(kind)
		require.Len(t, rejected, 1, "title changes invalidate the prior enriched index")
		require.Equal(t, companionID, rejected[0].ChunkID)
		require.NoError(t, db.Exec("UPDATE knowledges SET title=? WHERE id='public-doc'", title).Error)
	}
}
