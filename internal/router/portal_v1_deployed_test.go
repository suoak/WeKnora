//go:build sqlite_fts5 && knowhub_v1_deployed

package router

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	pgdriver "github.com/Tencent/WeKnora/internal/application/repository/retriever/postgres"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Seeds only the new, explicitly named V1 synthetic database after the full APP
// has performed its normal migrations. Never accepts a caller-provided DSN.
func TestPortalV1SeedDeployed(t *testing.T) {
	require.Equal(t, "SYNTHETIC_V1_ONLY", os.Getenv("KNOWHUB_V1_SEED"))
	db, err := gorm.Open(postgres.Open("host=postgres user=postgres dbname=knowhub_v1 sslmode=disable"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	defer pool.Close()
	var users int64
	require.NoError(t, db.Table("users").Count(&users).Error)
	require.Zero(t, users, "refuse to seed an existing business database")
	f := fixture.Seed(t, db)
	f.PreparePublished(t)
	require.NoError(t, db.Exec("INSERT INTO tenants(id,name,business) VALUES(7,'V1 second private','synthetic')").Error)
	require.NoError(t, db.Exec("INSERT INTO tenant_members(user_id,tenant_id,role,status) VALUES('A',7,'viewer','active')").Error)
	require.NoError(t, db.Exec("INSERT INTO knowledge_bases(id,name,tenant_id,embedding_model_id,summary_model_id,type) VALUES('private-other','V1 second private',7,'model-private-other','synthetic','document')").Error)
	require.NoError(t, db.Exec("INSERT INTO knowledges(id,tenant_id,knowledge_base_id,type,title,source,file_name,parse_status,enable_status,channel) VALUES('private-other-doc',7,'private-other','file','PUBLIC private second','synthetic-local','private.txt','completed','enabled','web')").Error)
	require.NoError(t, db.Exec("INSERT INTO chunks(id,tenant_id,knowledge_base_id,knowledge_id,content,chunk_type,content_revision,is_enabled,chunk_index,start_at,end_at) VALUES('private-other-chunk',7,'private-other','private-other-doc','PUBLIC private second text','text',1,true,0,0,50)").Error)
	password := os.Getenv("KNOWHUB_V1_TEST_PASSWORD")
	require.GreaterOrEqual(t, len(password), 20)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	require.NoError(t, err)
	require.NoError(t, db.Exec("UPDATE users SET password_hash=?", string(hash)).Error)
	require.NoError(t, db.Exec("UPDATE users SET is_system_admin=true WHERE id='reviewer'").Error)
	require.NoError(t, db.Exec("INSERT INTO tenant_members(user_id,tenant_id,role,status) VALUES('owner',99,'owner','active') ON CONFLICT DO NOTHING").Error)
	require.NoError(t, db.Exec("UPDATE chunks SET content=? WHERE knowledge_id='public-doc'", "PUBLIC ordinary Office policy [manual](https://docs.example.test/guide) https://storage.example.test/private?token=V1_URL_SECRET ![private](/files/V1_URL_SECRET)").Error)
	for _, m := range []struct {
		id     string
		tenant uint64
	}{{"synthetic", 99}, {"model-public-two", 1}, {"model-private-home", 1}, {"model-private-other", 7}} {
		require.NoError(t, db.Create(&types.Model{ID: m.id, TenantID: m.tenant, Name: "SYNTHETIC controlled 1024", Type: types.ModelTypeEmbedding, Source: types.ModelSourceRemote, Status: types.ModelStatusActive, Parameters: types.ModelParameters{BaseURL: "http://model:8000/v1", Provider: "openai", EmbeddingParameters: types.EmbeddingParameters{Dimension: 1024}}}).Error)
	}
	require.NoError(t, db.Exec(`UPDATE tenants SET retriever_engines='[{"retriever_type":"keywords","retriever_engine_type":"postgres"},{"retriever_type":"vector","retriever_engine_type":"postgres"}]' WHERE id IN (1,7,99)`).Error)
	driver := pgdriver.NewPostgresRetrieveEngineRepository(db)
	vector := make([]float32, 1024)
	vector[0] = 1
	var chunks []types.Chunk
	require.NoError(t, db.Find(&chunks).Error)
	for _, c := range chunks {
		require.NoError(t, driver.Save(context.Background(), &types.IndexInfo{SourceID: c.ID, SourceType: types.ChunkSourceType, ChunkID: c.ID, KnowledgeID: c.KnowledgeID, KnowledgeBaseID: c.KnowledgeBaseID, Content: c.Content, IsEnabled: true}, map[string]any{"embedding": map[string][]float32{c.ID: vector}}))
	}
	// Preserve legacy Key rows, and align sequences before exercising real APIs.
	require.NoError(t, db.Exec("SELECT setval(pg_get_serial_sequence('tenant_api_keys','id'),(SELECT max(id) FROM tenant_api_keys))").Error)
	dir := os.Getenv("KNOWHUB_V1_ARTIFACTS")
	require.Equal(t, "/artifacts", dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "seed-complete"), []byte("SYNTHETIC ONLY; no SID or real model validation"), 0600))
}
