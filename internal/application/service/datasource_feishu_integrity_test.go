package service

import (
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Only the external file store, parser queue and KB configuration are synthetic.
// DataSourceService, knowledgeService and KnowledgeRepository execute real SQL.
// This does not exercise ProcessSync admission/fencing or PostgreSQL.
type a4IntegrityFiles struct {
	interfaces.FileService
	root string
}

func (f *a4IntegrityFiles) SaveFile(_ context.Context, h *multipart.FileHeader, _ uint64, id string) (string, error) {
	r, err := h.Open()
	if err != nil {
		return "", err
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	p := filepath.Join(f.root, id)
	return p, os.WriteFile(p, b, 0600)
}
func (f *a4IntegrityFiles) DeleteFile(_ context.Context, p string) error { return os.Remove(p) }

type a4IntegrityOps struct{}

func (a4IntegrityOps) List(context.Context, *core.Client, string) ([]string, error, error) {
	return []string{"bad", "good"}, nil, nil
}
func (a4IntegrityOps) Token(n string) string  { return n }
func (a4IntegrityOps) Title(n string) string  { return n }
func (a4IntegrityOps) ObjType(string) string  { return "docx" }
func (a4IntegrityOps) EditTime(string) string { return "new" }
func (a4IntegrityOps) Fetch(ctx context.Context, c *core.Client, n, r string, _ bool) ([]*types.FetchedItem, error) {
	return core.FetchDocxWithBlocks(ctx, c, core.DocxFetchInput{DocToken: n, ObjToken: n, ResourceID: r})
}
func (a4IntegrityOps) ListFailureItems(string, error) []types.FetchedItem { return nil }
func (a4IntegrityOps) ResourceNoun() string                               { return "synthetic-documents" }
func (a4IntegrityOps) EmptyResourceIDsError() string                      { return "empty resource" }
func (a4IntegrityOps) LogTag() string                                     { return "[A4-SQL]" }
func (a4IntegrityOps) DecodeCursorTimes(m map[string]interface{}) map[string]map[string]string {
	b, _ := json.Marshal(m["times"])
	out := map[string]map[string]string{}
	_ = json.Unmarshal(b, &out)
	return out
}
func (a4IntegrityOps) EncodeCursor(m map[string]map[string]string, _ time.Time) *types.SyncCursor {
	b, _ := json.Marshal(m)
	var copy map[string]interface{}
	_ = json.Unmarshal(b, &copy)
	return &types.SyncCursor{ConnectorCursor: map[string]interface{}{"times": copy}}
}

type a4IntegritySQLHandler struct {
	svc    *DataSourceService
	ds     *types.DataSource
	result *types.SyncResult
	items  []types.FetchedItem
}

func (h *a4IntegritySQLHandler) Emit(ctx context.Context, item types.FetchedItem) error {
	h.items = append(h.items, item)
	h.result.Total++
	h.svc.applyFetchedItem(ctx, h.ds, &item, nil, h.result)
	return nil
}
func (h *a4IntegritySQLHandler) Checkpoint(ctx context.Context, c *types.SyncCursor) error {
	var err error
	h.ds.LastSyncCursor, err = c.ToJSON()
	if err != nil {
		return err
	}
	h.ds.LastSyncResult, err = h.result.ToJSON()
	if err != nil {
		return err
	}
	return h.svc.dsRepo.UpdateSyncState(ctx, h.ds)
}

func TestA4FeishuIntegrityRealSQLiteService(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1,localhost")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	t.Setenv("FEISHU_DOCX_PARSE_MODE", "blocks")
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "synthetic.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	// Explicit test-only DDL. No AutoMigrate, migration runner or external DSN.
	for _, ddl := range []string{
		`CREATE TABLE knowledges(id TEXT PRIMARY KEY,tenant_id INTEGER,knowledge_base_id TEXT,type TEXT,title TEXT,description TEXT,source TEXT,channel TEXT,parse_status TEXT,pending_subtasks_count INTEGER,summary_status TEXT,profile TEXT,enable_status TEXT,embedding_model_id TEXT,file_name TEXT,folder_path TEXT,file_type TEXT,file_size INTEGER,file_hash TEXT,file_path TEXT,storage_size INTEGER,metadata TEXT,custom_metadata TEXT,last_faq_import_result TEXT,created_at DATETIME,updated_at DATETIME,processed_at DATETIME,error_message TEXT,deleted_at DATETIME)`,
		`CREATE TABLE data_sources(id TEXT PRIMARY KEY,tenant_id INTEGER,knowledge_base_id TEXT,status TEXT,last_sync_at DATETIME,last_sync_cursor TEXT,last_sync_result TEXT,error_message TEXT,updated_at DATETIME,deleted_at DATETIME)`,
		`CREATE TABLE knowledge_tag_relations(knowledge_id TEXT,tag_id TEXT)`,
		`CREATE TABLE knowledge_tags(id TEXT,seq_id INTEGER,tenant_id INTEGER,knowledge_base_id TEXT,name TEXT,color TEXT,sort_order INTEGER,created_at DATETIME,updated_at DATETIME)`,
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	repo := repository.NewKnowledgeRepository(db)
	for _, k := range []*types.Knowledge{
		{ID: "history", TenantID: 1, KnowledgeBaseID: "kb-A", Title: "historical", Metadata: types.JSON(`{"datasource_id":"ds-A","external_id":"historical","source_updated_at":"old"}`)},
		{ID: "other-tenant", TenantID: 2, KnowledgeBaseID: "kb-B", Title: "isolated", Metadata: types.JSON(`{"datasource_id":"ds-A","external_id":"bad"}`)},
	} {
		require.NoError(t, repo.CreateKnowledge(context.Background(), k))
	}
	ks := &knowledgeService{repo: repo, kbService: &createKnowledgeFileKBServiceStub{kb: &types.KnowledgeBase{ID: "kb-A", TenantID: 1}}, fileSvc: &a4IntegrityFiles{root: t.TempDir()}, task: &createKnowledgeTaskEnqueuerStub{}}
	svc := &DataSourceService{dsRepo: repository.NewDataSourceRepository(db), knowledgeService: ks}
	ds := &types.DataSource{ID: "ds-A", TenantID: 1, KnowledgeBaseID: "kb-A", Type: types.ConnectorTypeFeishu, SyncDeletions: true, Status: types.DataSourceStatusActive}
	require.NoError(t, db.Exec(`INSERT INTO data_sources(id,tenant_id,knowledge_base_id,status) VALUES('ds-A',1,'kb-A','active')`).Error)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, &types.Tenant{ID: 1})
	var broken atomic.Bool
	broken.Store(true)
	var downloads atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case filepath.Base(r.URL.Path) == "tenant_access_token":
			_, _ = io.WriteString(w, `{"code":0,"tenant_access_token":"synthetic-provider-key","expire":7200}`)
		case filepath.Base(r.URL.Path) == "internal":
			_, _ = io.WriteString(w, `{"code":0,"tenant_access_token":"synthetic-provider-key","expire":7200}`)
		case filepath.Base(r.URL.Path) == "blocks":
			if strings.Contains(r.URL.Path, "/bad/") {
				_, _ = io.WriteString(w, `{"code":0,"data":{"items":[{"block_type":2,"text":{"elements":[{"text_run":{"content":"bad text"}}]}},{"block_type":18,"bitable":{"token":"app_table"}}]}}`)
			} else {
				_, _ = io.WriteString(w, `{"code":0,"data":{"items":[{"block_type":2,"text":{"elements":[{"text_run":{"content":"good text"}}]}}]}}`)
			}
		case filepath.Base(r.URL.Path) == "fields":
			if broken.Load() {
				_, _ = io.WriteString(w, `{"code":0,"data":{"items":[],"has_more":true,"page_token":"TOKEN-CANARY"}}`)
			} else {
				_, _ = io.WriteString(w, `{"code":0,"data":{"items":[],"has_more":false}}`)
			}
		case filepath.Base(r.URL.Path) == "search":
			_, _ = io.WriteString(w, `{"code":0,"data":{"items":[],"has_more":false}}`)
		case filepath.Base(r.URL.Path) == "download":
			downloads.Add(1)
			w.WriteHeader(500)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := core.NewClient(&core.Config{AppID: "synthetic-app", AppSecret: "synthetic-secret", BaseURL: srv.URL})
	ops := a4IntegrityOps{}
	cursor := ops.EncodeCursor(map[string]map[string]string{"space-A": {"bad": "old", "good": "old"}}, time.Time{})
	for attempt := 0; attempt < 3; attempt++ {
		if attempt == 1 {
			broken.Store(false)
		}
		h := &a4IntegritySQLHandler{svc: svc, ds: ds, result: &types.SyncResult{}}
		cursor, err = core.FetchStreamEngine(ctx, c, &types.DataSourceConfig{ResourceIDs: []string{"space-A"}}, cursor, h, ops)
		require.NoError(t, err)
		require.NoError(t, h.Checkpoint(ctx, cursor))
		var persisted string
		require.NoError(t, db.Raw("SELECT last_sync_cursor FROM data_sources WHERE id='ds-A'").Scan(&persisted).Error)
		expected, e := cursor.ToJSON()
		require.NoError(t, e)
		require.Equal(t, string(expected), persisted)
		if attempt == 0 {
			require.Equal(t, 1, h.result.Failed)
			require.Equal(t, 1, h.result.Created)
			require.Equal(t, "old", ops.DecodeCursorTimes(cursor.ConnectorCursor)["space-A"]["bad"])
			missing, e := repo.FindByDataSourceExternalID(ctx, 1, "kb-A", "ds-A", "bad")
			require.NoError(t, e)
			require.Nil(t, missing)
			for _, item := range h.items {
				require.False(t, item.IsDeleted)
				if item.ExternalID == "bad" {
					require.Empty(t, item.Content)
					require.False(t, item.ReplacesSubtree)
				}
			}
		} else if attempt == 1 {
			require.Zero(t, h.result.Failed)
			require.Equal(t, 1, h.result.Created)
		} else {
			require.Zero(t, h.result.Total)
		}
		history, e := repo.GetKnowledgeByID(ctx, 1, "history")
		require.NoError(t, e)
		require.Equal(t, "old", history.GetMetadata()["source_updated_at"])
		_, e = repo.GetKnowledgeByID(ctx, 2, "other-tenant")
		require.NoError(t, e)
		t.Logf("SQLite attempt=%d created=%d failed=%d deleted=%d checkpointBad=%s", attempt, h.result.Created, h.result.Failed, h.result.Deleted, ops.DecodeCursorTimes(cursor.ConnectorCursor)["space-A"]["bad"])
	}
	require.Zero(t, downloads.Load())
	// Actual driver failure reaches the real Service result, without fake success.
	require.NoError(t, db.Exec(`CREATE TRIGGER reject_fault BEFORE INSERT ON knowledges WHEN NEW.title='fault.md' BEGIN SELECT RAISE(ABORT,'SQL-CANARY'); END`).Error)
	r := &types.SyncResult{}
	svc.applyFetchedItem(ctx, ds, &types.FetchedItem{ExternalID: "fault", Title: "fault", Content: []byte("fault"), FileName: "fault.md"}, nil, r)
	require.Equal(t, 1, r.Failed)
	require.Zero(t, r.Created)
	missing, e := repo.FindByDataSourceExternalID(ctx, 1, "kb-A", "ds-A", "fault")
	require.NoError(t, e)
	require.Nil(t, missing)
	for _, problem := range r.Errors {
		require.NotContains(t, problem.Message, "SQL-CANARY")
	}
	// Repository query distinguishes the other tenant; no mock grant adapter.
	wrong, e := repo.FindByDataSourceExternalID(ctx, 1, "kb-A", "ds-A", "historical")
	require.NoError(t, e)
	require.NotNil(t, wrong)
	wrong, e = repo.FindByDataSourceExternalID(ctx, 2, "kb-B", "ds-A", "historical")
	require.NoError(t, e)
	require.Nil(t, wrong)
}
