//go:build sqlite_fts5 && knowhub_v1_deployed

package router

import (
	"context"
	"os"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	pgdriver "github.com/Tencent/WeKnora/internal/application/repository/retriever/postgres"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Explicit synthetic fixture only. This uses the real repository proof lifecycle
// with known test inputs; it does not claim a real LLM-generated Wiki was tested.
func TestV1NextWikiSeedDeployed(t *testing.T) {
	require.Equal(t, "SYNTHETIC_V1_ONLY", os.Getenv("KNOWHUB_V1_SEED"))
	db, err := gorm.Open(postgres.Open("host=postgres user=postgres dbname=knowhub_v1 sslmode=disable"), &gorm.Config{})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	defer pool.Close()
	var pages int64
	require.NoError(t, db.Table("wiki_pages").Count(&pages).Error)
	require.Zero(t, pages, "refuse to overwrite any existing generated Wiki")
	types.EnableLegacyReadGuards(db)
	r := repository.NewWikiPageRepository(db)
	for _, kb := range []string{"public-two", "private-home"} {
		ctx, e := repository.BeginWikiMCPGeneration(context.Background(), r, kb)
		require.NoError(t, e)
		page := &types.WikiPage{ID: "wiki-" + kb, TenantID: 1, KnowledgeBaseID: kb, Slug: "concept/verified", Title: "verified " + kb, Content: "verified " + kb + " Wiki ordinary content", PageType: types.WikiPageTypeConcept, Status: types.WikiPageStatusPublished, SourceRefs: types.StringArray{kb + "-doc|synthetic source"}, Version: 1}
		require.NoError(t, r.Create(ctx, page))
	}
	// No proof is issued for unknown/mixed historical inputs, even when one
	// SourceRef is readable. This page must never appear in MCP output.
	unknown := &types.WikiPage{ID: "wiki-untrusted", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/unknown", Title: "UNTRUSTED_WIKI_CANARY", Content: "UNTRUSTED_WIKI_CANARY", PageType: types.WikiPageTypeConcept, Status: types.WikiPageStatusPublished, SourceRefs: types.StringArray{"public-two-doc|known", "sensitive|unknown"}, Version: 1}
	require.NoError(t, r.Create(context.Background(), unknown))
	var indexes int64
	require.NoError(t, db.Table("wiki_pages").Where("page_type='index'").Count(&indexes).Error)
	require.Zero(t, indexes, "MCP index must read without manufacturing a page")
}

func TestV1NextDynamicPrivateFixtureDeployed(t *testing.T) {
	require.Equal(t, "SYNTHETIC_V1_ONLY", os.Getenv("KNOWHUB_V1_SEED"))
	db, err := gorm.Open(postgres.Open("host=postgres user=postgres dbname=knowhub_v1 sslmode=disable"), &gorm.Config{})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	defer pool.Close()
	require.NoError(t, db.Create(&types.Tenant{ID: 1207, Name: "future private synthetic", Status: "active"}).Error)
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "future-private", TenantID: 1207, Name: "future private synthetic", Type: "document", EmbeddingModelID: "future-private-model", SummaryModelID: "synthetic"}).Error)
	require.NoError(t, db.Create(&types.Knowledge{ID: "future-private-doc", TenantID: 1207, KnowledgeBaseID: "future-private", Type: "file", Title: "FUTURE_PRIVATE_CANARY", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", Channel: types.ChannelWeb}).Error)
	chunk := &types.Chunk{ID: "future-private-chunk", TenantID: 1207, KnowledgeBaseID: "future-private", KnowledgeID: "future-private-doc", Content: "FUTURE_PRIVATE_CANARY", ChunkType: types.ChunkTypeText, IsEnabled: true}
	require.NoError(t, db.Create(chunk).Error)
	require.NoError(t, db.Create(&types.Model{ID: "future-private-model", TenantID: 1207, Name: "SYNTHETIC 1024", Type: types.ModelTypeEmbedding, Source: types.ModelSourceRemote, Status: types.ModelStatusActive, Parameters: types.ModelParameters{BaseURL: "http://model:8000/v1", Provider: "openai", EmbeddingParameters: types.EmbeddingParameters{Dimension: 1024}}}).Error)
	require.NoError(t, db.Exec(`UPDATE tenants SET retriever_engines='[{"retriever_type":"keywords","retriever_engine_type":"postgres"},{"retriever_type":"vector","retriever_engine_type":"postgres"}]' WHERE id=1207`).Error)
	vector := make([]float32, 1024)
	vector[0] = 1
	require.NoError(t, pgdriver.NewPostgresRetrieveEngineRepository(db).Save(context.Background(), &types.IndexInfo{SourceID: chunk.ID, SourceType: types.ChunkSourceType, ChunkID: chunk.ID, KnowledgeID: chunk.KnowledgeID, KnowledgeBaseID: chunk.KnowledgeBaseID, Content: chunk.Content, IsEnabled: true}, map[string]any{"embedding": map[string][]float32{chunk.ID: vector}}))
	// Membership intentionally absent. The HTTP suite adds and removes it only
	// AFTER creating both dynamic and historical fixed credentials.
}
