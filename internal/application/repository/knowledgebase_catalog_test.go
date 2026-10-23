package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestKnowledgeBaseCatalogKeepsRestrictedSourcesVisible(t *testing.T) {
	db := setupKBTestDB(t)
	require.NoError(t, db.Exec(`CREATE TABLE tenants(id INTEGER PRIMARY KEY,status TEXT,deleted_at DATETIME);
CREATE TABLE data_sources(id TEXT,knowledge_base_id TEXT,type TEXT,legacy_access_state TEXT);
CREATE TABLE feishu_legacy_resources(knowledge_base_id TEXT,state TEXT);
CREATE TABLE knowledges(id TEXT,knowledge_base_id TEXT,deleted_at DATETIME);
INSERT INTO tenants(id,status) VALUES(1,'active'),(2,'active');`).Error)
	for _, row := range []struct {
		id, state string
		tenant    uint64
	}{
		{"held", "hold", 1}, {"revoked", "revoked", 1}, {"ready", "active", 1}, {"other", "hold", 2},
	} {
		require.NoError(t, db.Exec(`INSERT INTO knowledge_bases(id,name,tenant_id,embedding_model_id,summary_model_id,generated_profile) VALUES(?,?,?,'embed','summary','{}')`, row.id, row.id, row.tenant).Error)
		require.NoError(t, db.Exec(`INSERT INTO data_sources VALUES(?,?,'feishu',?)`, row.id, row.id, row.state).Error)
	}
	require.NoError(t, db.Exec(`INSERT INTO knowledges(id,knowledge_base_id) VALUES('doc','held')`).Error)
	guarded := types.EnableLegacyReadGuards(db)
	r := &knowledgeBaseRepository{db: guarded}
	ctx := context.Background()
	rows, err := r.ListKnowledgeBasesByTenantID(ctx, 1)
	require.NoError(t, err)
	require.Len(t, rows, 1, "content callers keep the source guard")
	rows, err = r.ListKnowledgeBasesByTenantID(types.WithKnowledgeBaseCatalog(ctx, 1), 1)
	require.NoError(t, err)
	require.Len(t, rows, 3, "catalog includes hold and revoked KB metadata")
	for _, kb := range rows {
		if kb.ID != "ready" {
			require.Nil(t, kb.GeneratedProfile)
		}
	}
	rows, err = r.GetReadKnowledgeBasesByIDs(types.WithKnowledgeBaseCatalog(ctx, 1), []string{"other"})
	require.NoError(t, err)
	require.Empty(t, rows, "catalog fallback cannot cross tenant boundaries")
	_, err = r.GetKnowledgeBaseByID(ctx, "held")
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
	var doc types.Knowledge
	err = guarded.WithContext(types.WithKnowledgeBaseCatalog(ctx, 1)).First(&doc, "id=?", "doc").Error
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted, "catalog context must not authorize document content")
}
