package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestKnowledgeBaseCatalogKeepsRestrictedSourcesVisible(t *testing.T) {
	db := setupKBTestDB(t)
	require.NoError(t, db.Exec(`CREATE TABLE tenants(id INTEGER PRIMARY KEY,status TEXT,deleted_at DATETIME);
CREATE TABLE data_sources(id TEXT,knowledge_base_id TEXT,type TEXT,legacy_access_state TEXT,deleted_at DATETIME);
CREATE TABLE feishu_legacy_resources(knowledge_base_id TEXT,state TEXT,knowledge_id TEXT);
CREATE TABLE knowledges(id TEXT,knowledge_base_id TEXT,deleted_at DATETIME,
channel TEXT NOT NULL DEFAULT 'web',metadata JSON NOT NULL DEFAULT '{}');
CREATE TABLE task_pending_ops(scope TEXT,scope_id TEXT);
INSERT INTO tenants(id,status) VALUES(1,'active'),(2,'active');`).Error)
	for _, row := range []struct {
		id, state string
		tenant    uint64
	}{
		{"held", "hold", 1}, {"revoked", "revoked", 1}, {"ready", "active", 1}, {"other", "hold", 2},
	} {
		require.NoError(t, db.Exec(`INSERT INTO knowledge_bases(
id,name,tenant_id,embedding_model_id,summary_model_id,generated_profile)
VALUES(?,?,?,'embed','summary','{}')`, row.id, row.id, row.tenant).Error)
		require.NoError(
			t,
			db.Exec(
				`INSERT INTO data_sources(id,knowledge_base_id,type,legacy_access_state) VALUES(?,?,'feishu',?)`,
				row.id,
				row.id,
				row.state,
			).Error,
		)
	}
	require.NoError(
		t,
		db.Exec(
			`INSERT INTO knowledges(id,knowledge_base_id,metadata) VALUES('doc','held','{"datasource_id":"held"}')`,
		).Error,
	)
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
	require.ErrorIs(
		t,
		err,
		types.ErrLegacySourceRestricted,
		"catalog context must not authorize document content",
	)

	t.Run("workspace-browser-keeps-existing-documents", func(t *testing.T) {
		caller := types.Caller{TenantID: 1, UserID: "member", Role: types.TenantRoleViewer}
		principal := types.Principal{Type: types.PrincipalWebUser, ID: "member"}
		web := types.WithPrincipal(types.WithCaller(types.WithLegacyReadScope(ctx), caller), principal)
		_, err := r.GetKnowledgeBaseByID(web, "held")
		require.NoError(t, err, "a source sync hold does not revoke workspace membership")
		var stored types.Knowledge
		require.NoError(t, guarded.WithContext(web).First(&stored, "id=?", "doc").Error)
		require.NoError(t, types.CheckLegacyReadScope(guarded.WithContext(web)))
		_, err = r.GetKnowledgeBaseByID(web, "other")
		require.ErrorIs(t, err, types.ErrLegacySourceRestricted,
			"other workspace sources remain protected")
		keyScope := types.TenantAPIKeyScope{KeyID: 1, ScopeType: types.APIKeyScopeUserMCP}
		keyed := types.WithTenantAPIKeyScope(web, keyScope)
		_, err = r.GetKnowledgeBaseByID(keyed, "held")
		require.ErrorIs(t, err, types.ErrLegacySourceRestricted,
			"MCP does not inherit browser membership rights")
		require.NoError(
			t,
			db.Exec(`UPDATE data_sources SET legacy_access_state='revoked' WHERE id='held'`).Error,
		)
		_, err = r.GetKnowledgeBaseByID(web, "held")
		require.NoError(t, err, "source revocation cannot remove workspace metadata")
		require.ErrorIs(t, guarded.WithContext(web).First(&stored, "id=?", "doc").Error,
			types.ErrLegacySourceRestricted, "a linked document remains denied")
		require.ErrorIs(
			t,
			types.CheckLegacyReadScope(guarded.WithContext(web)),
			types.ErrLegacySourceRestricted,
			"revocation after read is checked before output",
		)
		require.NoError(
			t,
			db.Exec(`UPDATE data_sources SET legacy_access_state='hold' WHERE id='held'`).Error,
		)
		require.NoError(t, db.Exec(`INSERT INTO feishu_legacy_resources
VALUES('held','hold','not-yet-imported')`).Error)
		_, err = r.GetKnowledgeBaseByID(web, "held")
		require.NoError(t, err, "an unfinished import without stored content must not hide the workspace KB")
		require.NoError(t, guarded.WithContext(web).First(&stored, "id=?", "doc").Error)
		require.NoError(t, types.CheckLegacyReadScope(guarded.WithContext(web)))
		_, err = r.GetKnowledgeBaseByID(ctx, "held")
		require.ErrorIs(t, err, types.ErrLegacySourceRestricted,
			"strict external readers still deny uncertain imports")
		require.NoError(t, db.Exec(`UPDATE feishu_legacy_resources
SET knowledge_id='doc' WHERE knowledge_base_id='held'`).Error)
		_, err = r.GetKnowledgeBaseByID(web, "held")
		require.NoError(t, err, "quarantined documents cannot remove workspace metadata")
		require.ErrorIs(t, guarded.WithContext(web).First(&stored, "id=?", "doc").Error,
			types.ErrLegacySourceRestricted, "document quarantine remains independent of metadata access")
	})
}

func TestKnowledgeBaseCatalogRevokedPrivateSourceFinalResponse(t *testing.T) {
	testKnowledgeBaseCatalogRevokedPrivateSource(t, setupKBTestDB(t))
}

