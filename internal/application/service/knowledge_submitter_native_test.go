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
	f := prepareWikiSelectionFixture(t)
	db := f.DB
	addWikiSelectionDocument(t, db, "audit-upload", "web", "completed", "enabled")
	addWikiSelectionDocument(t, db, "system-upload", "gitlab", "completed", "enabled")
	addWikiSelectionDocument(t, db, "unknown-upload", "web", "completed", "enabled")
	require.NoError(t, db.Create(&types.FeishuPersonalImport{
		ID: "submitter-import", UserID: "A", KBID: "public-two", KnowledgeID: "public-two-doc",
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
