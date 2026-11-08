package database

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPostgresUsageAnalyticsMigrationContract(t *testing.T) {
	root := sqliteRepoRoot(t)
	upPath := filepath.Join(root, "migrations", "versioned", "000095_usage_analytics.up.sql")
	downPath := filepath.Join(root, "migrations", "versioned", "000095_usage_analytics.down.sql")
	upBytes, err := os.ReadFile(upPath)
	require.NoError(t, err)
	downBytes, err := os.ReadFile(downPath)
	require.NoError(t, err)
	up, down := strings.ToLower(string(upBytes)), strings.ToLower(string(downBytes))
	for _, table := range []string{"model_usage_events", "mcp_usage_events", "usage_resource_links"} {
		require.Contains(t, up, "create table if not exists "+table)
		require.Contains(t, down, "drop table if exists "+table)
	}
	require.Contains(t, up, "event_key varchar(255) not null unique")
	require.Contains(t, up, "caller_tenant_id bigint")
	require.NotContains(t, up, "references messages")
	require.NotContains(t, up, "references sessions")
	require.NotContains(t, up, "references tenants")
	require.NotContains(t, up, "cascade")
}

func TestUsageAnalyticsPhase2MigrationContract(t *testing.T) {
	root := sqliteRepoRoot(t)
	for _, path := range []string{
		filepath.Join(root, "migrations", "versioned", "000096_usage_analytics_phase2.up.sql"),
		filepath.Join(root, "migrations", "sqlite", "000017_usage_analytics_phase2.up.sql"),
	} {
		contents, err := os.ReadFile(path)
		require.NoError(t, err)
		sql := strings.ToLower(string(contents))
		require.Contains(t, sql, "mcp_service_id")
		require.Contains(t, sql, "'outbound'")
		require.Contains(t, sql, "idx_model_usage_channel_time")
		require.Contains(t, sql, "idx_mcp_usage_direction_time")
	}
}

func TestMCPUsageSQLiteRollbackRetainsAndRestoresMetadata(t *testing.T) {
	root := sqliteRepoRoot(t)
	db := openSQLiteDB(t, filepath.Join(t.TempDir(), "mcp-rollback.sqlite"))
	_, err := db.Exec(`CREATE TABLE system_settings (
		key TEXT PRIMARY KEY,value TEXT,value_type TEXT,category TEXT,description TEXT,
		created_at TEXT,updated_at TEXT)`)
	require.NoError(t, err)
	apply := func(file string) {
		body, err := os.ReadFile(filepath.Join(root, "migrations", "sqlite", file))
		require.NoError(t, err)
		_, err = db.Exec(string(body))
		require.NoError(t, err, file)
	}
	apply("000016_usage_analytics.up.sql")
	apply("000017_usage_analytics_phase2.up.sql")
	_, err = db.Exec(`INSERT INTO mcp_usage_events
		(event_key,principal_type,principal_id,direction,tool_name,success,occurred_at)
		VALUES ('kept','web_user','employee','inbound','search_knowledge',1,CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	apply("003046_mcp_usage_coverage.up.sql")
	_, err = db.Exec(`UPDATE mcp_usage_events SET entry_point='go_personal',client_source='negotiated'
		WHERE event_key='kept'`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO usage_resource_links(event_kind,event_id,resource_type,resource_id,resource_tenant_id)
		VALUES ('mcp',1,'knowledge_base_target','kb',99)`)
	require.NoError(t, err)
	apply("003046_mcp_usage_coverage.down.sql")
	var count int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM mcp_usage_events").Scan(&count))
	require.Equal(t, 1, count)
	apply("003046_mcp_usage_coverage.up.sql")
	var id int
	var entry, client, principal string
	require.NoError(t, db.QueryRow(`SELECT id,entry_point,client_source,principal_id FROM mcp_usage_events
		WHERE event_key='kept'`).Scan(&id, &entry, &client, &principal))
	require.Equal(t, 1, id)
	require.Equal(t, "go_personal", entry)
	require.Equal(t, "negotiated", client)
	require.Equal(t, "employee", principal)
	require.NoError(t, db.QueryRow("SELECT count(*) FROM usage_resource_links").Scan(&count))
	require.Equal(t, 1, count)
}
