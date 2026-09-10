package repository

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUsageGovernancePostgresAggregation(t *testing.T) {
	dsn := os.Getenv("USAGE_ANALYTICS_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("USAGE_ANALYTICS_POSTGRES_DSN is not configured")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	tx := db.Begin()
	require.NoError(t, tx.Error)
	t.Cleanup(func() { tx.Rollback() })
	require.NoError(t, tx.Exec(`INSERT INTO tenants (id,name,business,status) VALUES (92001,'Gov A','test','active'),(92002,'Gov B','test','active'),(92003,'Gov Empty','test','active')`).Error)
	require.NoError(t, tx.Exec(`INSERT INTO knowledge_bases (id,name,tenant_id,embedding_model_id,summary_model_id) VALUES ('gov-pg-kb','Shared KB',92002,'',''),('gov-pg-never','Never KB',92003,'','')`).Error)
	now := time.Now().UTC().Truncate(time.Second)
	event := &types.ModelUsageEvent{EventKey: "gov-pg-model", TenantID: 92001, PrincipalType: types.PrincipalWebUser, PrincipalID: "gov-user", Channel: "web", Operation: types.ModelUsageOperationKnowledgeQA, TotalTokens: 10, UsageSource: "provider", Status: "completed", OccurredAt: now.Add(-time.Hour), CreatedAt: now.Add(-time.Hour)}
	require.NoError(t, tx.Create(event).Error)
	require.NoError(t, tx.Create(&types.UsageResourceLink{EventKind: "model", EventID: event.ID, ResourceType: "knowledge_base", ResourceID: "gov-pg-kb", ResourceTenantID: 92002}).Error)
	collecting := now.AddDate(0, 0, -100)
	q := types.UsageTimeRange{From: now.AddDate(0, 0, -30), To: now, Page: 1, PageSize: 20, IncludeInactive: true, CollectingSince: &collecting}
	repo := &usageAnalyticsRepository{db: tx}
	spaces, total, err := repo.Tenants(context.Background(), q)
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	require.Len(t, spaces, 3)
	q.GroupBy = "knowledge_base"
	kbs, total, err := repo.KnowledgeBases(context.Background(), q)
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Equal(t, int64(1), kbs[0].CrossTenantAccesses)
	logUsageGovernancePostgresPlans(t, tx, now)
}

func TestUsageGovernanceSQLiteExplain(t *testing.T) {
	db := usageAnalyticsTestDB(t)
	logUsageGovernanceSQLitePlans(t, db, time.Now().UTC())
}

type usageExplainCase struct {
	name, query string
	args        []any
}

func usageGovernanceExplainCases(now time.Time) []usageExplainCase {
	return []usageExplainCase{
		{"overview_30d", `SELECT SUM(total_tokens) FROM model_usage_events WHERE occurred_at>=? AND occurred_at<?`, []any{now.AddDate(0, 0, -30), now}},
		{"overview_90d", `SELECT COUNT(*) FROM mcp_usage_events WHERE occurred_at>=? AND occurred_at<?`, []any{now.AddDate(0, 0, -90), now}},
		{"spaces_30d", `SELECT t.id,COALESCE(m.tokens,0) FROM tenants t LEFT JOIN (SELECT tenant_id,SUM(total_tokens) tokens FROM model_usage_events WHERE occurred_at>=? AND occurred_at<? GROUP BY tenant_id) m ON m.tenant_id=t.id WHERE t.deleted_at IS NULL`, []any{now.AddDate(0, 0, -30), now}},
		{"kb_governance_90d", `SELECT kb.id,COUNT(e.id) FROM knowledge_bases kb LEFT JOIN usage_resource_links l ON l.resource_type='knowledge_base' AND l.resource_id=kb.id LEFT JOIN model_usage_events e ON l.event_kind='model' AND l.event_id=e.id AND e.occurred_at>=? AND e.occurred_at<? WHERE kb.deleted_at IS NULL GROUP BY kb.id`, []any{now.AddDate(0, 0, -90), now}},
		{"cross_tenant_ranking", `SELECT l.resource_tenant_id,COUNT(*) FROM usage_resource_links l JOIN model_usage_events e ON l.event_kind='model' AND l.event_id=e.id WHERE l.resource_type='knowledge_base' AND e.occurred_at>=? AND e.occurred_at<? AND e.tenant_id<>l.resource_tenant_id GROUP BY l.resource_tenant_id`, []any{now.AddDate(0, 0, -30), now}},
		{"mcp_adoption", `SELECT COUNT(DISTINCT caller_tenant_id) FROM mcp_usage_events WHERE occurred_at>=? AND occurred_at<? AND direction='inbound' AND caller_tenant_id IS NOT NULL`, []any{now.AddDate(0, 0, -30), now}},
		{"top_knowledge_bases", `SELECT l.resource_id,COUNT(DISTINCT e.tenant_id) callers FROM usage_resource_links l JOIN model_usage_events e ON l.event_kind='model' AND l.event_id=e.id WHERE l.resource_type='knowledge_base' AND e.occurred_at>=? AND e.occurred_at<? GROUP BY l.resource_id ORDER BY callers DESC LIMIT 5`, []any{now.AddDate(0, 0, -30), now}},
	}
}

func logUsageGovernancePostgresPlans(t *testing.T, db *gorm.DB, now time.Time) {
	t.Helper()
	for _, tc := range usageGovernanceExplainCases(now) {
		rows, err := db.Raw("EXPLAIN "+tc.query, tc.args...).Rows()
		require.NoError(t, err, tc.name)
		defer rows.Close()
		var lines []string
		for rows.Next() {
			var line string
			require.NoError(t, rows.Scan(&line))
			lines = append(lines, line)
		}
		t.Logf("PostgreSQL EXPLAIN %s: %s", tc.name, strings.Join(lines, " | "))
	}
}

func logUsageGovernanceSQLitePlans(t *testing.T, db *gorm.DB, now time.Time) {
	t.Helper()
	for _, tc := range usageGovernanceExplainCases(now) {
		rows, err := db.Raw("EXPLAIN QUERY PLAN "+tc.query, tc.args...).Rows()
		require.NoError(t, err, tc.name)
		defer rows.Close()
		var details []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			require.NoError(t, rows.Scan(&id, &parent, &unused, &detail))
			details = append(details, detail)
		}
		t.Logf("SQLite EXPLAIN %s: %s", tc.name, strings.Join(details, " | "))
	}
}

func usageAnalyticsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Tenant{}, &types.KnowledgeBase{}, &types.SystemSetting{}, &types.ModelUsageEvent{}, &types.MCPUsageEvent{}, &types.UsageResourceLink{}))
	return db
}

func TestUsageCollectingSinceIsWriteOnceAndIndependentOfEvents(t *testing.T) {
	db := usageAnalyticsTestDB(t)
	repo := NewUsageAnalyticsRepository(db)
	first := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	require.NoError(t, repo.EnsureCollectingSince(context.Background(), first))
	require.NoError(t, repo.EnsureCollectingSince(context.Background(), first.Add(24*time.Hour)))
	got, err := repo.CollectingSince(context.Background())
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, first, *got)
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

func TestUsageAnalyticsPhase2FiltersAndOperationDistribution(t *testing.T) {
	db := usageAnalyticsTestDB(t)
	repo := NewUsageAnalyticsRepository(db)
	now := time.Now()
	for _, event := range []*types.ModelUsageEvent{
		{EventKey: "message:foreground", TenantID: 1, PrincipalType: "web_user", Channel: "web", Operation: types.ModelUsageOperationKnowledgeQA, ModelType: "knowledge_qa", TotalTokens: 10, UsageSource: "provider", Status: "completed", OccurredAt: now, CreatedAt: now},
		{EventKey: "rewrite:background", TenantID: 1, PrincipalType: "web_user", Channel: "background", Operation: types.ModelUsageOperationQueryRewrite, ModelType: "knowledge_qa", TotalTokens: 5, UsageSource: "provider", Status: "completed", OccurredAt: now, CreatedAt: now},
	} {
		require.NoError(t, db.Create(event).Error)
	}
	base := types.UsageTimeRange{From: now.Add(-time.Hour), To: now.Add(time.Hour)}
	background := base
	background.UsageClass = types.UsageClassBackground
	overview, err := repo.Overview(context.Background(), background)
	require.NoError(t, err)
	require.Equal(t, int64(5), overview.TotalTokens)
	require.Equal(t, int64(0), overview.ForegroundTokens)
	require.Equal(t, int64(5), overview.BackgroundTokens)

	rows, err := repo.Operations(context.Background(), base)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, types.ModelUsageOperationKnowledgeQA, rows[0].Operation)
	require.InDelta(t, 66.666, rows[0].Percentage, 0.01)
	require.Equal(t, types.UsageClassForeground, rows[0].UsageClass)

	require.NoError(t, db.Create(&types.MCPUsageEvent{EventKey: "inbound:1", PrincipalType: "api", Direction: "inbound", ToolName: "search", Success: true, OccurredAt: now, CreatedAt: now}).Error)
	require.NoError(t, db.Create(&types.MCPUsageEvent{EventKey: "outbound:1", PrincipalType: "web", Direction: "outbound", ToolName: "search", Success: true, OccurredAt: now, CreatedAt: now}).Error)
	outbound := base
	outbound.Direction = "outbound"
	mcpOverview, err := repo.Overview(context.Background(), outbound)
	require.NoError(t, err)
	require.Equal(t, int64(1), mcpOverview.MCPCalls)
}

