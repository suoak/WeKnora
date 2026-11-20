package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPublishedSnapshotColdReadUsesEpochsWithoutChunkScan(t *testing.T) {
	f := newEnterpriseFixture(t)
	chunks := 0
	count := func(tx *gorm.DB) {
		if tx.Statement.Table == "chunks" {
			chunks++
		}
	}
	require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register("count_published_chunks", count))
	load := func() *types.EnterpriseReadSnapshot {
		snapshot, err := f.repo.LoadPublishedDocumentReadSnapshot(context.Background(), "A", 1, "public-kb")
		require.NoError(t, err)
		return snapshot
	}
	before := load()
	require.Contains(t, before.Documents[0].CurrentVersion, "published-epoch-v1:")
	require.Equal(t, before, load())
	require.Zero(t, chunks, "the first read must not scan chunk metadata")
	for _, statement := range []string{
		"UPDATE chunks SET content='changed without timestamps' WHERE id='chunk'",
		"UPDATE chunks SET is_enabled=false WHERE id='chunk'",
		"UPDATE chunks SET deleted_at='2026-10-23 00:00:00' WHERE id='chunk'",
		"DELETE FROM chunks WHERE id='chunk'",
		"UPDATE knowledges SET channel='api' WHERE id='doc'",
	} {
		require.NoError(t, f.db.Exec(statement).Error)
		after := load()
		require.NotEqual(t, before.Documents[0].CurrentVersion, after.Documents[0].CurrentVersion)
		before = after
	}
	require.Zero(t, chunks)
	require.NoError(t, f.db.Exec("DELETE FROM knowledges WHERE id='doc'").Error)
	require.NoError(t, f.db.Exec("INSERT INTO knowledges VALUES('doc',99,'public-kb','file','completed',"+
		"'enabled','api','2026-10-20 00:00:00',NULL)").Error)
	require.NotEqual(t, before.Documents[0].CurrentVersion, load().Documents[0].CurrentVersion)
	require.NoError(t, f.db.Exec("DROP TRIGGER document_epoch_chunks_update").Error)
	_, err := f.repo.LoadPublishedDocumentReadSnapshot(context.Background(), "A", 1, "public-kb")
	require.ErrorContains(t, err, "trigger coverage unavailable")
}

func TestPublishedSnapshotRevalidationKeepsLiveRevocations(t *testing.T) {
	for name, statement := range map[string]string{
		"chunk":      "UPDATE chunks SET content='raw write' WHERE id='chunk'",
		"key":        "UPDATE tenant_api_keys SET revoked_at='2026-10-23 00:00:00' WHERE id=1",
		"capability": "UPDATE tenant_api_keys SET capabilities='[\"retrieve\"]' WHERE id=1",
		"user":       "UPDATE users SET is_active=false WHERE id='A'",
		"portal":     "UPDATE tenant_portal_configs SET status='draft' WHERE tenant_id=99",
		"policy": "INSERT INTO knowledge_access_policies(knowledge_id,state,policy,content_class," +
			"source_acl_state,revision,updated_by) VALUES('doc','pending_review','deny','ordinary','local',1,'owner')",
	} {
		t.Run(name, func(t *testing.T) {
			f := newEnterpriseFixture(t)
			reader := access.NewEnterprisePublicReader(access.EnterprisePublicOptions{
				Enabled: true, PortalPublishedDefault: true,
			}, f.repo, f.employees, NewChunkRepository(f.db))
			ctx := enterpriseUserContext("A", 1, true)
			grant, err := reader.Authorize(ctx, "public-kb")
			require.NoError(t, err)
			require.NoError(t, reader.Revalidate(ctx, grant))
			require.NoError(t, f.db.Exec(statement).Error)
			require.Error(t, reader.Revalidate(ctx, grant))
		})
	}
}

func TestPublishedSnapshotKeepsPrivateSourceOwnerGuard(t *testing.T) {
	f := newEnterpriseFixture(t)
	require.NoError(t, f.db.Exec("CREATE TABLE feishu_private_kbs(kb_id TEXT PRIMARY KEY,owner_id TEXT)").Error)
	require.NoError(t, f.db.Exec("INSERT INTO feishu_private_kbs VALUES('public-kb','B')").Error)
	f.repo.db = types.EnablePrivateKBGuards(f.db)
	_, err := f.repo.LoadPublishedDocumentReadSnapshot(enterpriseUserContext("A", 1, true), "A", 1, "public-kb")
	require.ErrorIs(t, err, types.ErrPrivateKnowledgeBase)
}

func TestPublishedSnapshotLeavesCanonicalApprovalVersionsUnchanged(t *testing.T) {
	f := newEnterpriseFixture(t)
	f.approveClean(t)
	ctx := enterpriseUserContext("A", 1, true)
	grant, err := f.reader.Authorize(ctx, "public-kb")
	require.NoError(t, err)
	published, err := f.repo.LoadPublishedDocumentReadSnapshot(ctx, "A", 1, "public-kb")
	require.NoError(t, err)
	canonical, err := f.repo.LoadEnterpriseReadSnapshot(ctx, "A", 1, "public-kb")
	require.NoError(t, err)
	require.NotEqual(t, canonical.Documents[0].CurrentVersion, published.Documents[0].CurrentVersion)
	require.Equal(t, canonical.Documents[0].ResourceVersion, canonical.Documents[0].CurrentVersion)
	require.NoError(t, f.reader.Revalidate(ctx, grant))
}
