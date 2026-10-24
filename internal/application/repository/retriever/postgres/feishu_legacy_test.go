package postgres

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDocumentDeferralCandidateAccessSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	legacyCandidateAccessSuite(t, db)
}

func TestDocumentDeferralCandidateAccessPostgres(t *testing.T) {
	db, err := gorm.Open(postgres.Open(feishulegacy.PrivateDSN(t)), &gorm.Config{})
	require.NoError(t, err)
	legacyCandidateAccessSuite(t, db)
}

func legacyCandidateAccessSuite(t *testing.T, db *gorm.DB) {
	t.Helper()
	conn, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	for _, query := range []string{
		`CREATE TABLE embeddings(knowledge_base_id text)`,
		`CREATE TABLE knowledge_bases(id text,tenant_id bigint)`,
		`CREATE TABLE knowledges(id text,deleted_at timestamp)`,
		`CREATE TABLE data_sources(knowledge_base_id text,legacy_access_state text)`,
		`CREATE TABLE feishu_legacy_resources(knowledge_base_id text,knowledge_id text,state text)`,
		`INSERT INTO embeddings VALUES('kb')`,
		`INSERT INTO knowledge_bases VALUES('kb',1)`,
		`INSERT INTO knowledges VALUES('doc',NULL)`,
		`INSERT INTO data_sources VALUES('kb','hold')`,
	} {
		require.NoError(t, db.Exec(query).Error)
	}
	caller := types.Caller{TenantID: 1, UserID: "member", Role: types.TenantRoleViewer}
	principal := types.Principal{Type: types.PrincipalWebUser, ID: "member"}
	web := types.WithPrincipal(types.WithCaller(context.Background(), caller), principal)
	allowed := func(ctx context.Context) bool {
		var count int64
		require.NoError(t, db.Table("embeddings").Where(legacySourceCandidatePredicate(ctx)).Count(&count).Error)
		return count == 1
	}
	// The candidate query must honor the same browser rights as document reads,
	// without granting those rights to MCP, API keys or a different workspace.
	require.True(t, allowed(web), "source sync hold must not hide existing browser search results")
	require.False(t, allowed(context.Background()))
	key := types.WithTenantAPIKeyScope(web, types.TenantAPIKeyScope{KeyID: 1, ScopeType: types.APIKeyScopeUserMCP})
	require.False(t, allowed(key))
	foreign := caller
	foreign.TenantID = 2
	require.False(t, allowed(types.WithCaller(web, foreign)))
	wrongPrincipal := types.WithPrincipal(web, types.Principal{Type: types.PrincipalWebUser, ID: "other"})
	require.False(t, allowed(wrongPrincipal))
	require.NoError(t, db.Exec(`UPDATE data_sources SET legacy_access_state='revoked'`).Error)
	require.False(t, allowed(web), "revocation remains authoritative")
	require.NoError(t, db.Exec(`UPDATE data_sources SET legacy_access_state='hold'`).Error)
	require.NoError(t, db.Exec(`INSERT INTO feishu_legacy_resources VALUES('kb','unimported','hold')`).Error)
	require.True(t, allowed(web), "an unpublished import must not hide stored browser knowledge")
	require.False(t, allowed(context.Background()))
	require.NoError(t, db.Exec(`UPDATE feishu_legacy_resources SET knowledge_id='doc'`).Error)
	require.False(t, allowed(web), "document quarantine remains authoritative")
}