func TestUsageGovernanceIncludesZeroUsageAndAttributesCrossTenant(t *testing.T) {
	db := usageAnalyticsTestDB(t)
	repo := NewUsageAnalyticsRepository(db)
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	for _, tenant := range []*types.Tenant{
		{ID: 1, Name: "Space A", Status: "active"}, {ID: 2, Name: "Space B", Status: "active"},
		{ID: 3, Name: "Space C", Status: "active"}, {ID: 4, Name: "Unused", Status: "active"},
	} {
		require.NoError(t, db.Create(tenant).Error)
	}
	for _, kb := range []*types.KnowledgeBase{
		{ID: "kb-b", Name: "Shared B", TenantID: 2}, {ID: "kb-never", Name: "Never", TenantID: 4},
	} {
		require.NoError(t, db.Create(kb).Error)
	}
	model := &types.ModelUsageEvent{EventKey: "model:a", TenantID: 1, PrincipalType: types.PrincipalWebUser, PrincipalID: "user-a", Channel: "web", Operation: types.ModelUsageOperationKnowledgeQA, TotalTokens: 50, UsageSource: "provider", Status: "completed", OccurredAt: now.Add(-time.Hour), CreatedAt: now.Add(-time.Hour)}
	require.NoError(t, db.Create(model).Error)
	spaceB := uint64(2)
	mcp := &types.MCPUsageEvent{EventKey: "mcp:b", CallerTenantID: &spaceB, PrincipalType: types.PrincipalAPITenant, PrincipalID: "key-b", Direction: "inbound", ToolName: "search", Success: true, LatencyMs: 10, OccurredAt: now.Add(-time.Hour), CreatedAt: now.Add(-time.Hour)}
	require.NoError(t, db.Create(mcp).Error)
	unattributed := &types.MCPUsageEvent{EventKey: "mcp:unknown", PrincipalType: types.UsagePrincipalUnknown, Direction: "inbound", ToolName: "search", Success: true, OccurredAt: now.Add(-time.Hour), CreatedAt: now.Add(-time.Hour)}
	require.NoError(t, db.Create(unattributed).Error)
	spaceC := uint64(3)
	outbound := &types.MCPUsageEvent{EventKey: "mcp:c-out", CallerTenantID: &spaceC, PrincipalType: types.PrincipalWebUser, PrincipalID: "user-c", Direction: "outbound", ToolName: "remote", Success: true, OccurredAt: now.Add(-time.Hour), CreatedAt: now.Add(-time.Hour)}
	require.NoError(t, db.Create(outbound).Error)
	for _, link := range []*types.UsageResourceLink{
		{EventKind: "model", EventID: model.ID, ResourceType: "knowledge_base", ResourceID: "kb-b", ResourceTenantID: 2},
		{EventKind: "mcp", EventID: mcp.ID, ResourceType: "knowledge_base", ResourceID: "kb-b", ResourceTenantID: 2},
		{EventKind: "mcp", EventID: unattributed.ID, ResourceType: "knowledge_base", ResourceID: "kb-b", ResourceTenantID: 2},
	} {
		require.NoError(t, db.Create(link).Error)
	}
	collecting := now.AddDate(0, 0, -100)
	q := types.UsageTimeRange{From: now.AddDate(0, 0, -30), To: now, Page: 1, PageSize: 20, IncludeInactive: true, CollectingSince: &collecting}
	spaces, total, err := repo.Tenants(context.Background(), q)
	require.NoError(t, err)
	require.Equal(t, int64(4), total)
	byID := map[uint64]types.TenantUsageRow{}
	for _, row := range spaces {
		byID[row.TenantID] = row
	}
	require.Equal(t, int64(1), byID[1].CrossTenantAccesses)
	require.Equal(t, int64(1), byID[1].KnowledgeBasesUsed)
	require.Equal(t, int64(1), byID[1].ExternalKnowledgeBasesUsed)
	require.Equal(t, int64(1), byID[2].KnowledgeBasesOwned)
	require.Equal(t, int64(1), byID[2].OwnedKBCrossTenantAccesses)
	require.True(t, byID[2].MCPAdopted)
	require.False(t, byID[3].MCPAdopted, "outbound-only MCP is not adoption")
	require.Equal(t, types.UsageGovernanceNeverUsed, byID[4].UsageStatus)

	kbQuery := q
	kbQuery.GroupBy = "knowledge_base"
	kbs, total, err := repo.KnowledgeBases(context.Background(), kbQuery)
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	byKB := map[string]types.KnowledgeBaseUsageRow{}
	for _, row := range kbs {
		byKB[row.KnowledgeBaseID] = row
	}
	require.Equal(t, int64(3), byKB["kb-b"].TotalAccesses)
	require.Equal(t, int64(1), byKB["kb-b"].CrossTenantAccesses)
	require.Equal(t, int64(1), byKB["kb-b"].ExternalTenants)
	require.Equal(t, types.UsageGovernanceNeverUsed, byKB["kb-never"].UsageStatus)
	trendQuery := q
	trendQuery.KnowledgeBaseID = "kb-b"
	trendQuery.Metric = "accesses"
	trendQuery.Interval = "day"
	trend, err := repo.TimeSeries(context.Background(), trendQuery)
	require.NoError(t, err)
	require.Len(t, trend, 1)
	require.Equal(t, int64(1), trend[0].ModelAccesses)
	require.Equal(t, int64(2), trend[0].MCPCalls)
	require.Equal(t, int64(1), trend[0].ExternalAccesses)
}

