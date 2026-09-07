package database

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPostgresKnowledgePortalMigrationUpAndDown(t *testing.T) {
	root := sqliteRepoRoot(t)
	up, err := os.ReadFile(filepath.Join(root, "migrations", "versioned", "000093_knowledge_portal.up.sql"))
	require.NoError(t, err)
	down, err := os.ReadFile(filepath.Join(root, "migrations", "versioned", "000093_knowledge_portal.down.sql"))
	require.NoError(t, err)

	upSQL := strings.ToLower(string(up))
	for _, fragment := range []string{
		"create table tenant_portal_configs",
		"create table tenant_portal_stages",
		"create table tenant_access_requests",
		"on delete cascade",
		"on delete set null",
		"where status = 'pending'",
		"check (source = 'portal')",
		"check (requested_role = 'viewer')",
		"category not in ('concept_market'",
		"stage_key       varchar(64) not null",
	} {
		require.Contains(t, upSQL, fragment)
	}
	require.NotContains(t, upSQL, "check (stage_key", "stage catalog validation belongs in the application layer")
	downSQL := strings.ToLower(string(down))
	for _, fragment := range []string{
		"drop table if exists tenant_portal_stages",
		"drop table if exists tenant_access_requests",
		"drop table if exists tenant_portal_configs",
	} {
		require.Contains(t, downSQL, fragment)
	}
	require.Less(t,
		strings.Index(downSQL, "drop table if exists tenant_portal_stages"),
		strings.Index(downSQL, "drop table if exists tenant_portal_configs"),
		"dependent stages table must be removed before portal configs",
	)
}
