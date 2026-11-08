package repository

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestMCPUsageCoverageAggregationMatchesLedger(t *testing.T) {
	db := usageAnalyticsTestDB(t)
	repo := NewUsageAnalyticsRepository(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	tenant, key := uint64(100), uint64(42)
	for i, entry := range []string{"go_personal", "go_endpoint", "python"} {
		event := &types.MCPUsageEvent{EventKey: entry, EntryPoint: entry, CallerTenantID: &tenant, PrincipalType: types.PrincipalWebUser, PrincipalID: "employee", APIKeyID: &key, Direction: "inbound", ToolName: "search_knowledge", Success: i != 2, ErrorCode: "", LatencyMs: 10, OccurredAt: now, CreatedAt: now}
		if i == 2 {
			event.ErrorCode = "permission_denied"
		}
		links := []types.UsageResourceLink{}
		for _, id := range []string{"a", "b", "c", "d", "e"} {
			links = append(links, types.UsageResourceLink{ResourceType: "knowledge_base_target", ResourceID: id, ResourceTenantID: 200})
		}
		for _, id := range []string{"a", "b"} {
			links = append(links, types.UsageResourceLink{ResourceType: "knowledge_base_hit", ResourceID: id, ResourceTenantID: 200}, types.UsageResourceLink{ResourceType: "knowledge_base", ResourceID: id, ResourceTenantID: 200})
		}
		inserted, err := repo.RecordMCPUsage(ctx, event, links)
		require.NoError(t, err)
		require.True(t, inserted)
		duplicate := *event
		duplicate.ID = 0
		inserted, err = repo.RecordMCPUsage(ctx, &duplicate, links)
		require.NoError(t, err)
		require.False(t, inserted)
	}
	q := types.UsageTimeRange{From: now.Add(-time.Hour), To: now.Add(time.Hour), Direction: "inbound", Page: 1, PageSize: 100}
	rows, total, err := repo.MCP(ctx, q)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Equal(t, int64(3), rows[0].Calls)
	require.Equal(t, int64(2), rows[0].SuccessfulCalls)
	q.GroupBy = "summary"
	rows, total, err = repo.MCP(ctx, q)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Equal(t, int64(3), rows[0].Calls)
	require.Equal(t, int64(1), rows[0].ActiveUsers)
	require.Equal(t, int64(1), rows[0].ActiveKeys)
	q.GroupBy = "entry_point"
	rows, total, err = repo.MCP(ctx, q)
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	for _, row := range rows {
		require.Equal(t, int64(1), row.Calls)
		require.Equal(t, int64(1), row.ActiveUsers)
		require.Equal(t, int64(1), row.ActiveKeys)
	}
	q.EntryPoint = "go_personal"
	q.GroupBy = "resource"
	rows, total, err = repo.MCP(ctx, q)
	require.NoError(t, err)
	require.Equal(t, int64(9), total)
	counts := map[string]int{}
	for _, row := range rows {
		counts[row.ResourceType]++
		require.Equal(t, uint64(200), row.ResourceTenantID)
	}
	require.Equal(t, 5, counts["knowledge_base_target"])
	require.Equal(t, 2, counts["knowledge_base_hit"])
	q.GroupBy = "event"
	rows, total, err = repo.MCP(ctx, q)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Equal(t, "employee", rows[0].PrincipalID)
	require.Equal(t, key, *rows[0].APIKeyID)
}

func TestMCPUsagePostgresMigrationAndReplay(t *testing.T) {
	dsn := os.Getenv("MCP_USAGE_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("MCP_USAGE_POSTGRES_DSN not configured")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	schema := "mcp_usage_" + uuid.NewString()[:8]
	require.NoError(t, tx.Exec(`CREATE SCHEMA "`+schema+`"`).Error)
	require.NoError(t, tx.Exec(`SET LOCAL search_path TO "`+schema+`"`).Error)
	require.NoError(t, tx.AutoMigrate(&types.SystemSetting{}))
	for _, file := range []string{"000095_usage_analytics.up.sql", "000096_usage_analytics_phase2.up.sql"} {
		sql, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "versioned", file))
		require.NoError(t, err)
		require.NoError(t, tx.Exec(string(sql)).Error)
	}
	now := time.Now().UTC()
	repo := NewUsageAnalyticsRepository(tx)
	legacy := &types.MCPUsageEvent{EventKey: "legacy", Direction: "inbound", ToolName: "read_document", PrincipalType: "system/unknown", OccurredAt: now, CreatedAt: now}
	// The old schema has no new metadata columns. This models a pre-upgrade row.
	require.NoError(t, tx.Omit("entry_point", "endpoint_id", "client_source").Create(legacy).Error)
	sql, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "versioned", "003046_mcp_usage_coverage.up.sql"))
	require.NoError(t, err)
	require.NoError(t, tx.Exec(string(sql)).Error)
	key, tenant := uint64(42), uint64(100)
	event := &types.MCPUsageEvent{EventKey: "real-final", EntryPoint: "go_personal", PrincipalType: "web_user", PrincipalID: "employee", APIKeyID: &key, CallerTenantID: &tenant, Direction: "inbound", ToolName: "search_knowledge", Success: true, OccurredAt: now, CreatedAt: now}
	links := []types.UsageResourceLink{{ResourceType: "knowledge_base_target", ResourceID: "kb-1", ResourceTenantID: 200}, {ResourceType: "knowledge_base_hit", ResourceID: "kb-1", ResourceTenantID: 200}, {ResourceType: "knowledge_base", ResourceID: "kb-1", ResourceTenantID: 200}}
	inserted, err := repo.RecordMCPUsage(context.Background(), event, links)
	require.NoError(t, err)
	require.True(t, inserted)
	duplicate := *event
	duplicate.ID = 0
	inserted, err = repo.RecordMCPUsage(context.Background(), &duplicate, links)
	require.NoError(t, err)
	require.False(t, inserted)
	q := types.UsageTimeRange{From: now.Add(-time.Hour), To: now.Add(time.Hour), EntryPoint: "go_personal", GroupBy: "entry_point", Page: 1, PageSize: 100}
	rows, total, err := repo.MCP(context.Background(), q)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Equal(t, int64(1), rows[0].Calls)
	require.Equal(t, int64(1), rows[0].ActiveUsers)
	// The rollback intentionally retains all ledger rows and new dimensions.
	down, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "versioned", "003046_mcp_usage_coverage.down.sql"))
	require.NoError(t, err)
	require.NoError(t, tx.Exec(string(down)).Error)
	var count int64
	require.NoError(t, tx.Model(&types.MCPUsageEvent{}).Count(&count).Error)
	require.Equal(t, int64(2), count)
}
