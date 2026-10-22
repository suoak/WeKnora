//go:build sqlite_fts5 && knowhub_m6_postgres

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	pgdriver "github.com/Tencent/WeKnora/internal/application/repository/retriever/postgres"
	fileService "github.com/Tencent/WeKnora/internal/application/service/file"
	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/config"
	projectdb "github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Real existing Feishu personal import/recovery and durable dispatch, with a
// synthetic Feishu HTTP peer. Parser output and embedding vectors are controlled
// boundaries; this does not certify a real Feishu cloud or legacy sync worker.
func TestPortalM7PublishedFeishuIngressPostgres(t *testing.T) {
	dsn := os.Getenv("KNOWHUB_M6_POSTGRES_DSN")
	require.Equal(t, "host=/work/pgsocket user=postgres dbname=knowhub_m6 sslmode=disable", dsn)
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, admin.Exec("CREATE DATABASE knowhub_m7_feishu").Error)
	p, _ := admin.DB()
	defer p.Close()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(filepath.Clean(filepath.Join(cwd, "../../.."))))
	err = projectdb.RunMigrationsWithOptions("postgresql://postgres@/knowhub_m7_feishu?host="+url.QueryEscape("/work/pgsocket")+"&sslmode=disable", projectdb.MigrationOptions{})
	require.NoError(t, os.Chdir(cwd))
	require.NoError(t, err)
	db, err := gorm.Open(postgres.Open(strings.Replace(dsn, "dbname=knowhub_m6", "dbname=knowhub_m7_feishu", 1)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	p, _ = db.DB()
	defer p.Close()
	f := fixture.Seed(t, db)
	f.PreparePublished(t)
	require.NoError(t, db.Exec("INSERT INTO tenant_members(user_id,tenant_id,role,status) VALUES('owner',99,'owner','active')").Error)
	t.Setenv("SYSTEM_AES_KEY", "01234567890123456789012345678901")
	t.Setenv("FEISHU_PERSONAL_MVP_FIRST_CREATE", "1")
	t.Setenv("SSRF_WHITELIST", "127.0.0.1,localhost,open.feishu.cn")
	utils.ResetSSRFWhitelistForTest()
	defer utils.ResetSSRFWhitelistForTest()
	var kb types.KnowledgeBase
	require.NoError(t, db.Where("id='public-kb'").Take(&kb).Error)
	ks := &knowledgeService{repo: repository.NewKnowledgeRepository(db), task: &dispatchQueue{}, kbService: dispatchActualKB{kb: &kb}, fileSvc: fileService.NewLocalFileService(t.TempDir(), "")}
	source := NewFeishuPersonalService(db, repository.NewSystemSettingRepository(db), ks, dispatchActualKB{kb: &kb}, nil)
	require.NoError(t, source.SaveApp(context.WithValue(personalUser("owner"), types.SystemAdminContextKey, true), &FeishuAppConfig{AppID: "cli_test", Secret: "synthetic-only", RedirectURI: "https://test.invalid/feishu/callback", Enabled: true}))
	cipher, err := sealFeishu(feishuTokens{UserID: "owner", Access: "access-owner", Refresh: "refresh-owner"})
	require.NoError(t, err)
	require.NoError(t, db.Create(&types.FeishuConnection{UserID: "owner", AppID: "cli_test", OpenID: "open-owner", Tokens: cipher, ExpiresAt: time.Now().Add(time.Hour), RefreshExpiresAt: time.Now().Add(2 * time.Hour)}).Error)
	require.NoError(t, db.Create(&types.FeishuPersonalGrantEpoch{UserID: "owner", Epoch: uuid.NewString()}).Error)
	require.NoError(t, EnablePersonalRecovery(source, ks, nil, t.TempDir()))
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer access-owner", r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/open-apis/wiki/v2/spaces/get_node":
			_, _ = io.WriteString(w, `{"code":0,"data":{"node":{"obj_type":"file","obj_token":"real"}}}`)
		case "/open-apis/drive/v1/metas/batch_query":
			_, _ = io.WriteString(w, `{"code":0,"data":{"metas":[{"doc_type":"file","doc_token":"real","title":"SYNTHETICFEISHUPUBLIC.pdf","latest_modify_time":"1"}]}}`)
		case "/open-apis/drive/v1/files/real/download":
			_, _ = io.WriteString(w, "%PDF-1.7 synthetic candidate")
		default:
			w.WriteHeader(404)
		}
	}))
	defer remote.Close()
	source.newClient = func(cfg *core.Config) *core.Client { cfg.BaseURL = remote.URL; return core.NewClient(cfg) }
	caller := types.Caller{TenantID: 99, UserID: "owner", Role: types.TenantRoleOwner}
	ctx := types.WithCaller(personalUser("owner"), caller)
	grant, err := access.ResolveKB(ctx, access.KBRequest{Caller: caller}, &kb, types.OrgRoleEditor, nil, nil)
	require.NoError(t, err)
	ctx = grant.Context(ctx)
	manifest, err := source.Import(ctx, kb.ID, "https://tenant.feishu.cn/file/real")
	require.NoError(t, err)
	var op types.TaskPendingOp
	require.NoError(t, db.Where("dedup_key=?", "personal-"+manifest.AttemptID).Take(&op).Error)
	var envelope types.KnowledgeDispatch
	require.NoError(t, json.Unmarshal(op.Payload, &envelope))
	require.NoError(t, source.recovery.Process(ctx, envelope.Task))
	var k types.Knowledge
	require.NoError(t, db.Where("id=?", manifest.LogicalKnowledgeID).Take(&k).Error)
	require.NotEqual(t, types.ParseStatusCompleted, k.ParseStatus)
	vector := make([]float32, 1024)
	vector[0] = 1
	embed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"index": 0, "embedding": vector}}}))
	}))
	defer embed.Close()
	require.NoError(t, db.Create(&types.Model{ID: "synthetic", TenantID: 99, Name: "controlled-1024", Type: types.ModelTypeEmbedding, Source: types.ModelSourceRemote, Status: types.ModelStatusActive, Parameters: types.ModelParameters{BaseURL: embed.URL + "/v1", Provider: "openai", EmbeddingParameters: types.EmbeddingParameters{Dimension: 1024}}}).Error)
	require.NoError(t, db.Exec(`UPDATE tenants SET retriever_engines='[{"retriever_type":"keywords","retriever_engine_type":"postgres"},{"retriever_type":"vector","retriever_engine_type":"postgres"}]' WHERE id=99`).Error)
	driver := pgdriver.NewPostgresRetrieveEngineRepository(db)
	registry := retriever.NewRetrieveEngineRegistry(nil, nil)
	require.NoError(t, registry.Register(retriever.NewKVHybridRetrieveEngine(driver, types.PostgresRetrieverEngineType)))
	s := NewPortalPersonalKnowledge(&config.Config{PersonalMCP: &config.PersonalMCPConfig{Enabled: true, PolicyReference: "ISOLATED_DYNAMIC"}}, db, publishedMembersM7{repo: repository.NewTenantMemberRepository(db)}, repository.NewTenantAPIKeyRepository(db), repository.NewKnowledgeBaseRepository(db), repository.NewTenantRepository(db), repository.NewKnowledgeRepository(db), repository.NewChunkRepository(db), NewModelService(repository.NewModelRepository(db), repository.NewKnowledgeBaseRepository(db), nil, nil, nil, nil), registry, nil, nil, nil)
	employee := fixture.Key("A", 1, 1, true)
	_, err = s.ReadDocument(employee, k.ID)
	require.Error(t, err, "download is not indexed Knowledge")
	// Controlled parser boundary: real stored Chunk and real ParadeDB index.
	chunk := types.Chunk{ID: uuid.NewString(), TenantID: 99, KnowledgeBaseID: kb.ID, KnowledgeID: k.ID, Content: "SYNTHETICFEISHUPUBLIC", ChunkType: types.ChunkTypeText, IsEnabled: true, ContentRevision: 1}
	require.NoError(t, db.Create(&chunk).Error)
	require.NoError(t, driver.Save(context.Background(), &types.IndexInfo{SourceID: chunk.ID, SourceType: types.ChunkSourceType, ChunkID: chunk.ID, KnowledgeID: k.ID, KnowledgeBaseID: kb.ID, Content: chunk.Content, IsEnabled: true}, map[string]any{"embedding": map[string][]float32{chunk.ID: vector}}))
	require.NoError(t, db.Model(&k).Updates(map[string]any{"parse_status": "completed", "enable_status": "enabled"}).Error)
	result, err := s.Search(employee, []string{kb.ID}, "SYNTHETICFEISHUPUBLIC", "hybrid", 10)
	require.NoError(t, err)
	require.NotEmpty(t, result.Results)
	require.Equal(t, k.ID, result.Results[0].DocumentID)
	read, err := s.ReadDocument(employee, k.ID)
	require.NoError(t, err)
	require.Equal(t, "SYNTHETICFEISHUPUBLIC", read[0].Text)
	var approved int64
	require.NoError(t, db.Table("kb_publication_policies").Where("state='approved'").Count(&approved).Error)
	require.Zero(t, approved)
	t.Log("M7_FEISHU: real import/recovery/dispatch -> controlled parser -> real 1024 index -> nonmember personal MCP PASS; remote Feishu and parser are synthetic")
}
