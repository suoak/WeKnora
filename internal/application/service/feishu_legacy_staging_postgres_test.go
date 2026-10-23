package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	files "github.com/Tencent/WeKnora/internal/application/service/file"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type legacyLiveKB struct {
	interfaces.KnowledgeBaseService
	db *gorm.DB
}

func (s *legacyLiveKB) GetKnowledgeBaseByID(ctx context.Context, id string) (*types.KnowledgeBase, error) {
	var kb types.KnowledgeBase
	err := s.db.WithContext(ctx).First(&kb, "id=?", id).Error
	kb.EnsureDefaults()
	return &kb, err
}

type legacyLiveModel struct {
	interfaces.ModelService
	db *gorm.DB
}

func (s *legacyLiveModel) GetEmbeddingModelForTenant(ctx context.Context, id string, tenant uint64) (embedding.Embedder, error) {
	var model types.Model
	if err := s.db.WithContext(ctx).First(&model, "id=? AND tenant_id=?", id, tenant).Error; err != nil {
		return nil, err
	}
	return embedding.NewEmbedder(embedding.ConfigFromModel(&model, "", ""), nil, nil)
}

type legacyNoCloud struct{ interfaces.TenantService }

func (*legacyNoCloud) GetWeKnoraCloudCredentials(context.Context) *types.WeKnoraCloudCredentials {
	return nil
}

