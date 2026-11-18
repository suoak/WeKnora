package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestModelGateDoesNotScanUnrelatedFeishuSources(t *testing.T) {
	db := setupKBTestDB(t)
	require.NoError(t, db.Exec(`CREATE TABLE tenants(id INTEGER PRIMARY KEY,status TEXT,deleted_at DATETIME);
CREATE TABLE data_sources(tenant_id INTEGER,knowledge_base_id TEXT,type TEXT,legacy_access_state TEXT);
CREATE TABLE feishu_legacy_resources(tenant_id INTEGER,knowledge_base_id TEXT,state TEXT,knowledge_id TEXT);
INSERT INTO tenants VALUES(1,'active',NULL);
INSERT INTO knowledge_bases(id,name,tenant_id,embedding_model_id,summary_model_id,generated_profile)
VALUES('held','held',1,'embed','summary','{}');
INSERT INTO data_sources VALUES(1,'held','feishu','revoked');
INSERT INTO feishu_legacy_resources VALUES(1,'held','hold','doc');`).Error)
	guarded := types.EnableLegacyReadGuards(db)
	ctx := types.WithLegacyReadScope(context.Background())
	require.NoError(t, legacyTenantModelGate(ctx, guarded, 1),
		"first agent turn and title generation have no source inputs")
	types.RecordLegacyCandidateRead(ctx, "held")
	require.ErrorIs(t, legacyTenantModelGate(ctx, guarded, 1), types.ErrLegacySourceRestricted,
		"external readers still recheck the actual inputs")
	web := types.WithPrincipal(types.WithCaller(types.WithLocalSourceContentRead(ctx), types.Caller{
		TenantID: 1, UserID: "member", Role: types.TenantRoleViewer,
	}), types.Principal{Type: types.PrincipalWebUser, ID: "member"})
	require.NoError(t, legacyTenantModelGate(web, guarded, 1),
		"stored workspace inputs use local KB permissions")
	mcp := types.WithTenantAPIKeyScope(types.WithLocalSourceContentRead(ctx), types.TenantAPIKeyScope{
		KeyID: 1, ScopeType: types.APIKeyScopeUserMCP,
	})
	require.NoError(t, legacyTenantModelGate(mcp, guarded, 1),
		"authorized MCP inputs use the same platform content policy")
}
