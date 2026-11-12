package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestProtocolCredentialRequiresCurrentIdentityMembershipAndKey(t *testing.T) {
	for _, mutation := range []string{
		"", "UPDATE tenant_api_keys SET revoked_at=CURRENT_TIMESTAMP",
		"UPDATE tenant_api_keys SET expires_at='2000-01-01'",
		"UPDATE tenant_api_keys SET owner_user_id='other'",
		"UPDATE tenant_api_keys SET capabilities='[]'",
		"UPDATE tenant_api_keys SET capabilities='invalid'",
		"UPDATE tenant_api_keys SET full_access=true",
		"UPDATE tenant_api_keys SET scope_type='tenant'",
		"UPDATE tenant_api_keys SET personal_scope_mode='invalid'",
		"UPDATE users SET is_active=false", "UPDATE users SET deleted_at=CURRENT_TIMESTAMP",
		"UPDATE tenant_members SET status='revoked'", "UPDATE tenant_members SET role='invalid'",
		"UPDATE tenant_members SET deleted_at=CURRENT_TIMESTAMP",
		"UPDATE tenants SET status='inactive'", "UPDATE tenants SET deleted_at=CURRENT_TIMESTAMP",
		"UPDATE tenant_api_keys SET personal_scope_mode='fixed'",
	} {
		t.Run(mutation, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = sqlDB.Close() })
			for _, sql := range []string{
				`CREATE TABLE tenant_api_keys (id INTEGER PRIMARY KEY, owner_user_id TEXT, scope_type TEXT, capabilities TEXT, revoked_at DATETIME, expires_at DATETIME, full_access BOOLEAN, personal_scope_mode TEXT)`,
				`CREATE TABLE users (id TEXT, is_active BOOLEAN, deleted_at DATETIME)`,
				`CREATE TABLE tenants (id INTEGER, status TEXT, deleted_at DATETIME)`,
				`CREATE TABLE tenant_members (user_id TEXT, tenant_id INTEGER, status TEXT, role TEXT, deleted_at DATETIME)`,
				`CREATE TABLE api_key_tenant_scopes (id INTEGER PRIMARY KEY, api_key_id INTEGER, tenant_id INTEGER, kb_scope_mode TEXT)`,
				`CREATE TABLE api_key_kb_scopes (id INTEGER PRIMARY KEY, api_key_tenant_scope_id INTEGER, api_key_id INTEGER, tenant_id INTEGER, knowledge_base_id TEXT, source_type TEXT, kb_share_id TEXT)`,
				`INSERT INTO tenant_api_keys VALUES (1,'A','user_mcp','["retrieve"]',NULL,NULL,false,'dynamic_all')`,
				`INSERT INTO users VALUES ('A',true,NULL)`,
				`INSERT INTO tenants VALUES (99,'active',NULL)`,
				`INSERT INTO tenant_members VALUES ('A',99,'active','viewer',NULL)`,
			} {
				require.NoError(t, db.Exec(sql).Error)
			}
			if mutation != "" {
				require.NoError(t, db.Exec(mutation).Error)
			}
			repo := NewEnterpriseAccessRepository(db)
			err = repo.VerifyPortalMCPProtocolCredential(context.Background(), "A", 1, 99)
			if mutation != "" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err, "protocol discovery needs no knowledge/chunk tables or write permission")
			require.Error(t, repo.VerifyPortalMCPProtocolCredential(context.Background(), "other", 1, 99))
			require.Error(t, repo.VerifyPortalMCPProtocolCredential(context.Background(), "A", 1, 100))
			require.NoError(t, db.Exec(`UPDATE tenant_api_keys SET personal_scope_mode='fixed'`).Error)
			require.NoError(t, db.Exec(`INSERT INTO api_key_tenant_scopes VALUES (1,1,99,'selected')`).Error)
			require.NoError(t, repo.VerifyPortalMCPProtocolCredential(context.Background(), "A", 1, 99), "fixed scope can discover schemas without gaining any KB grant")
		})
	}
}
