package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestHistoricalIndexedLocalPublicationRead(t *testing.T) {
	t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY", "1")
	f, a := exactSourceFixture(t, false)
	id := a.LogicalKnowledgeID
	require.NoError(t, f.db.AutoMigrate(&types.Chunk{}))
	require.NoError(t, f.db.Exec("DROP TABLE embeddings").Error)
	require.NoError(t, f.db.Exec(`CREATE TABLE embeddings(
 knowledge_id text,knowledge_base_id text,chunk_id text,is_enabled boolean,embedding text,dimension int)`).Error)
	require.NoError(
		t,
		f.db.Exec("UPDATE knowledges SET parse_status='completed',enable_status='enabled' WHERE id=?", id).Error,
	)
	require.NoError(t, f.db.Create(&types.Chunk{
		ID: "historical-text", KnowledgeID: id, KnowledgeBaseID: "kb",
		TenantID: 1, Content: "published body", ChunkType: types.ChunkTypeText, IsEnabled: true,
	}).Error)
	require.NoError(
		t,
		f.db.Exec("INSERT INTO embeddings VALUES(?,?,'historical-text',true,'vector',1)", id, "kb").Error,
	)
	var k struct{ FileHash string }
	require.NoError(t, f.db.Table("knowledges").Select("file_hash").Where("id=?", id).Scan(&k).Error)
	payload, err := json.Marshal(map[string]any{
		"payload_version": 1, "knowledge_id": id, "attempt_id": a.ID, "manifest_sha256": a.ManifestHash,
		"file_hash": k.FileHash, "text_chunks": 1, "pg_vector_rows_verified": true, "bm25_document_readback": true,
	})
	require.NoError(t, err)
	require.NoError(t, f.db.Create(&types.TaskPendingOp{
		Scope: personalIndexReceiptScope, ScopeID: id, TenantID: 1, DedupKey: "index-" + a.ID,
		Op: "verified", Payload: payload, EnqueuedAt: time.Now(),
	}).Error)
	viewer := types.WithPrincipal(f.userContext("B"), types.Principal{Type: types.PrincipalWebUser, ID: "B"})
	viewer = types.WithCaller(viewer, types.Caller{UserID: "B", TenantID: 1, Role: types.TenantRoleViewer})
	require.NoError(t, f.db.Exec("UPDATE tenant_members SET role='viewer' WHERE user_id='B'").Error)
	ctx, handled, err := f.p.indexedLocalReadContext(viewer, id)
	require.True(t, handled)
	require.NoError(t, err)
	require.NoError(t, types.CheckLegacySourceRead(f.db.WithContext(ctx), id, "kb"))
	require.Error(t, types.CheckLegacySourceRead(f.db.WithContext(ctx), "sibling", "kb"))
	file, _, err := f.ks.GetKnowledgeFile(ctx, id)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	require.NoError(t, f.db.Exec("UPDATE resources SET content_hash='tampered' WHERE id='exact-file'").Error)
	_, _, err = f.p.indexedLocalReadContext(viewer, id)
	require.Error(t, err, "registry checksum changes cannot expose unchecked bytes")
	require.NoError(t, f.db.Exec("UPDATE resources SET content_hash=? WHERE id='exact-file'", a.ManifestHash).Error)
	require.NoError(t, f.db.Exec("UPDATE task_pending_ops SET op='staging' WHERE scope=? AND scope_id=?",
		personalIndexReceiptScope, id).Error)
	_, _, err = f.p.indexedLocalReadContext(viewer, id)
	require.Error(t, err, "unverified receipts cannot grant body reads")
	require.NoError(t, f.db.Exec("UPDATE task_pending_ops SET op='verified' WHERE scope=? AND scope_id=?",
		personalIndexReceiptScope, id).Error)
	require.NoError(t, f.db.Exec("UPDATE knowledges SET enable_status='disabled' WHERE id=?", id).Error)
	_, _, err = f.p.indexedLocalReadContext(viewer, id)
	require.Error(t, err, "KnowHub disabling remains authoritative")
	require.NoError(t, f.db.Exec("UPDATE knowledges SET enable_status='enabled' WHERE id=?", id).Error)
	require.NoError(t, f.db.Exec("UPDATE chunks SET content='changed' WHERE id='historical-text'").Error)
	_, _, err = f.ks.GetKnowledgeFile(ctx, id)
	require.Error(t, err, "a request must not serve after the local publication changes")
	require.NoError(t, f.db.Exec("UPDATE tenant_members SET status='inactive' WHERE user_id='B'").Error)
	_, _, err = f.p.indexedLocalReadContext(viewer, id)
	require.Error(t, err, "local publication still requires live space authorization")
}
