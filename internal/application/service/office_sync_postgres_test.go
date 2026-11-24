package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	files "github.com/Tencent/WeKnora/internal/application/service/file"
	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/officeguard"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Real PostgreSQL rows, GIN FTS index, local file store and creation service.
// Only asynchronous enqueue is captured; no security switch is disabled.
func TestOfficeSyncPostgresRetainsOldVersionAndContinuesBatch(t *testing.T) {
	db := syncdb.Open(t, "postgres")
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.Chunk{}, &types.KnowledgeTag{}, &types.KnowledgeTagRelation{}))
	require.NoError(t, db.Exec(`CREATE TABLE office_retained_index(id text PRIMARY KEY,knowledge_id text,content text,is_enabled boolean);
 CREATE INDEX office_retained_fts ON office_retained_index USING gin(to_tsvector('simple',content));
 INSERT INTO office_retained_index VALUES('old-vector','old-knowledge','retainedcanary',true)`).Error)
	root := t.TempDir()
	oldPath := filepath.Join(root, "original.docx")
	oldBytes := []byte(officeFixture(t, "normal.docx"))
	require.NoError(t, os.WriteFile(oldPath, oldBytes, 0600))
	meta, _ := json.Marshal(map[string]string{"datasource_id": "office-source", "external_id": "parent#office"})
	old := &types.Knowledge{ID: "old-knowledge", TenantID: 1, KnowledgeBaseID: "kb-office", Type: "file", FileName: "original.docx", FilePath: oldPath, Metadata: meta, ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled"}
	require.NoError(t, db.Create(old).Error)
	require.NoError(t, db.Create(&types.Chunk{ID: "old-chunk", TenantID: 1, KnowledgeID: old.ID, KnowledgeBaseID: old.KnowledgeBaseID, Content: "retainedcanary", IsEnabled: true, ChunkType: types.ChunkTypeText}).Error)
	snapshot := func() string {
		t.Helper()
		var s string
		require.NoError(t, db.Raw(`SELECT jsonb_build_object('knowledge',(SELECT to_jsonb(k) FROM knowledges k WHERE id='old-knowledge'),'chunks',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM chunks c WHERE knowledge_id='old-knowledge'),'index',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM office_retained_index i))::text`).Scan(&s).Error)
		return s
	}
	before := snapshot()
	kb := &types.KnowledgeBase{ID: "kb-office", TenantID: 1}
	queue := &createKnowledgeTaskEnqueuerStub{}
	ks := &knowledgeService{repo: repository.NewKnowledgeRepository(db), kbService: &createKnowledgeFileKBServiceStub{kb: kb}, fileSvc: files.NewLocalFileService(root, ""), task: queue}
	svc := &DataSourceService{knowledgeService: ks}
	ctx, err := access.WithKBTaskWrite(newCreateKnowledgeFileContext(), kb, 1)
	require.NoError(t, err)
	ds := &types.DataSource{ID: "office-source", TenantID: 1, KnowledgeBaseID: kb.ID, Type: "confluence", SyncDeletions: true}
	result := &types.SyncResult{}
	for _, name := range []string{"encrypted.docx", "encrypted.xlsx", "encrypted.xlsm", "encrypted.pptx", "encrypted.doc", "encrypted.xls", "encrypted.ppt"} {
		item := &types.FetchedItem{ExternalID: "parent#office", FileName: name, Content: []byte(officeFixture(t, name)), ReplacesSubtree: true}
		require.NoError(t, svc.applyFetchedItem(ctx, ds, item, nil, result))
		require.False(t, item.IsDeleted)
		require.JSONEq(t, before, snapshot())
		got, e := os.ReadFile(oldPath)
		require.NoError(t, e)
		require.Equal(t, oldBytes, got)
	}
	require.Equal(t, 7, result.Failed)
	// A download adapter's content refusal cannot be reinterpreted as a tombstone.
	refused := &types.FetchedItem{ExternalID: "parent#office", IsDeleted: true,
		Metadata: core.FeishuErrorItemMeta(officeguard.NewEncryptedError(), map[string]string{"failure_stage": "fetch_document"})}
	require.NoError(t, svc.applyFetchedItem(ctx, ds, refused, nil, result))
	require.Equal(t, 8, result.Failed)
	require.JSONEq(t, before, snapshot())
	require.Zero(t, result.Deleted)
	require.Zero(t, queue.calls)
	for _, e := range result.Errors {
		require.Equal(t, "office_encrypted", e.Code)
	}
	require.NoError(t, svc.applyFetchedItem(ctx, ds, &types.FetchedItem{ExternalID: "healthy-page", FileName: "healthy.txt", Content: []byte("normal sibling content")}, nil, result))
	require.Equal(t, 1, result.Created)
	require.Equal(t, 1, queue.calls)
	require.NoError(t, svc.applyFetchedItem(ctx, ds, &types.FetchedItem{ExternalID: "parent", FileName: "parent.txt", Content: []byte("parent with present encrypted child"), ReplacesSubtree: true, SubtreeKeep: []string{"parent#office"}}, nil, result))
	require.JSONEq(t, before, snapshot())
	var retrievable int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM office_retained_index WHERE is_enabled AND to_tsvector('simple',content) @@ plainto_tsquery('simple','retainedcanary')`).Scan(&retrievable).Error)
	require.EqualValues(t, 1, retrievable)
	// The same adapter verdict remains document-local on the Feishu streaming
	// path, without calling the source access-loss/quarantine mechanism.
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	feishuDS := *ds
	feishuDS.Type = "feishu"
	refused.IsDeleted = false
	refused.SourceResourceID = "links"
	var local *datasource.DocumentFailureError
	require.ErrorAs(t, svc.applyFetchedItem(ctx, &feishuDS, refused, nil, result), &local)
	require.Equal(t, "office_encrypted", local.Code)
	require.JSONEq(t, before, snapshot())
}

func TestOfficePersonalPostgresFirstImportRejectsBeforeManifestOrKnowledge(t *testing.T) {
	f := newMVPFixture(t)
	f.content = []byte(officeFixture(t, "encrypted.docx"))
	m := f.prepare("https://tenant.feishu.cn/file/real")
	payload := f.payload(m.AttemptID)
	processErr := f.p.Process(f.ctx, payload)
	require.True(t, officeguard.IsEncryptedError(processErr), "actual refusal: %v", processErr)
	_, resource, attempt, e := f.p.repo.Load(f.ctx, m.AttemptID)
	require.NoError(t, e)
	require.Equal(t, "needs_attention", attempt.Status)
	require.Equal(t, "office_encrypted", attempt.LastErrorCategory)
	require.Empty(t, resource.ActiveVersionID)
	var count int64
	require.NoError(t, f.db.Model(&types.Knowledge{}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, f.db.Model(&types.Chunk{}).Count(&count).Error)
	require.Zero(t, count)
	// Re-delivery never publishes the failed attempt or downloads it indefinitely.
	downloads := f.downloads.Load()
	_ = f.p.Process(f.ctx, payload)
	require.Equal(t, downloads, f.downloads.Load())
	matches, e := filepath.Glob(filepath.Join(f.p.store.root, "*", "*", "*", "content"))
	require.NoError(t, e)
	require.Empty(t, matches)
}

func TestOfficePersonalPostgresEncryptedUpdatePreservesExistingPolicyAndVersion(t *testing.T) {
	f := newMVPFixture(t)
	m := f.prepare("https://tenant.feishu.cn/file/real")
	require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
	k, _, _ := mvpPublicTask(t, f, m)
	var before string
	require.NoError(t, f.db.Raw("SELECT to_jsonb(k)::text FROM knowledges k WHERE id=?", k.ID).Scan(&before).Error)
	f.revision.Store(2)
	f.content = []byte(officeFixture(t, "encrypted.xlsx"))
	_, err := f.s.Import(f.ctx, "kb", "https://tenant.feishu.cn/file/real")
	require.ErrorIs(t, err, repository.ErrPersonalUpdateDisabled, "Office policy must not unlock disabled personal updates")
	var after string
	require.NoError(t, f.db.Raw("SELECT to_jsonb(k)::text FROM knowledges k WHERE id=?", k.ID).Scan(&after).Error)
	require.JSONEq(t, before, after)
	mvpAssertSingle(t, f, m)
}

func TestOfficeManifestRejectsEncryptedAttachmentsBeforeAnyWrite(t *testing.T) {
	store, e := newPersonalManifestStore(t.TempDir())
	require.NoError(t, e)
	a := &types.FeishuPersonalAttempt{ID: uuid.NewString(), CandidateVersionID: uuid.NewString(), ImportID: uuid.NewString(), Revision: "1"}
	a.ManifestKey = a.ID + "/" + a.CandidateVersionID + "/f1"
	encrypted := []byte(officeFixture(t, "encrypted.docx"))
	_, e = store.put(a, []byte("# Normal parent"), "parent.md", core.ApplicationAttachment{Name: "attachment.docx", Data: encrypted})
	require.True(t, officeguard.IsEncryptedError(e))
	entries, e := os.ReadDir(store.root)
	require.NoError(t, e)
	require.Empty(t, entries)
}