func TestFeishuLegacyRealParserEmbeddingPostgres(t *testing.T) {
	if os.Getenv("FEISHU_LEGACY_TEST_DSN") == "" {
		t.Skip("requires disposable feishu_atomic_test PostgreSQL/ParadeDB")
	}
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	t.Chdir(root)
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	t.Setenv("FEISHU_LEGACY_SYNC_SUSPENDED", "false")
	t.Setenv("RETRIEVE_DRIVER", "postgres")
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	var fail atomic.Bool
	var failureCalls atomic.Int32
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if fail.Load() && failureCalls.Add(1) == 2 {
			http.Error(w, "synthetic middle batch rejected", http.StatusBadRequest)
			return
		}
		var req struct {
			Input []string `json:"input"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			http.Error(w, "bad request", 400)
			return
		}
		data := []any{}
		for i := range req.Input {
			v := make([]float32, 1024)
			v[i%1024] = 1
			data = append(data, map[string]any{"object": "embedding", "index": i, "embedding": v})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data, "model": "synthetic", "usage": map[string]int{"prompt_tokens": 1, "total_tokens": 1}})
	}))
	defer server.Close()
	for sourceIndex, scenario := range []struct {
		kind   string
		parent bool
	}{{"feishu", false}, {"feishu_drive", false}, {"feishu", true}, {"feishu_drive", true}} {
		kind := scenario.kind
		t.Run(fmt.Sprintf("%s/parent=%v", kind, scenario.parent), func(t *testing.T) {
			dsn := feishulegacy.PrivateDSN(t)
			require.NoError(t, database.RunMigrations(dsn))
			db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			localRoot := t.TempDir()
			t.Setenv("LOCAL_STORAGE_BASE_DIR", localRoot)
			tenant := &types.Tenant{ID: 809902, Name: "synthetic-real-parser", Status: "active"}
			require.NoError(t, db.Create(tenant).Error)
			model := &types.Model{ID: uuid.NewString(), TenantID: tenant.ID, Name: "synthetic", Type: types.ModelTypeEmbedding, Source: types.ModelSourceRemote}
			model.Parameters.BaseURL = server.URL + "/v1"
			model.Parameters.Provider = "generic"
			model.Parameters.EmbeddingParameters.Dimension = 1024
			require.NoError(t, db.Create(model).Error)
			kb := &types.KnowledgeBase{ID: uuid.NewString(), TenantID: tenant.ID, Name: "synthetic-private", Type: "document", EmbeddingModelID: model.ID}
			require.NoError(t, db.Create(kb).Error)
			ks := &knowledgeService{config: &config.Config{}, repo: repository.NewKnowledgeRepository(db), kbService: &legacyLiveKB{db: db}, modelService: &legacyLiveModel{db: db}, tenantService: &legacyNoCloud{}, fileSvc: files.NewLocalFileService(localRoot, "")}
			kb.ChunkingConfig.EnableParentChild = scenario.parent
			kb.ChunkingConfig.ParentChunkSize = 4096
			kb.ChunkingConfig.ChildChunkSize = 1024
			chunkConfig, err := json.Marshal(kb.ChunkingConfig)
			require.NoError(t, err)
			require.NoError(t, db.Model(kb).UpdateColumn("chunking_config", types.JSON(chunkConfig)).Error)
			if kind == "feishu_drive" {
				backend := &types.StorageBackend{TenantID: tenant.ID, Name: "registered local", Provider: "local", Config: types.StorageBackendConfig{}, Status: types.StorageBackendStatusActive}
				require.NoError(t, repository.NewStorageBackendRepository(db).Create(context.Background(), backend))
				kb.StorageBackendID = &backend.ID
				tenant.DefaultStorageBackendID = &backend.ID
				require.NoError(t, db.Model(kb).UpdateColumn("storage_backend_id", backend.ID).Error)
				require.NoError(t, db.Model(tenant).UpdateColumn("default_storage_backend_id", backend.ID).Error)
				ks.resourceCatalog = NewResourceCatalog(repository.NewResourceRepository(db))
				ks.storageResolver = NewStorageBackendServiceWithResources(repository.NewStorageBackendRepository(db), db, ks.resourceCatalog)
				ks.tenantRepo = repository.NewTenantRepository(db)
			}
			ds := &types.DataSource{ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID, Name: "synthetic-" + kind, Type: kind, Status: "active", Config: types.JSON(`{}`)}
			require.NoError(t, db.Create(ds).Error)
			log := &types.SyncLog{ID: uuid.NewString(), DataSourceID: ds.ID, TenantID: tenant.ID, Status: types.SyncLogStatusRunning, ExecutionGeneration: 1, ExecutionClaimed: true}
			require.NoError(t, db.Create(log).Error)
			ctx := types.WithLegacyReceiptSet(types.WithSyncExecution(context.Background(), types.SyncExecution{DataSourceID: ds.ID, SyncLogID: log.ID, TenantID: tenant.ID, Generation: 1}))
			ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)
			ctx = context.WithValue(ctx, types.TenantIDContextKey, tenant.ID)
			item := &types.FetchedItem{ExternalID: "stable-source", FileName: "source.md", Title: "synthetic source", Content: []byte("# Normal knowledge\n\noldparsercanary normal content."), UpdatedAt: time.Date(2026, 10, 21, 2, sourceIndex, 0, 0, time.UTC)}
			if scenario.parent {
				item.Content = []byte(strings.Repeat("oldparsercanary normal content. ", 300))
			}
			first, e := ks.StageFeishuLegacyItem(ctx, ds, item, nil)
			require.NoError(t, e)
			require.Equal(t, 1024, first.Dimension)
			require.True(t, first.Created)
			var oldPath string
			require.NoError(t, db.Model(&types.Knowledge{}).Select("file_path").Where("id=?", first.KnowledgeID).Scan(&oldPath).Error)
			item.Content = []byte("# Normal knowledge\n\nnewparsercanary updated content.")
			if scenario.parent {
				item.Content = []byte(strings.Repeat("newparsercanary updated content. ", 300))
			}
			item.UpdatedAt = item.UpdatedAt.Add(time.Minute)
			second, e := ks.StageFeishuLegacyItem(ctx, ds, item, nil)
			require.NoError(t, e)
			require.Equal(t, first.KnowledgeID, second.KnowledgeID)
			require.False(t, second.Created)
			if scenario.parent {
				require.Greater(t, second.Chunks, second.Vectors)
				var invalid, parentVectors int64
				require.NoError(t, db.Raw("SELECT count(*) FROM chunks c LEFT JOIN chunks p ON p.id=c.parent_chunk_id WHERE c.knowledge_id=? AND c.deleted_at IS NULL AND c.chunk_type='text' AND (p.id IS NULL OR p.deleted_at IS NOT NULL OR p.chunk_type<>'parent_text' OR p.knowledge_id<>c.knowledge_id)", first.KnowledgeID).Scan(&invalid).Error)
				require.Zero(t, invalid)
				require.NoError(t, db.Raw("SELECT count(*) FROM chunks WHERE knowledge_id=? AND deleted_at IS NULL AND chunk_type='text' AND (pre_chunk_id<>'' OR next_chunk_id<>'')", first.KnowledgeID).Scan(&invalid).Error)
				require.Zero(t, invalid, "parent-child children must retain ordinary ingestion adjacency semantics")
				require.NoError(t, db.Raw("SELECT count(*) FROM embeddings e JOIN chunks c ON c.id=e.chunk_id WHERE c.knowledge_id=? AND c.chunk_type='parent_text'", first.KnowledgeID).Scan(&parentVectors).Error)
				require.Zero(t, parentVectors, "parents must never become vector/BM25 search hits")
			}
			var n int64
			require.NoError(t, db.Raw("SELECT count(*) FROM embeddings WHERE knowledge_id=? AND content ||| 'newparsercanary'", first.KnowledgeID).Scan(&n).Error)
			require.Positive(t, n)
			before := calls.Load()
			_, e = ks.StageFeishuLegacyItem(ctx, ds, item, nil)
			var duplicate *types.DuplicateKnowledgeError
			require.ErrorAs(t, e, &duplicate)
			require.Equal(t, before, calls.Load(), "unchanged source must not re-embed")
			item.SourceResourceID = "another-configured-root"
			_, e = ks.StageFeishuLegacyItem(ctx, ds, item, nil)
			require.ErrorAs(t, e, &duplicate)
			require.Equal(t, before, calls.Load(), "root location alone must not rebuild unchanged content")
			var oldChunks, oldVectors, oldKnowledge string
			const knowledgeSQL = "SELECT to_jsonb(k)::text FROM knowledges k WHERE id=?"
			const chunksSQL = "SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY c.id)::text,'[]') " +
				"FROM chunks c WHERE knowledge_id=?"
			const vectorsSQL = "SELECT coalesce(jsonb_agg(to_jsonb(e) ORDER BY to_jsonb(e)::text)::text,'[]') " +
				"FROM embeddings e WHERE knowledge_id=?"
			require.NoError(t, db.Raw(knowledgeSQL, first.KnowledgeID).Scan(&oldKnowledge).Error)
			require.NoError(t, db.Raw(chunksSQL, first.KnowledgeID).Scan(&oldChunks).Error)
			require.NoError(t, db.Raw(vectorsSQL, first.KnowledgeID).Scan(&oldVectors).Error)
			// More than 20 default-sized chunks force a successful first batch
			// followed by an injected second-batch failure, before publication.
			item.Content = []byte(strings.Repeat("mustnotpublish embedding failure content. ", 10000))
			item.UpdatedAt = item.UpdatedAt.Add(time.Minute)
			failureCalls.Store(0)
			fail.Store(true)
			_, e = ks.StageFeishuLegacyItem(ctx, ds, item, nil)
			require.Error(t, e)
			fail.Store(false)
			require.EqualValues(t, 2, failureCalls.Load(), "failure must occur in the middle batch")
			var currentChunks, currentVectors, currentKnowledge string
			require.NoError(t, db.Raw(knowledgeSQL, first.KnowledgeID).Scan(&currentKnowledge).Error)
			require.NoError(t, db.Raw(chunksSQL, first.KnowledgeID).Scan(&currentChunks).Error)
			require.NoError(t, db.Raw(vectorsSQL, first.KnowledgeID).Scan(&currentVectors).Error)
			require.JSONEq(t, oldChunks, currentChunks, "old chunks unchanged after candidate failure")
			require.JSONEq(t, oldVectors, currentVectors, "old searchable vectors unchanged after candidate failure")
			require.JSONEq(t, oldKnowledge, currentKnowledge, "old document and its metadata must remain unchanged")
			require.NoError(t, db.Raw(
				"SELECT count(*) FROM embeddings WHERE knowledge_id=? AND content LIKE '%mustnotpublish%'",
				first.KnowledgeID).Scan(&n).Error)
			require.Zero(t, n, "unpublished candidate must not be searchable")
			require.NoError(t, db.Raw("SELECT count(*) FROM embeddings WHERE knowledge_id=? AND content ||| 'newparsercanary'", first.KnowledgeID).Scan(&n).Error)
			require.Positive(t, n)
			oldFile, e := ks.resolveFileServiceForPath(ctx, kb, oldPath).GetFile(ctx, oldPath)
			require.NoError(t, e, "do not destroy old file before safe publication/cleanup")
			require.NoError(t, oldFile.Close())
			guardDB := types.EnableLegacyReadGuards(db)
			fileGate := repository.NewTenantRepository(guardDB).(interface {
				CheckLegacyFileRead(context.Context, uint64, string) (bool, error)
			})
			_, e = fileGate.CheckLegacyFileRead(context.Background(), tenant.ID, oldPath)
			require.ErrorIs(t, e, types.ErrLegacySourceRestricted, "old anonymous links must not bypass version replacement")
			var currentPath string
			require.NoError(t, db.WithContext(ctx).Model(&types.Knowledge{}).Select("file_path").Where("id=?", first.KnowledgeID).Scan(&currentPath).Error)
			managed, e := fileGate.CheckLegacyFileRead(context.Background(), tenant.ID, currentPath)
			require.NoError(t, e)
			require.True(t, managed)
			physical := currentPath
			if _, registered := types.ParseResourcePath(currentPath); registered {
				resource, err := ks.resourceCatalog.Resolve(ctx, currentPath)
				require.NoError(t, err)
				physical = resource.PhysicalPath
				bound, err := repository.NewResourceRepository(db).IsReferencedByKnowledgeBase(ctx, tenant.ID, kb.ID, resource.ID)
				require.NoError(t, err)
				require.True(t, bound, "atomic publication must bind the real stable Knowledge ID")
			}
			store := repository.NewFeishuLegacyStore(db)
			_, e = store.RestrictSource(ctx, ds, item.ExternalID, false)
			require.NoError(t, e)
			for _, p := range []string{currentPath, physical, filepath.Dir(physical) + "/./" + filepath.Base(physical)} {
				_, e = fileGate.CheckLegacyFileRead(context.Background(), tenant.ID, p)
				require.ErrorIs(t, e, types.ErrLegacySourceRestricted)
			}
			item.Content = []byte("recoveredparsercanary safely reconciled")
			item.UpdatedAt = item.UpdatedAt.Add(time.Minute)
			recovered, e := ks.StageFeishuLegacyItem(ctx, ds, item, nil)
			require.NoError(t, e)
			require.Equal(t, first.KnowledgeID, recovered.KnowledgeID)
			require.ErrorIs(t, types.CheckLegacySourceRead(guardDB, first.KnowledgeID, kb.ID), types.ErrLegacySourceRestricted,
				"one recovered document must not release a source before full reconciliation")
			exerciseLegacyProcessSync(t, guardDB, ks, ds, item, first.KnowledgeID)
			require.NoError(t, types.CheckLegacySourceRead(guardDB, first.KnowledgeID, kb.ID))
			exerciseLegacyHTTPConnector(t, guardDB, ks, tenant.ID, kb.ID, kind)
			t.Setenv("FEISHU_LEGACY_SYNC_SUSPENDED", "true")
			_, e = ks.StageFeishuLegacyItem(ctx, ds, item, nil)
			require.ErrorIs(t, e, datasource.ErrLegacyFeishuSyncSuspended)
		})
	}
}
