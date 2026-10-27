package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/drive"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/wiki"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Actual Wiki/Drive HTTP connector -> ProcessSync -> private parser/index ->
// atomic publish. Only the remote Feishu server and embedding endpoint are
// synthetic; this is not evidence of real Feishu cloud permission validation.
func exerciseLegacyHTTPConnector(t *testing.T, db *gorm.DB, ks *knowledgeService, tenant uint64, kbID, kind string) {
	t.Helper()
	t.Setenv("FEISHU_DOCX_PARSE_MODE", "blocks")
	var revision atomic.Int64
	revision.Store(time.Date(2026, 10, 21, 4, 0, 0, 0, time.UTC).Unix())
	var missing atomic.Bool
	var forbidden atomic.Bool
	var blocks atomic.Int64
	var body atomic.Value
	body.Store("httpoldcanary ordinary Feishu content")
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		write := func(value any) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(value)
		}
		if strings.Contains(r.URL.Path, "tenant_access_token") {
			write(map[string]any{"code": 0, "tenant_access_token": "synthetic-only", "expire": 7200})
			return
		}
		if strings.Contains(r.URL.Path, "/nodes") || r.URL.Path == "/open-apis/drive/v1/files" {
			if forbidden.Load() {
				w.WriteHeader(403)
				write(map[string]any{"code": 1069902, "msg": "synthetic temporary access failure"})
				return
			}
			if kind == "feishu" {
				rows := []core.WikiNode{}
				if !missing.Load() {
					rows = append(rows, core.WikiNode{NodeToken: "http-document", ObjToken: "http-object", ObjType: "docx", Title: "synthetic HTTP", ObjEditTime: strconv.FormatInt(revision.Load(), 10)})
				}
				write(core.WikiNodeListResponse{Data: core.WikiNodeListData{Items: rows}})
			} else {
				rows := []core.DriveFile{}
				if !missing.Load() {
					rows = append(rows, core.DriveFile{Token: "http-document", Type: "docx", Name: "synthetic HTTP", ModifiedTime: strconv.FormatInt(revision.Load(), 10), URL: server.URL + "/docx/http-document"})
				}
				write(core.DriveFileListResponse{Data: core.DriveFileListData{Files: rows}})
			}
			return
		}
		if strings.Contains(r.URL.Path, "/blocks") {
			blocks.Add(1)
			write(core.DocxBlocksResponse{Data: core.DocxBlocksData{Items: []core.DocxBlock{{BlockID: "page", BlockType: core.BlockTypePage, Children: []string{"text"}}, {BlockID: "text", ParentID: "page", BlockType: core.BlockTypeText, Text: &core.BlockText{Elements: []core.TextElement{{TextRun: &core.TextRun{Content: body.Load().(string)}}}}}}}})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	region := core.Region{ConnectorType: kind, OpenBaseURL: server.URL, WebBaseURL: server.URL, Label: "SyntheticHTTP"}
	registry := datasource.NewConnectorRegistry()
	if kind == "feishu" {
		require.NoError(t, registry.Register(wiki.NewConnector(region)))
	} else {
		require.NoError(t, registry.Register(drive.NewDriveConnector(region)))
	}
	config := types.DataSourceConfig{Type: kind, Credentials: map[string]interface{}{"app_id": "synthetic", "app_secret": "synthetic-not-a-secret", "base_url": server.URL}, ResourceIDs: []string{"root"}}
	encoded, e := config.ToJSON()
	require.NoError(t, e)
	ds := &types.DataSource{ID: uuid.NewString(), TenantID: tenant, KnowledgeBaseID: kbID, Name: "HTTP fixture", Type: kind, Status: "active", Config: encoded, SyncMode: types.SyncModeIncremental}
	require.NoError(t, db.Create(ds).Error)
	svc := &DataSourceService{dsRepo: repository.NewDataSourceRepository(db), syncLogRepo: repository.NewSyncLogRepository(db), knowledgeService: ks, kbService: ks.kbService, tenantRepo: repository.NewTenantRepository(db), tagService: &legacyNoAutoTag{}, connectorRegistry: registry}
	run := func(full bool) error {
		log := &types.SyncLog{ID: uuid.NewString(), TenantID: tenant, DataSourceID: ds.ID, Status: types.SyncLogStatusRunning}
		require.NoError(t, db.Create(log).Error)
		payload, _ := json.Marshal(types.DataSourceSyncPayload{TenantID: tenant, DataSourceID: ds.ID, SyncLogID: log.ID, ForceFull: full})
		return svc.ProcessSync(context.Background(), asynq.NewTask("datasource:sync", payload))
	}
	require.NoError(t, run(false))
	var kid string
	require.NoError(t, db.Raw("SELECT id FROM knowledges WHERE metadata->>'datasource_id'=? AND metadata->>'external_id'='http-document'", ds.ID).Scan(&kid).Error)
	require.NotEmpty(t, kid)
	seen := blocks.Load()
	require.NoError(t, run(false))
	require.Equal(t, seen, blocks.Load(), "unchanged source must skip real HTTP export/blocks")
	revision.Add(60)
	body.Store("httpnewcanary updated Feishu content")
	require.NoError(t, run(false))
	var savedKID string
	require.NoError(t, db.Raw("SELECT id FROM knowledges WHERE metadata->>'datasource_id'=?", ds.ID).Scan(&savedKID).Error)
	require.Equal(t, kid, savedKID)
	var count int64
	require.NoError(t, db.Raw("SELECT count(*) FROM embeddings WHERE knowledge_id=? AND content ||| 'httpnewcanary'", kid).Scan(&count).Error)
	require.Positive(t, count)
	var saved types.DataSource
	require.NoError(t, db.First(&saved, "id=?", ds.ID).Error)
	cursor := string(saved.LastSyncCursor)
	forbidden.Store(true)
	require.Error(t, run(false))
	require.NoError(t, db.First(&saved, "id=?", ds.ID).Error)
	require.Equal(t, cursor, string(saved.LastSyncCursor))
	require.ErrorIs(t, types.CheckLegacySourceRead(db, kid, kbID), types.ErrLegacySourceRestricted)
	forbidden.Store(false)
	require.NoError(t, run(true))
	require.NoError(t, types.CheckLegacySourceRead(db, kid, kbID))
	missing.Store(true)
	require.Error(t, run(false), "listing absence must quarantine, never delete then acknowledge")
	require.ErrorIs(t, types.CheckLegacySourceRead(db, kid, kbID), types.ErrLegacySourceRestricted)
	missing.Store(false)
	require.NoError(t, run(true))
	require.NoError(t, types.CheckLegacySourceRead(db, kid, kbID))
}
