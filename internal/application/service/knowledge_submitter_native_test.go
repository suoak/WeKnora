//go:build anydoc && personal_exact_live

package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestKnowledgeSubmitterProjectionUsesOriginalEvidenceWithoutChangingDocuments(t *testing.T) {
	t.Setenv("WIKI_SELECTION_NATIVE_TEST", "true")
	f := prepareWikiSelectionFixture(t)
	db := f.DB
	addWikiSelectionDocument(t, db, "audit-upload", "web", "completed", "enabled")
	addWikiSelectionDocument(t, db, "system-upload", "gitlab", "completed", "enabled")
	addWikiSelectionDocument(t, db, "unknown-upload", "web", "completed", "enabled")
	require.NoError(t, db.Create(&types.FeishuPersonalImport{
		ID: "submitter-import", UserID: "A", AppID: "cli_test", OpenID: "open-A",
		KBID: "public-two", KnowledgeID: "public-two-doc",
	}).Error)
	require.NoError(t, db.Create(&types.AuditLog{
		TenantID: 1, ActorUserID: "B", Action: types.AuditActionKnowledgeCreated,
		ScopeType: "knowledge_base", ScopeID: "public-two", TargetType: "knowledge",
		TargetID: "audit-upload", Outcome: types.AuditOutcomeSuccess,
	}).Error)
	require.NoError(t, db.Exec(`INSERT INTO data_sources(id,tenant_id,knowledge_base_id,name,type,
 status,legacy_access_state,config) VALUES('submitter-source',1,'public-two','synthetic','gitlab',
 'active','unmanaged','{}')`).Error)
	require.NoError(t, db.Exec(`UPDATE knowledges SET metadata='{"datasource_id":"submitter-source"}'
 WHERE id='system-upload'`).Error)
	var before string
	require.NoError(t, db.Raw(`SELECT md5(string_agg(to_jsonb(k)::text,'' ORDER BY id))
 FROM knowledges k WHERE knowledge_base_id='public-two'`).Scan(&before).Error)
	reader := repository.NewKnowledgeRepository(db).(interface {
		ReadKnowledgeSubmitters(context.Context, uint64, string, []string) (map[string]*types.KnowledgeSubmitter, error)
	})
	ids := []string{"public-two-doc", "audit-upload", "system-upload", "unknown-upload"}
	rows, err := reader.ReadKnowledgeSubmitters(context.Background(), 1, "public-two", ids)
	require.NoError(t, err)
	require.Equal(t, "A", rows["public-two-doc"].UserID)
	require.Equal(t, "A", rows["public-two-doc"].Name)
	require.Equal(t, "B", rows["audit-upload"].UserID)
	require.Equal(t, "system_sync", rows["system-upload"].Kind)
	require.Nil(t, rows["unknown-upload"], "the current viewer or KB owner is not an attribution fallback")
	wrong, err := reader.ReadKnowledgeSubmitters(context.Background(), 2, "public-two", ids)
	require.NoError(t, err)
	require.Empty(t, wrong)
	var after string
	require.NoError(t, db.Raw(`SELECT md5(string_agg(to_jsonb(k)::text,'' ORDER BY id))
 FROM knowledges k WHERE knowledge_base_id='public-two'`).Scan(&after).Error)
	require.Equal(t, before, after, "historical publication rows must stay byte-identical")
	// Conflicting creation evidence must not choose an arbitrary person.
	require.NoError(t, db.Create(&types.AuditLog{
		TenantID: 1, ActorUserID: "A", Action: types.AuditActionKnowledgeCreated,
		ScopeType: "knowledge_base", ScopeID: "public-two", TargetType: "knowledge",
		TargetID: "audit-upload", Outcome: types.AuditOutcomeSuccess,
	}).Error)
	rows, err = reader.ReadKnowledgeSubmitters(context.Background(), 1, "public-two", ids)
	require.NoError(t, err)
	require.Nil(t, rows["audit-upload"])
}