func TestUsageGovernanceInsufficientCollectionHistory(t *testing.T) {
	db := usageAnalyticsTestDB(t)
	repo := NewUsageAnalyticsRepository(db)
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&types.Tenant{ID: 1, Name: "Space", Status: "active"}).Error)
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "kb", Name: "KB", TenantID: 1}).Error)
	collecting := now.AddDate(0, 0, -20)
	q := types.UsageTimeRange{From: now.AddDate(0, 0, -30), To: now, Page: 1, PageSize: 20, IncludeInactive: true, CollectingSince: &collecting}
	spaces, _, err := repo.Tenants(context.Background(), q)
	require.NoError(t, err)
	require.Equal(t, types.UsageGovernanceInsufficientData, spaces[0].UsageStatus)
	q.GroupBy = "knowledge_base"
	kbs, _, err := repo.KnowledgeBases(context.Background(), q)
	require.NoError(t, err)
	require.Equal(t, types.UsageGovernanceInsufficientData, kbs[0].UsageStatus)
}

func TestUsageGovernanceLowActivityAndInactiveStatuses(t *testing.T) {
	db := usageAnalyticsTestDB(t)
	repo := NewUsageAnalyticsRepository(db)
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	for _, tenant := range []*types.Tenant{{ID: 1, Name: "Low", Status: "active"}, {ID: 2, Name: "Inactive", Status: "active"}} {
		require.NoError(t, db.Create(tenant).Error)
	}
	for _, kb := range []*types.KnowledgeBase{{ID: "kb-low", Name: "Low", TenantID: 1}, {ID: "kb-inactive", Name: "Inactive", TenantID: 2}} {
		require.NoError(t, db.Create(kb).Error)
	}
	for i, fixture := range []struct {
		tenant uint64
		kb     string
		at     time.Time
	}{{1, "kb-low", now.AddDate(0, 0, -60)}, {2, "kb-inactive", now.AddDate(0, 0, -100)}} {
		event := &types.ModelUsageEvent{EventKey: fmt.Sprintf("status:%d", i), TenantID: fixture.tenant, PrincipalType: types.PrincipalWebUser, PrincipalID: "user", Channel: "web", Operation: types.ModelUsageOperationKnowledgeQA, TotalTokens: 1, UsageSource: "provider", Status: "completed", OccurredAt: fixture.at, CreatedAt: fixture.at}
		require.NoError(t, db.Create(event).Error)
		require.NoError(t, db.Create(&types.UsageResourceLink{EventKind: "model", EventID: event.ID, ResourceType: "knowledge_base", ResourceID: fixture.kb, ResourceTenantID: fixture.tenant}).Error)
	}
	collecting := now.AddDate(0, 0, -120)
	q := types.UsageTimeRange{From: now.AddDate(0, 0, -30), To: now, Page: 1, PageSize: 20, IncludeInactive: true, CollectingSince: &collecting}
	spaces, _, err := repo.Tenants(context.Background(), q)
	require.NoError(t, err)
	byTenant := map[uint64]string{}
	for _, row := range spaces {
		byTenant[row.TenantID] = row.UsageStatus
	}
	require.Equal(t, types.UsageGovernanceLowActivity, byTenant[1])
	require.Equal(t, types.UsageGovernanceInactive, byTenant[2])
	q.GroupBy = "knowledge_base"
	kbs, _, err := repo.KnowledgeBases(context.Background(), q)
	require.NoError(t, err)
	byKB := map[string]string{}
	for _, row := range kbs {
		byKB[row.KnowledgeBaseID] = row.UsageStatus
	}
	require.Equal(t, types.UsageGovernanceLowActivity, byKB["kb-low"])
	require.Equal(t, types.UsageGovernanceInactive, byKB["kb-inactive"])
}
