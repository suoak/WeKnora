package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPortalRepositorySearchesMetadataOnlyAndReturnsSafeProjection(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:portal-security?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	for _, statement := range []string{
		`CREATE TABLE tenants (id INTEGER PRIMARY KEY, name TEXT, status TEXT, deleted_at DATETIME, created_at DATETIME)`,
		`CREATE TABLE tenant_portal_configs (tenant_id INTEGER PRIMARY KEY, status TEXT, display_name TEXT,
			description TEXT, category TEXT, responsible_team TEXT, contact TEXT, featured BOOLEAN,
			display_order INTEGER, allow_access_request BOOLEAN, interaction_organization_id TEXT)`,
		`CREATE TABLE tenant_portal_stages (tenant_id INTEGER, stage_key TEXT, display_order INTEGER, created_at DATETIME)`,
		`CREATE TABLE tenant_members (id TEXT, tenant_id INTEGER, user_id TEXT, role TEXT, status TEXT, deleted_at DATETIME, joined_at DATETIME)`,
		`CREATE TABLE tenant_access_requests (id TEXT, tenant_id INTEGER, applicant_user_id TEXT, status TEXT)`,
		`CREATE TABLE organizations (id TEXT PRIMARY KEY, deleted_at DATETIME)`,
		`CREATE TABLE organization_tenant_members (organization_id TEXT, tenant_id INTEGER)`,
		`CREATE TABLE knowledge_bases (id TEXT, tenant_id INTEGER, name TEXT, is_temporary BOOLEAN, deleted_at DATETIME)`,
		`CREATE TABLE knowledges (id TEXT, tenant_id INTEGER, knowledge_base_id TEXT, title TEXT, deleted_at DATETIME)`,
		`INSERT INTO tenants(id, name, status) VALUES (1, 'private-tenant-name', 'active'), (2, 'deleted', 'active'), (3, 'inactive', 'inactive'), (99, 'active-context', 'active')`,
		`INSERT INTO tenant_portal_configs VALUES
			(1, 'published', 'Hardware Hub', 'Reusable platform knowledge', 'hardware', 'Platform', 'team@example.com', 1, 10, 1, 'org-1'),
			(2, 'published', 'Deleted Hub', '', 'hardware', '', '', 0, 0, 1, NULL),
			(3, 'published', 'Inactive Hub', '', 'hardware', '', '', 0, 0, 1, NULL)`,
		`UPDATE tenants SET deleted_at = CURRENT_TIMESTAMP WHERE id = 2`,
		`INSERT INTO tenant_portal_stages VALUES (1, 'architecture', 0, CURRENT_TIMESTAMP), (1, 'lmt', 1, CURRENT_TIMESTAMP), (1, 'retired_legacy', 2, CURRENT_TIMESTAMP)`,
		`INSERT INTO knowledge_bases VALUES
			('kb-secret', 1, 'classified quantum roadmap', 0, NULL),
			('kb-deleted', 1, 'deleted knowledge base', 0, CURRENT_TIMESTAMP),
			('kb-temporary', 1, 'temporary knowledge base', 1, NULL)`,
		`INSERT INTO knowledges VALUES
			('file-secret', 1, 'kb-secret', 'confidential launch plan', NULL),
			('file-deleted', 1, 'kb-secret', 'deleted file', CURRENT_TIMESTAMP),
			('file-hidden-kb', 1, 'kb-deleted', 'file in deleted kb', NULL),
			('file-temporary', 1, 'kb-temporary', 'temporary file', NULL)`,
		`INSERT INTO organizations VALUES ('org-1', NULL)`,
		`INSERT INTO organization_tenant_members VALUES ('org-1', 99)`,
		`INSERT INTO tenant_members(id, tenant_id, user_id, role, status) VALUES ('99', 99, 'tenantless-user', 'viewer', 'active')`,
	} {
		require.NoError(t, db.Exec(statement).Error, statement)
	}

	repo := NewPortalRepository(db)
	ctx := context.Background()
	rows, err := repo.ListPublished(ctx, "tenantless-user", 0, interfaces.PortalListQuery{})
	require.NoError(t, err)
	require.Len(t, rows, 1, "soft-deleted and inactive workspaces must be excluded")
	var retainedConfigCount int64
	require.NoError(t, db.Table("tenant_portal_configs").Where("tenant_id = ?", 2).Count(&retainedConfigCount).Error)
	require.Equal(t, int64(1), retainedConfigCount, "soft delete must retain portal config for audit")
	require.Equal(t, types.PortalAccessNotMember, rows[0].AccessState)
	require.True(t, rows[0].CanRequestAccess)
	require.Equal(t, types.PortalInteractionNone, rows[0].InteractionAction)
	require.Equal(t, int64(1), rows[0].KnowledgeBaseCount)
	require.Equal(t, int64(1), rows[0].FileCount)
	require.Equal(t, []string{"architecture", "lmt"}, rows[0].Stages, "unknown stage associations must not be exposed")

	rows, err = repo.ListPublished(ctx, "tenantless-user", 0, interfaces.PortalListQuery{Query: "classified quantum roadmap"})
	require.NoError(t, err)
	require.Empty(t, rows, "knowledge-base content must never participate in portal search")
	rows, err = repo.ListPublished(ctx, "tenantless-user", 0, interfaces.PortalListQuery{Query: "confidential launch plan"})
	require.NoError(t, err)
	require.Empty(t, rows, "knowledge titles must never participate in portal search")
	rows, err = repo.ListPublished(ctx, "tenantless-user", 0, interfaces.PortalListQuery{Query: "%"})
	require.NoError(t, err)
	require.Empty(t, rows, "search wildcards must be treated as metadata literals")
	rows, err = repo.ListPublished(ctx, "tenantless-user", 99, interfaces.PortalListQuery{Query: "architecture"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, types.PortalInteractionEnter, rows[0].InteractionAction)

	payload, err := json.Marshal(rows[0])
	require.NoError(t, err)
	serialized := strings.ToLower(string(payload))
	for _, forbidden := range portalForbiddenResponseFields() {
		require.NotContains(t, serialized, forbidden)
	}
}

func TestPortalAccessStatePrecedence(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:portal-state?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	for _, statement := range []string{
		`CREATE TABLE tenants (id INTEGER PRIMARY KEY, status TEXT, deleted_at DATETIME)`,
		`CREATE TABLE tenant_portal_configs (tenant_id INTEGER PRIMARY KEY, status TEXT, display_name TEXT,
			description TEXT, category TEXT, responsible_team TEXT, contact TEXT, featured BOOLEAN,
			display_order INTEGER, allow_access_request BOOLEAN, interaction_organization_id TEXT)`,
		`CREATE TABLE tenant_portal_stages (tenant_id INTEGER, stage_key TEXT, display_order INTEGER, created_at DATETIME)`,
		`CREATE TABLE tenant_members (id TEXT, tenant_id INTEGER, user_id TEXT, role TEXT, status TEXT, deleted_at DATETIME)`,
		`CREATE TABLE tenant_access_requests (id TEXT, tenant_id INTEGER, applicant_user_id TEXT, status TEXT)`,
		`CREATE TABLE organizations (id TEXT PRIMARY KEY, deleted_at DATETIME)`,
		`CREATE TABLE organization_tenant_members (organization_id TEXT, tenant_id INTEGER)`,
		`CREATE TABLE knowledge_bases (id TEXT, tenant_id INTEGER, is_temporary BOOLEAN, deleted_at DATETIME)`,
		`CREATE TABLE knowledges (id TEXT, tenant_id INTEGER, knowledge_base_id TEXT, deleted_at DATETIME)`,
		`INSERT INTO tenants(id, status) VALUES (1, 'active'), (2, 'active'), (3, 'active')`,
		`INSERT INTO tenant_portal_configs VALUES
			(1, 'published', 'one', '', '', '', '', 0, 0, 1, NULL),
			(2, 'published', 'two', '', '', '', '', 0, 0, 1, NULL),
			(3, 'published', 'three', '', '', '', '', 0, 0, 1, NULL)`,
		`INSERT INTO tenant_members VALUES
			('1', 1, 'u', 'viewer', 'active', NULL),
			('2', 2, 'u', 'viewer', 'suspended', NULL),
			('3', 3, 'u', 'viewer', 'invited', NULL)`,
		`INSERT INTO tenant_access_requests VALUES ('r1', 1, 'u', 'pending'), ('r2', 2, 'u', 'pending'), ('r3', 3, 'u', 'pending')`,
	} {
		require.NoError(t, db.Exec(statement).Error, statement)
	}
	rows, err := NewPortalRepository(db).ListPublished(context.Background(), "u", 0, interfaces.PortalListQuery{})
	require.NoError(t, err)
	require.Len(t, rows, 3)
	require.Equal(t, types.PortalAccessMember, rows[0].AccessState)
	require.NotNil(t, rows[0].CurrentRole)
	require.Equal(t, types.PortalAccessSuspended, rows[1].AccessState)
	require.Nil(t, rows[1].CurrentRole)
	require.Equal(t, types.PortalAccessPending, rows[2].AccessState)
	for _, row := range rows {
		require.False(t, row.CanRequestAccess)
	}
}

func TestPortalOrganizationOptionsExcludeSoftDeletedAndExposeOnlyIDName(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:portal-org-options?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE organizations (id TEXT PRIMARY KEY, name TEXT, description TEXT, owner_id TEXT, deleted_at DATETIME)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO organizations VALUES ('active', 'Active Org', 'private description', 'owner', NULL), ('deleted', 'Deleted Org', '', 'owner', CURRENT_TIMESTAMP)`).Error)
	rows, err := NewPortalRepository(db).ListOrganizationOptions(context.Background())
	require.NoError(t, err)
	require.Equal(t, []*types.PortalOrganizationOption{{ID: "active", Name: "Active Org"}}, rows)
	payload, err := json.Marshal(rows)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "description")
	require.NotContains(t, string(payload), "owner")
}

func portalForbiddenResponseFields() []string {
	return []string{
		"knowledge_bases", "knowledge_name", "document_count", "document_title", "file_name",
		"preview", "chunk", "agent", "mcp", "datasource", "storage",
		"members", "tenant_config", "api_key", "model_config", "interaction_organization_id",
	}
}