func TestKnowledgeSubmitterRetainsOriginalCreatorAfterMoveAndSourceDeletion(t *testing.T) {
	t.Setenv("WIKI_SELECTION_NATIVE_TEST", "true")
	f := prepareWikiSelectionFixture(t)
	db := f.DB
	for _, id := range []string{"moved-upload", "foreign-origin", "deleted-source", "wrong-source-tenant"} {
		addWikiSelectionDocument(t, db, id, "web", "completed", "enabled")
	}
	for _, origin := range []*types.KnowledgeBase{
		{ID: "old-same-tenant", TenantID: 1, Name: "Previous library", Type: "document"},
		{ID: "old-other-tenant", TenantID: 2, Name: "Foreign library", Type: "document"},
	} {
		require.NoError(t, db.Create(origin).Error)
	}
	for doc, origin := range map[string]string{
		"moved-upload": "old-same-tenant", "foreign-origin": "old-other-tenant",
	} {
		require.NoError(t, db.Create(&types.AuditLog{
			TenantID: 1, ActorUserID: "B", Action: types.AuditActionKnowledgeCreated,
			ScopeType: "knowledge_base", ScopeID: origin, TargetType: "knowledge",
			TargetID: doc, Outcome: types.AuditOutcomeAccepted,
		}).Error)
	}
	require.NoError(t, db.Exec(`UPDATE knowledge_bases SET deleted_at=CURRENT_TIMESTAMP
 WHERE id='old-same-tenant'`).Error)
	for _, source := range []struct {
		ID     string
		Tenant uint64
		Doc    string
	}{{"old-feed", 1, "deleted-source"}, {"foreign-feed", 2, "wrong-source-tenant"}} {
		require.NoError(t, db.Exec(`INSERT INTO data_sources(id,tenant_id,knowledge_base_id,name,type,
 status,legacy_access_state,config,deleted_at) VALUES(?,?,'public-two','historical','feishu_drive',
 'error','unmanaged','{}',CURRENT_TIMESTAMP)`, source.ID, source.Tenant).Error)
		require.NoError(t, db.Exec(`UPDATE knowledges SET channel='feishu_drive',
 metadata=jsonb_build_object('datasource_id',?::text) WHERE id=?`, source.ID, source.Doc).Error)
	}
	var before string
	require.NoError(t, db.Raw(`SELECT md5(string_agg(to_jsonb(k)::text,'' ORDER BY id))
 FROM knowledges k WHERE knowledge_base_id='public-two'`).Scan(&before).Error)
	reader := repository.NewKnowledgeRepository(db).(interface {
		ReadKnowledgeSubmitters(context.Context, uint64, string, []string) (map[string]*types.KnowledgeSubmitter, error)
	})
	ids := []string{"moved-upload", "foreign-origin", "deleted-source", "wrong-source-tenant"}
	rows, err := reader.ReadKnowledgeSubmitters(context.Background(), 1, "public-two", ids)
	require.NoError(t, err)
	require.Equal(t, "B", rows["moved-upload"].UserID, "moving a document preserves its original creator")
	require.Equal(t, "system_sync", rows["deleted-source"].Kind, "source deletion does not erase attribution")
	require.Nil(t, rows["foreign-origin"], "a foreign KB cannot attest the creation in this tenant")
	require.Nil(t, rows["wrong-source-tenant"], "a foreign source cannot provide historical attribution")
	var after string
	require.NoError(t, db.Raw(`SELECT md5(string_agg(to_jsonb(k)::text,'' ORDER BY id))
 FROM knowledges k WHERE knowledge_base_id='public-two'`).Scan(&after).Error)
	require.Equal(t, before, after)
	// A second, conflicting creation event remains ambiguous after a move.
	require.NoError(t, db.Create(&types.AuditLog{
		TenantID: 1, ActorUserID: "A", Action: types.AuditActionKnowledgeCreated,
		ScopeType: "knowledge_base", ScopeID: "public-two", TargetType: "knowledge",
		TargetID: "moved-upload", Outcome: types.AuditOutcomeSuccess,
	}).Error)
	rows, err = reader.ReadKnowledgeSubmitters(context.Background(), 1, "public-two", ids)
	require.NoError(t, err)
	require.Nil(t, rows["moved-upload"], "conflicting original and current evidence must not pick a person")
}
