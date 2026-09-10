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
