package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestLocalSourceContentUsesPlatformPermissions(t *testing.T) {
	db := setupKBTestDB(t)
	require.NoError(t, db.Exec(`CREATE TABLE tenants(id INTEGER PRIMARY KEY,status TEXT,deleted_at DATETIME);
CREATE TABLE data_sources(tenant_id INTEGER,knowledge_base_id TEXT,type TEXT,legacy_access_state TEXT);
CREATE TABLE feishu_legacy_resources(tenant_id INTEGER,knowledge_base_id TEXT,state TEXT,knowledge_id TEXT);
CREATE TABLE feishu_private_kbs(kb_id TEXT,owner_id TEXT);
CREATE TABLE knowledges(id TEXT,knowledge_base_id TEXT,channel TEXT,deleted_at DATETIME);
INSERT INTO tenants VALUES(1,'active',NULL);
INSERT INTO knowledge_bases(id,name,tenant_id,embedding_model_id,summary_model_id,generated_profile)
VALUES('local','local',1,'embed','summary','{}');
INSERT INTO data_sources VALUES(1,'local','feishu','revoked');
INSERT INTO feishu_legacy_resources VALUES(1,'local','hold','doc');
INSERT INTO knowledges VALUES('doc','local','feishu_personal',NULL);`).Error)
	guarded := types.EnablePrivateKBGuards(types.EnableLegacyReadGuards(db))
	for _, principal := range []string{"browser", "mcp"} {
		t.Run(principal, func(t *testing.T) {
			ctx := types.WithLocalSourceContentRead(types.WithLegacyReadScope(context.Background()))
			if principal == "mcp" {
				ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{KeyID: 1, ScopeType: types.APIKeyScopeUserMCP})
			}
			require.NoError(t, types.CheckLegacySourceRead(guarded.WithContext(ctx), "doc", "local"))
			require.NoError(t, types.CheckLegacyReadScope(guarded.WithContext(ctx)))
			require.NoError(t, legacyTenantModelGate(ctx, guarded, 1))
			require.NoError(t, db.Exec("INSERT INTO feishu_private_kbs VALUES('local','owner')").Error)
			require.ErrorIs(t, types.CheckLegacySourceRead(guarded.WithContext(ctx), "doc", "local"), types.ErrPrivateKnowledgeBase)
			require.ErrorIs(t, types.CheckLegacyReadScope(guarded.WithContext(ctx)), types.ErrPrivateKnowledgeBase)
			require.NoError(t, db.Exec("DELETE FROM feishu_private_kbs").Error)
			require.NoError(t, db.Exec("UPDATE tenants SET status='disabled'").Error)
			require.ErrorIs(t, types.CheckLegacyReadScope(guarded.WithContext(ctx)), types.ErrLegacySourceRestricted)
			require.NoError(t, db.Exec("UPDATE tenants SET status='active'").Error)
		})
	}
}