func TestKnowledgeBaseCatalogRevokedPrivateSourcePostgres(t *testing.T) {
	db := syncdb.Open(t, "postgres")
	ddl := strings.ReplaceAll(knowledgeBasesTestDDL, "DATETIME", "TIMESTAMPTZ")
	ddl = strings.ReplaceAll(ddl, "BOOLEAN NOT NULL DEFAULT 0", "BOOLEAN NOT NULL DEFAULT false")
	require.NoError(t, db.Exec(ddl).Error)
	testKnowledgeBaseCatalogRevokedPrivateSource(t, db)
}

func testKnowledgeBaseCatalogRevokedPrivateSource(t *testing.T, db *gorm.DB) {
	t.Helper()
	ddl := `CREATE TABLE tenants(id INTEGER PRIMARY KEY,status TEXT,deleted_at DATETIME);
CREATE TABLE IF NOT EXISTS data_sources(id TEXT,knowledge_base_id TEXT,type TEXT,
legacy_access_state TEXT,deleted_at DATETIME);
CREATE TABLE feishu_legacy_resources(knowledge_base_id TEXT,state TEXT,knowledge_id TEXT);
CREATE TABLE knowledges(id TEXT,knowledge_base_id TEXT,deleted_at DATETIME,
channel TEXT NOT NULL DEFAULT 'web',metadata JSON NOT NULL DEFAULT '{}');
CREATE TABLE task_pending_ops(scope TEXT,scope_id TEXT);
CREATE TABLE feishu_private_kbs(kb_id TEXT PRIMARY KEY,owner_id TEXT);
INSERT INTO tenants(id,status) VALUES(1,'active'),(2,'active');
INSERT INTO knowledge_bases(id,name,tenant_id,embedding_model_id,summary_model_id,generated_profile)
VALUES('private','private',1,'embed','summary','{}'),('healthy','healthy',1,'embed','summary','{}'),
('foreign','foreign',2,'embed','summary','{}');
INSERT INTO feishu_private_kbs VALUES('private','owner'),('foreign','owner');
INSERT INTO data_sources(id,knowledge_base_id,type,legacy_access_state)
VALUES('removed','private','feishu','revoked'),('other','foreign','feishu','revoked');
INSERT INTO knowledges(id,knowledge_base_id,metadata) VALUES('private-doc','private','{"datasource_id":"removed"}');`
	if db.Name() == "postgres" {
		ddl = strings.ReplaceAll(ddl, "DATETIME", "TIMESTAMPTZ")
	}
	require.NoError(t, db.Exec(ddl).Error)
	guarded := types.EnablePrivateKBGuards(types.EnableLegacyReadGuards(db))
	r := &knowledgeBaseRepository{db: guarded}
	owner := func() context.Context {
		ctx := context.WithValue(context.Background(), types.UserIDContextKey, "owner")
		ctx = types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: "owner", Role: types.TenantRoleAdmin})
		ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: "owner"})
		return types.WithLegacyReadScope(ctx)
	}
	web := owner()
	rows, err := r.ListKnowledgeBasesByTenantID(types.WithKnowledgeBaseCatalog(web, 1), 1)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, kb := range rows {
		if kb.ID == "private" {
			require.Nil(t, kb.GeneratedProfile)
		} else {
			require.NotNil(t, kb.GeneratedProfile, "healthy catalog content keeps its existing authorization")
		}
	}
	require.NoError(t, types.CheckLegacyReadScope(guarded.WithContext(web)),
		"revoked private source metadata must not turn the whole list into HTTP 403")
	require.NoError(
		t,
		types.CheckLegacyReadScope(guarded.WithContext(web)),
		"final checking must be repeatable",
	)

	var doc types.Knowledge
	err = guarded.WithContext(types.WithKnowledgeBaseCatalog(owner(), 1)).
		First(&doc, "id=?", "private-doc").
		Error
	require.ErrorIs(
		t,
		err,
		types.ErrLegacySourceRestricted,
		"catalog visibility cannot authorize source content",
	)
	foreign := owner()
	rows, err = r.GetReadKnowledgeBasesByIDs(types.WithKnowledgeBaseCatalog(foreign, 1), []string{"foreign"})
	require.NoError(t, err)
	require.Empty(t, rows)
	require.NoError(t, types.CheckLegacyReadScope(guarded.WithContext(foreign)))

	require.NoError(
		t,
		db.Exec("UPDATE feishu_private_kbs SET owner_id='different' WHERE kb_id='private'").Error,
	)
	require.ErrorIs(t, types.CheckLegacyReadScope(guarded.WithContext(web)), types.ErrPrivateKnowledgeBase,
		"ownership changes between lookup and response remain closed")
	require.NoError(t, db.Exec("UPDATE feishu_private_kbs SET owner_id='owner' WHERE kb_id='private'").Error)
	require.NoError(
		t,
		db.Exec("UPDATE data_sources SET legacy_access_state='active' WHERE id='removed'").Error,
	)
	body := owner()
	require.NoError(t, guarded.WithContext(body).First(&doc, "id=?", "private-doc").Error)
	require.NoError(
		t,
		db.Exec("UPDATE data_sources SET legacy_access_state='revoked' WHERE id='removed'").Error,
	)
	require.ErrorIs(t, types.CheckLegacyReadScope(guarded.WithContext(body)), types.ErrLegacySourceRestricted,
		"real content reads must still fail if their source is revoked before output")
	require.NoError(
		t,
		db.Exec("UPDATE knowledge_bases SET deleted_at=CURRENT_TIMESTAMP WHERE id='private'").Error,
	)
	require.ErrorIs(t, types.CheckLegacyReadScope(guarded.WithContext(web)), types.ErrPrivateKnowledgeBase,
		"deleted metadata cannot be emitted after lookup")
}
