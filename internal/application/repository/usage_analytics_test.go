package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func usageAnalyticsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.ModelUsageEvent{}, &types.MCPUsageEvent{}, &types.UsageResourceLink{}))
	return db
}

func TestUsageAnalyticsRecordIsIdempotentWithResourceLinks(t *testing.T) {
	db := usageAnalyticsTestDB(t)
	repo := NewUsageAnalyticsRepository(db)
	ctx := context.Background()
	event := &types.ModelUsageEvent{
		EventKey: "message:one", TenantID: 1, PrincipalType: "web_user", Channel: "web",
		Operation: "knowledge_qa_turn", TotalTokens: 12, UsageSource: "provider", Status: "completed",
		OccurredAt: time.Now(), CreatedAt: time.Now(),
	}
	links := []types.UsageResourceLink{{ResourceType: "knowledge_base", ResourceID: "kb-1", ResourceTenantID: 2}}
	inserted, err := repo.RecordModelUsage(ctx, event, links)
	require.NoError(t, err)
	require.True(t, inserted)
	duplicate := *event
	duplicate.ID = 0
	inserted, err = repo.RecordModelUsage(ctx, &duplicate, links)
	require.NoError(t, err)
	require.False(t, inserted)
	var events, resourceLinks int64
	require.NoError(t, db.Model(&types.ModelUsageEvent{}).Count(&events).Error)
	require.NoError(t, db.Model(&types.UsageResourceLink{}).Count(&resourceLinks).Error)
	require.Equal(t, int64(1), events)
	require.Equal(t, int64(1), resourceLinks)
}

func TestUsageOverviewCountsMCPOnlyTenantInActiveTenantUnion(t *testing.T) {
	db := usageAnalyticsTestDB(t)
	repo := NewUsageAnalyticsRepository(db)
	now := time.Now()
	tenant := uint64(9)
	require.NoError(t, db.Create(&types.MCPUsageEvent{
		EventKey: "mcp:one", CallerTenantID: &tenant, PrincipalType: "api_tenant", PrincipalID: "p",
		Direction: "inbound", ToolName: "search", Success: true, LatencyMs: 8, OccurredAt: now, CreatedAt: now,
	}).Error)
	overview, err := repo.Overview(context.Background(), types.UsageTimeRange{From: now.Add(-time.Hour), To: now.Add(time.Hour)})
	require.NoError(t, err)
	require.Equal(t, int64(1), overview.ActiveTenants)
	require.Equal(t, int64(1), overview.ActivePrincipals)
	require.Equal(t, int64(1), overview.MCPCalls)
}
