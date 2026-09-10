package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type usageAnalyticsRepository struct{ db *gorm.DB }

const usageCollectingSinceKey = "usage.analytics.collecting_since"

func NewUsageAnalyticsRepository(db *gorm.DB) interfaces.UsageAnalyticsRepository {
	return &usageAnalyticsRepository{db: db}
}

func (r *usageAnalyticsRepository) EnsureCollectingSince(ctx context.Context, startedAt time.Time) error {
	value, err := json.Marshal(startedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	setting := &types.SystemSetting{
		Key: usageCollectingSinceKey, Value: types.JSON(value), ValueType: "string",
		Category: "internal", Description: "Internal Usage Analytics collection epoch.",
		CreatedAt: startedAt.UTC(), UpdatedAt: startedAt.UTC(),
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "key"}}, DoNothing: true,
	}).Create(setting).Error
}

func (r *usageAnalyticsRepository) RecordModelUsage(ctx context.Context, event *types.ModelUsageEvent, resources []types.UsageResourceLink) (bool, error) {
	inserted := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "event_key"}}, DoNothing: true}).Create(event)
		if res.Error != nil {
			return res.Error
		}
		inserted = res.RowsAffected > 0
		if !inserted {
			return tx.Select("id").Where("event_key = ?", event.EventKey).First(event).Error
		}
		return insertUsageResourceLinks(tx, types.UsageEventKindModel, event.ID, resources)
	})
	return inserted, err
}

func (r *usageAnalyticsRepository) RecordMCPUsage(ctx context.Context, event *types.MCPUsageEvent, resources []types.UsageResourceLink) (bool, error) {
	inserted := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "event_key"}}, DoNothing: true}).Create(event)
		if res.Error != nil {
			return res.Error
		}
		inserted = res.RowsAffected > 0
		if !inserted {
			return tx.Select("id").Where("event_key = ?", event.EventKey).First(event).Error
		}
		return insertUsageResourceLinks(tx, types.UsageEventKindMCP, event.ID, resources)
	})
	return inserted, err
}

func insertUsageResourceLinks(tx *gorm.DB, kind string, eventID uint64, resources []types.UsageResourceLink) error {
	if len(resources) == 0 {
		return nil
	}
	rows := make([]types.UsageResourceLink, 0, len(resources))
	seen := map[string]bool{}
	for _, resource := range resources {
		key := resource.ResourceType + "\x00" + resource.ResourceID
		if resource.ResourceID == "" || resource.ResourceTenantID == 0 || seen[key] {
			continue
		}
		seen[key] = true
		resource.ID, resource.EventKind, resource.EventID = 0, kind, eventID
		rows = append(rows, resource)
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

func (r *usageAnalyticsRepository) ResolveUsageResources(ctx context.Context, kbIDs, knowledgeIDs []string) ([]types.UsageResourceLink, error) {
	result := make([]types.UsageResourceLink, 0)
	if len(kbIDs) > 0 {
		var rows []struct {
			ID       string
			TenantID uint64
		}
		if err := r.db.WithContext(ctx).Table("knowledge_bases").Select("id, tenant_id").Where("id IN ?", uniqueStrings(kbIDs)).Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			result = append(result, types.UsageResourceLink{ResourceType: "knowledge_base", ResourceID: row.ID, ResourceTenantID: row.TenantID})
		}
	}
	if len(knowledgeIDs) > 0 {
		var rows []struct {
			ID, KnowledgeBaseID string
			TenantID            uint64
		}
		if err := r.db.WithContext(ctx).Table("knowledges").Select("id, knowledge_base_id, tenant_id").Where("id IN ?", uniqueStrings(knowledgeIDs)).Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			result = append(result,
				types.UsageResourceLink{ResourceType: "knowledge", ResourceID: row.ID, ResourceTenantID: row.TenantID},
				types.UsageResourceLink{ResourceType: "knowledge_base", ResourceID: row.KnowledgeBaseID, ResourceTenantID: row.TenantID},
			)
		}
	}
	return result, nil
}

func uniqueStrings(values []string) []string {
	out, seen := make([]string, 0, len(values)), map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func usageWhere(column, tenantColumn string, q types.UsageTimeRange) (string, []any) {
	where, args := column+" >= ? AND "+column+" < ?", []any{q.From, q.To}
	if q.TenantID != nil {
		where += " AND " + tenantColumn + " = ?"
		args = append(args, *q.TenantID)
	}
	if q.Operation != "" {
		where += " AND operation = ?"
		args = append(args, q.Operation)
	}
	if q.UsageClass == types.UsageClassForeground {
		where += " AND operation IN (?, ?)"
		args = append(args, types.ModelUsageOperationKnowledgeQA, types.ModelUsageOperationAgent)
	} else if q.UsageClass == types.UsageClassBackground {
		where += " AND operation NOT IN (?, ?)"
		args = append(args, types.ModelUsageOperationKnowledgeQA, types.ModelUsageOperationAgent)
	}
	if q.Channel != "" {
		where += " AND channel = ?"
		args = append(args, q.Channel)
	}
	if q.ModelType != "" {
		where += " AND model_type = ?"
		args = append(args, q.ModelType)
	}
	return where, args
}

func mcpUsageWhere(q types.UsageTimeRange) (string, []any) {
	where, args := "occurred_at >= ? AND occurred_at < ?", []any{q.From, q.To}
	if q.TenantID != nil {
		where += " AND caller_tenant_id = ?"
		args = append(args, *q.TenantID)
	}
	if q.Direction != "" {
		where += " AND direction = ?"
		args = append(args, q.Direction)
	}
	return where, args
}

func (r *usageAnalyticsRepository) Overview(ctx context.Context, q types.UsageTimeRange) (*types.UsageOverview, error) {
	out := &types.UsageOverview{}
	mw, ma := usageWhere("occurred_at", "tenant_id", q)
	if err := r.db.WithContext(ctx).Raw(`SELECT COALESCE(SUM(total_tokens),0) total_tokens,
		COALESCE(SUM(CASE WHEN operation IN ('knowledge_qa_turn','agent_turn') THEN total_tokens ELSE 0 END),0) foreground_tokens,
		COALESCE(SUM(CASE WHEN operation NOT IN ('knowledge_qa_turn','agent_turn') THEN total_tokens ELSE 0 END),0) background_tokens,
		COALESCE(SUM(input_tokens),0) input_tokens,
		COALESCE(SUM(output_tokens),0) output_tokens, COALESCE(SUM(cache_read_tokens),0) cache_read_tokens,
		COALESCE(SUM(cache_write_tokens),0) cache_write_tokens, COUNT(*) assistant_turns,
		COUNT(DISTINCT tenant_id) active_tenants FROM model_usage_events WHERE `+mw, ma...).Scan(out).Error; err != nil {
		return nil, err
	}
	mcpw, mcpa := mcpUsageWhere(q)
	var mcp struct {
		MCPCalls             int64
		MCPSuccessRate       float64
		MCPAverageLatencyMs  float64
		UnattributedMCPCalls int64
	}
	if err := r.db.WithContext(ctx).Raw(`SELECT COUNT(*) mcp_calls,
		COALESCE(100.0 * SUM(CASE WHEN success THEN 1 ELSE 0 END) / NULLIF(COUNT(*),0),0) mcp_success_rate,
		COALESCE(AVG(latency_ms),0) mcp_average_latency_ms,
		SUM(CASE WHEN caller_tenant_id IS NULL THEN 1 ELSE 0 END) unattributed_mcp_calls
		FROM mcp_usage_events WHERE `+mcpw, mcpa...).Scan(&mcp).Error; err != nil {
		return nil, err
	}
	out.MCPCalls = mcp.MCPCalls
	out.MCPSuccessRate = mcp.MCPSuccessRate
	out.MCPAverageLatencyMs = mcp.MCPAverageLatencyMs
	out.UnattributedMCPCalls = mcp.UnattributedMCPCalls
	args := append(append([]any{}, ma...), mcpa...)
	activeSQL := `SELECT COUNT(*) active_principals FROM (
		SELECT tenant_id, principal_type, principal_id FROM model_usage_events WHERE ` + mw + ` AND principal_id IS NOT NULL AND principal_id <> '' AND principal_type <> ? GROUP BY tenant_id, principal_type, principal_id
		UNION SELECT caller_tenant_id, principal_type, principal_id FROM mcp_usage_events WHERE ` + mcpw + ` AND caller_tenant_id IS NOT NULL AND principal_id IS NOT NULL AND principal_id <> '' AND principal_type <> ? GROUP BY caller_tenant_id, principal_type, principal_id) p`
	args = append(append(append([]any{}, ma...), types.UsagePrincipalUnknown), mcpa...)
	args = append(args, types.UsagePrincipalUnknown)
	if err := r.db.WithContext(ctx).Raw(activeSQL, args...).Scan(&out.ActivePrincipals).Error; err != nil {
		return nil, err
	}
	args = append(append([]any{}, ma...), mcpa...)
	activeTenantSQL := `SELECT COUNT(*) FROM (
		SELECT tenant_id FROM model_usage_events WHERE ` + mw + ` GROUP BY tenant_id
		UNION SELECT caller_tenant_id FROM mcp_usage_events WHERE ` + mcpw + ` AND caller_tenant_id IS NOT NULL GROUP BY caller_tenant_id) t`
	if err := r.db.WithContext(ctx).Raw(activeTenantSQL, args...).Scan(&out.ActiveTenants).Error; err != nil {
		return nil, err
	}
	rangeActiveTenants := out.ActiveTenants
	active30Query := q
	active30Query.From = q.To.AddDate(0, 0, -30)
	active30ModelWhere, active30ModelArgs := usageWhere("occurred_at", "tenant_id", active30Query)
	active30MCPWhere, active30MCPArgs := mcpUsageWhere(active30Query)
	active30SQL := `SELECT COUNT(*) FROM (
		SELECT tenant_id FROM model_usage_events WHERE ` + active30ModelWhere + ` GROUP BY tenant_id
		UNION SELECT caller_tenant_id FROM mcp_usage_events WHERE ` + active30MCPWhere + ` AND caller_tenant_id IS NOT NULL GROUP BY caller_tenant_id) t`
	active30Args := append(append([]any{}, active30ModelArgs...), active30MCPArgs...)
	if err := r.db.WithContext(ctx).Raw(active30SQL, active30Args...).Scan(&out.ActiveTenants).Error; err != nil {
		return nil, err
	}

	activeFrom := q.To.AddDate(0, 0, -30)
	var governance struct {
		ActiveKnowledgeBases int64
		CrossTenantUsage     int64
		MCPActiveTenants     int64
		EligibleTenants      int64
	}
	governanceSQL := `SELECT
		(SELECT COUNT(DISTINCT l.resource_id) FROM usage_resource_links l JOIN model_usage_events e ON l.event_kind='model' AND l.event_id=e.id WHERE l.resource_type='knowledge_base' AND e.occurred_at>=? AND e.occurred_at<?)
		+ (SELECT COUNT(DISTINCT l.resource_id) FROM usage_resource_links l JOIN mcp_usage_events e ON l.event_kind='mcp' AND l.event_id=e.id WHERE l.resource_type='knowledge_base' AND e.occurred_at>=? AND e.occurred_at<? AND l.resource_id NOT IN (SELECT l2.resource_id FROM usage_resource_links l2 JOIN model_usage_events e2 ON l2.event_kind='model' AND l2.event_id=e2.id WHERE l2.resource_type='knowledge_base' AND e2.occurred_at>=? AND e2.occurred_at<?)) active_knowledge_bases,
		(SELECT COUNT(*) FROM usage_resource_links l JOIN model_usage_events e ON l.event_kind='model' AND l.event_id=e.id WHERE l.resource_type='knowledge_base' AND e.occurred_at>=? AND e.occurred_at<? AND e.tenant_id<>l.resource_tenant_id)
		+ (SELECT COUNT(*) FROM usage_resource_links l JOIN mcp_usage_events e ON l.event_kind='mcp' AND l.event_id=e.id WHERE l.resource_type='knowledge_base' AND e.occurred_at>=? AND e.occurred_at<? AND e.caller_tenant_id IS NOT NULL AND e.caller_tenant_id<>l.resource_tenant_id) cross_tenant_usage,
		(SELECT COUNT(DISTINCT caller_tenant_id) FROM mcp_usage_events WHERE occurred_at>=? AND occurred_at<? AND direction='inbound' AND caller_tenant_id IS NOT NULL) mcp_active_tenants,
		(SELECT COUNT(*) FROM tenants WHERE deleted_at IS NULL AND status='active') eligible_tenants`
	gargs := []any{activeFrom, q.To, activeFrom, q.To, activeFrom, q.To, q.From, q.To, q.From, q.To, q.From, q.To}
	if err := r.db.WithContext(ctx).Raw(governanceSQL, gargs...).Scan(&governance).Error; err != nil {
		return nil, err
	}
	out.ActiveKnowledgeBases = governance.ActiveKnowledgeBases
	out.CrossTenantUsage = governance.CrossTenantUsage
	out.MCPActiveTenants = governance.MCPActiveTenants
	if out.MCPCalls > 0 {
		out.UnattributedMCPRatio = 100 * float64(out.UnattributedMCPCalls) / float64(out.MCPCalls)
	}
	if governance.EligibleTenants > 0 {
		out.MCPPlatformPenetration = 100 * float64(governance.MCPActiveTenants) / float64(governance.EligibleTenants)
	}
	if rangeActiveTenants > 0 {
		out.MCPAdoptionAmongActive = 100 * float64(governance.MCPActiveTenants) / float64(rangeActiveTenants)
	}
	if q.CollectingSince != nil && !q.CollectingSince.After(q.To.AddDate(0, 0, -90)) {
		inactiveSQL := `SELECT COUNT(*) FROM knowledge_bases kb WHERE kb.deleted_at IS NULL AND NOT EXISTS (
			SELECT 1 FROM usage_resource_links l LEFT JOIN model_usage_events me ON l.event_kind='model' AND l.event_id=me.id
			LEFT JOIN mcp_usage_events xe ON l.event_kind='mcp' AND l.event_id=xe.id
			WHERE l.resource_type='knowledge_base' AND l.resource_id=kb.id AND COALESCE(me.occurred_at,xe.occurred_at)>=? AND COALESCE(me.occurred_at,xe.occurred_at)<?)`
		if err := r.db.WithContext(ctx).Raw(inactiveSQL, q.To.AddDate(0, 0, -90), q.To).Scan(&out.InactiveKnowledgeBases).Error; err != nil {
			return nil, err
		}
	}
	var growth struct{ CurrentTokens, PreviousTokens int64 }
	if err := r.db.WithContext(ctx).Raw(`SELECT
		COALESCE(SUM(CASE WHEN occurred_at>=? AND occurred_at<? THEN total_tokens ELSE 0 END),0) current_tokens,
		COALESCE(SUM(CASE WHEN occurred_at>=? AND occurred_at<? THEN total_tokens ELSE 0 END),0) previous_tokens
		FROM model_usage_events WHERE occurred_at>=? AND occurred_at<?`, q.To.AddDate(0, 0, -7), q.To, q.To.AddDate(0, 0, -14), q.To.AddDate(0, 0, -7), q.To.AddDate(0, 0, -14), q.To).Scan(&growth).Error; err != nil {
		return nil, err
	}
	if growth.PreviousTokens > 0 {
		out.TokenGrowthPercent = 100 * float64(growth.CurrentTokens-growth.PreviousTokens) / float64(growth.PreviousTokens)
	}
	if out.InactiveKnowledgeBases > 0 {
		out.AttentionNeeded = append(out.AttentionNeeded, types.UsageAttention{Code: "inactive_knowledge_bases", EntityType: "knowledge_base", Value: float64(out.InactiveKnowledgeBases)})
	}
	if out.MCPCalls >= 20 && out.MCPSuccessRate < 90 {
		out.AttentionNeeded = append(out.AttentionNeeded, types.UsageAttention{Code: "mcp_high_failure_rate", Value: 100 - out.MCPSuccessRate})
	}
	if out.MCPCalls >= 20 && out.UnattributedMCPRatio >= 10 {
		out.AttentionNeeded = append(out.AttentionNeeded, types.UsageAttention{Code: "mcp_high_unattributed_ratio", Value: out.UnattributedMCPRatio})
	}
	if out.MCPCalls >= 20 && out.MCPAverageLatencyMs >= 3000 {
		out.AttentionNeeded = append(out.AttentionNeeded, types.UsageAttention{Code: "mcp_high_latency", Value: out.MCPAverageLatencyMs})
	}
	if growth.CurrentTokens >= 10000 && out.TokenGrowthPercent >= 100 {
		out.AttentionNeeded = append(out.AttentionNeeded, types.UsageAttention{Code: "significant_token_growth", Value: out.TokenGrowthPercent})
	}
	topQuery := q
	topQuery.GroupBy, topQuery.Sort, topQuery.Page, topQuery.PageSize = "knowledge_base", "unique_tenants", 1, 5
	top, _, err := r.knowledgeBaseGovernance(ctx, topQuery)
	if err != nil {
		return nil, err
	}
	out.TopKnowledgeBases = top
	if q.CollectingSince != nil && !q.CollectingSince.After(q.To.AddDate(0, 0, -90)) {
		inactiveQuery := q
		inactiveQuery.IncludeInactive, inactiveQuery.Status, inactiveQuery.Page, inactiveQuery.PageSize = true, types.UsageGovernanceInactive, 1, 1
		_, inactiveSpaces, err := r.Tenants(ctx, inactiveQuery)
		if err != nil {
			return nil, err
		}
		if inactiveSpaces > 0 {
			out.AttentionNeeded = append(out.AttentionNeeded, types.UsageAttention{Code: "inactive_spaces", EntityType: "tenant", Value: float64(inactiveSpaces)})
		}
	}
	return out, nil
}

func page(q types.UsageTimeRange) (int, int) {
	p, s := q.Page, q.PageSize
	if p < 1 {
		p = 1
	}
	if s < 1 {
		s = 20
	}
	if s > 100 {
		s = 100
	}
	return p, s
}

func (r *usageAnalyticsRepository) Tenants(ctx context.Context, q types.UsageTimeRange) ([]types.TenantUsageRow, int64, error) {
	mw, ma := usageWhere("occurred_at", "tenant_id", q)
	xw, xa := mcpUsageWhere(q)
	p, size := page(q)
	orders := map[string]string{"tokens": "total_tokens DESC", "mcp_calls": "mcp_calls DESC", "active_principals": "active_principals DESC", "last_active": "last_active DESC", "cross_tenant": "cross_tenant_accesses DESC"}
	order := orders[q.Sort]
	if order == "" {
		order = orders["tokens"]
	}
	collectingSince := q.To
	if q.CollectingSince != nil {
		collectingSince = *q.CollectingSince
	}
	lastActiveProjection := "all_activity.last_active,0 last_active_unix"
	if r.db.Dialector.Name() == "sqlite" {
		lastActiveProjection = "NULL last_active,COALESCE(CAST(strftime('%s',all_activity.last_active) AS INTEGER),0) last_active_unix"
		if order == "last_active DESC" {
			order = "last_active_unix DESC"
		}
	}
	sql := `WITH model AS (SELECT tenant_id, COUNT(*) assistant_turns, SUM(CASE WHEN operation='agent_turn' THEN 1 ELSE 0 END) agent_turns,
		SUM(input_tokens) input_tokens, SUM(output_tokens) output_tokens, SUM(total_tokens) total_tokens, MAX(occurred_at) last_active
		FROM model_usage_events WHERE ` + mw + ` GROUP BY tenant_id),
	 mcp AS (SELECT caller_tenant_id tenant_id, COUNT(*) mcp_calls, 100.0*SUM(CASE WHEN success THEN 1 ELSE 0 END)/COUNT(*) mcp_success_rate,
		AVG(latency_ms) mcp_avg_latency_ms, MAX(occurred_at) last_active,
		SUM(CASE WHEN direction='inbound' THEN 1 ELSE 0 END) inbound_calls FROM mcp_usage_events WHERE ` + xw + ` AND caller_tenant_id IS NOT NULL GROUP BY caller_tenant_id),
	 principals AS (SELECT tenant_id, COUNT(*) active_principals FROM (SELECT tenant_id, principal_type, principal_id FROM model_usage_events WHERE ` + mw + ` AND principal_id IS NOT NULL AND principal_id<>'' AND principal_type<>?
		UNION SELECT caller_tenant_id, principal_type, principal_id FROM mcp_usage_events WHERE ` + xw + ` AND caller_tenant_id IS NOT NULL AND principal_id IS NOT NULL AND principal_id<>'' AND principal_type<>?) u GROUP BY tenant_id),
	 kb_hits AS (SELECT e.tenant_id caller_tenant_id,l.resource_id kb_id,l.resource_tenant_id owner_tenant_id FROM usage_resource_links l JOIN model_usage_events e ON l.event_kind='model' AND l.event_id=e.id WHERE l.resource_type='knowledge_base' AND ` + strings.ReplaceAll(strings.ReplaceAll(mw, "occurred_at", "e.occurred_at"), "tenant_id", "e.tenant_id") + `
		UNION ALL SELECT e.caller_tenant_id,l.resource_id,l.resource_tenant_id FROM usage_resource_links l JOIN mcp_usage_events e ON l.event_kind='mcp' AND l.event_id=e.id WHERE l.resource_type='knowledge_base' AND e.caller_tenant_id IS NOT NULL AND ` + strings.ReplaceAll(strings.ReplaceAll(xw, "occurred_at", "e.occurred_at"), "caller_tenant_id", "e.caller_tenant_id") + `),
	 kb_usage AS (SELECT caller_tenant_id tenant_id,COUNT(DISTINCT kb_id) kb_used_count,COUNT(DISTINCT CASE WHEN caller_tenant_id<>owner_tenant_id THEN kb_id END) external_kb_used_count,SUM(CASE WHEN caller_tenant_id<>owner_tenant_id THEN 1 ELSE 0 END) cross_tenant_accesses FROM kb_hits GROUP BY caller_tenant_id),
	 owned_cross AS (SELECT owner_tenant_id tenant_id,COUNT(*) owned_kb_cross_tenant_accesses FROM kb_hits WHERE caller_tenant_id<>owner_tenant_id GROUP BY owner_tenant_id),
	 owned AS (SELECT tenant_id,COUNT(*) kb_owned_count FROM knowledge_bases WHERE deleted_at IS NULL GROUP BY tenant_id),
	 all_activity AS (SELECT tenant_id,MAX(occurred_at) last_active FROM (SELECT tenant_id,occurred_at FROM model_usage_events UNION ALL SELECT caller_tenant_id,occurred_at FROM mcp_usage_events WHERE caller_tenant_id IS NOT NULL) a GROUP BY tenant_id),
	 combined AS (SELECT t.id tenant_id, t.name tenant_name, COALESCE(p.active_principals,0) active_principals,
		COALESCE(model.assistant_turns,0) assistant_turns, COALESCE(model.agent_turns,0) agent_turns,
		COALESCE(model.input_tokens,0) input_tokens, COALESCE(model.output_tokens,0) output_tokens, COALESCE(model.total_tokens,0) total_tokens,
		COALESCE(mcp.mcp_calls,0) mcp_calls, COALESCE(mcp.mcp_success_rate,0) mcp_success_rate, COALESCE(mcp.mcp_avg_latency_ms,0) mcp_avg_latency_ms,
		` + lastActiveProjection + `,COALESCE(kb_usage.kb_used_count,0) kb_used_count,COALESCE(owned.kb_owned_count,0) kb_owned_count,
		COALESCE(kb_usage.external_kb_used_count,0) external_kb_used_count,COALESCE(kb_usage.cross_tenant_accesses,0) cross_tenant_accesses,
		COALESCE(owned_cross.owned_kb_cross_tenant_accesses,0) owned_kb_cross_tenant_accesses,CASE WHEN COALESCE(mcp.inbound_calls,0)>0 THEN 1 ELSE 0 END mcp_adopted,
		CASE WHEN all_activity.last_active>=? THEN 'active' WHEN all_activity.last_active>=? THEN 'low_activity'
		WHEN ?>? THEN 'insufficient_data' WHEN all_activity.last_active IS NULL THEN 'never_used' ELSE 'inactive' END usage_status,
		CASE WHEN model.tenant_id IS NOT NULL OR mcp.tenant_id IS NOT NULL THEN 1 ELSE 0 END has_window_usage
		FROM tenants t LEFT JOIN model ON model.tenant_id=t.id LEFT JOIN mcp ON mcp.tenant_id=t.id LEFT JOIN principals p ON p.tenant_id=t.id
		LEFT JOIN kb_usage ON kb_usage.tenant_id=t.id LEFT JOIN owned_cross ON owned_cross.tenant_id=t.id LEFT JOIN owned ON owned.tenant_id=t.id LEFT JOIN all_activity ON all_activity.tenant_id=t.id
		WHERE t.deleted_at IS NULL), filtered AS (SELECT * FROM combined WHERE 1=1`
	args := append([]any{}, ma...)
	args = append(args, xa...)
	args = append(args, ma...)
	args = append(args, types.UsagePrincipalUnknown)
	args = append(args, xa...)
	args = append(args, types.UsagePrincipalUnknown)
	args = append(args, ma...)
	args = append(args, xa...)
	args = append(args, q.To.AddDate(0, 0, -30), q.To.AddDate(0, 0, -90), collectingSince, q.To.AddDate(0, 0, -90))
	if !q.IncludeInactive {
		sql += ` AND has_window_usage=1`
	}
	if q.Status != "" {
		sql += ` AND usage_status=?`
		args = append(args, q.Status)
	}
	if q.MCPAdoption == "adopted" {
		sql += ` AND mcp_adopted=1`
	} else if q.MCPAdoption == "not_adopted" {
		sql += ` AND mcp_adopted=0`
	}
	if q.CrossTenant == "with" {
		sql += ` AND cross_tenant_accesses>0`
	} else if q.CrossTenant == "without" {
		sql += ` AND cross_tenant_accesses=0`
	}
	sql += `) SELECT *,COUNT(*) OVER() total_count FROM filtered ORDER BY ` + order + `,tenant_id LIMIT ? OFFSET ?`
	args = append(args, size, (p-1)*size)
	var rows []types.TenantUsageRow
	if err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	restoreSQLiteLastActive(rows)
	var total int64
	if len(rows) > 0 {
		total = rows[0].TotalCount
	}
	return rows, total, nil
}

func (r *usageAnalyticsRepository) TimeSeries(ctx context.Context, q types.UsageTimeRange) ([]types.UsageTimeSeriesPoint, error) {
	mw, ma := usageWhere("occurred_at", "tenant_id", q)
	xw, xa := mcpUsageWhere(q)
	bucket := "date_trunc('day', occurred_at)"
	if r.db.Dialector.Name() == "sqlite" {
		formats := map[string]string{"hour": "%Y-%m-%d %H:00:00", "day": "%Y-%m-%d 00:00:00", "month": "%Y-%m-01 00:00:00"}
		f := formats[q.Interval]
		if q.Interval == "week" {
			bucket = "datetime(occurred_at, 'weekday 1', '-7 days', 'start of day')"
		} else if f == "" {
			f = formats["day"]
			bucket = "strftime('" + f + "', occurred_at)"
		} else {
			bucket = "strftime('" + f + "', occurred_at)"
		}
	} else {
		allowed := map[string]bool{"hour": true, "day": true, "week": true, "month": true}
		interval := q.Interval
		if !allowed[interval] {
			interval = "day"
		}
		bucket = "date_trunc('" + interval + "', occurred_at)"
	}
	if q.Metric == "accesses" || q.KnowledgeBaseID != "" {
		modelWhere, modelArgs := usageWhere("e.occurred_at", "e.tenant_id", q)
		mcpWhere, mcpArgs := mcpUsageWhere(q)
		mcpWhere = strings.ReplaceAll(mcpWhere, "occurred_at", "e.occurred_at")
		mcpWhere = strings.ReplaceAll(mcpWhere, "caller_tenant_id", "e.caller_tenant_id")
		modelBucket := strings.ReplaceAll(bucket, "occurred_at", "e.occurred_at")
		mcpBucket := strings.ReplaceAll(bucket, "occurred_at", "e.occurred_at")
		kbFilter := ""
		args := append([]any{}, modelArgs...)
		if q.KnowledgeBaseID != "" {
			kbFilter = " AND l.resource_id=?"
			args = append(args, q.KnowledgeBaseID)
		}
		args = append(args, mcpArgs...)
		if q.KnowledgeBaseID != "" {
			args = append(args, q.KnowledgeBaseID)
		}
		sql := `SELECT bucket,SUM(model_accesses) model_accesses,SUM(mcp_calls) mcp_calls,SUM(internal_accesses) internal_accesses,SUM(external_accesses) external_accesses FROM (
			SELECT ` + modelBucket + ` bucket,COUNT(*) model_accesses,0 mcp_calls,SUM(CASE WHEN e.tenant_id=l.resource_tenant_id THEN 1 ELSE 0 END) internal_accesses,SUM(CASE WHEN e.tenant_id<>l.resource_tenant_id THEN 1 ELSE 0 END) external_accesses
			FROM usage_resource_links l JOIN model_usage_events e ON l.event_kind='model' AND l.event_id=e.id WHERE l.resource_type='knowledge_base' AND ` + modelWhere + kbFilter + ` GROUP BY bucket
			UNION ALL SELECT ` + mcpBucket + ` bucket,0,COUNT(*),SUM(CASE WHEN e.caller_tenant_id=l.resource_tenant_id THEN 1 ELSE 0 END),SUM(CASE WHEN e.caller_tenant_id IS NOT NULL AND e.caller_tenant_id<>l.resource_tenant_id THEN 1 ELSE 0 END)
			FROM usage_resource_links l JOIN mcp_usage_events e ON l.event_kind='mcp' AND l.event_id=e.id WHERE l.resource_type='knowledge_base' AND ` + mcpWhere + kbFilter + ` GROUP BY bucket) s GROUP BY bucket ORDER BY bucket`
		return r.scanUsageTimeSeries(ctx, sql, args)
	}
	sql := `SELECT bucket, SUM(input_tokens) input_tokens, SUM(output_tokens) output_tokens, SUM(total_tokens) total_tokens,
		SUM(assistant_turns) assistant_turns, SUM(mcp_calls) mcp_calls FROM (
		SELECT ` + bucket + ` bucket, SUM(input_tokens) input_tokens, SUM(output_tokens) output_tokens, SUM(total_tokens) total_tokens, COUNT(*) assistant_turns, 0 mcp_calls FROM model_usage_events WHERE ` + mw + ` GROUP BY bucket
		UNION ALL SELECT ` + bucket + ` bucket, 0,0,0,0,COUNT(*) FROM mcp_usage_events WHERE ` + xw + ` GROUP BY bucket) s GROUP BY bucket ORDER BY bucket`
	args := append(append([]any{}, ma...), xa...)
	return r.scanUsageTimeSeries(ctx, sql, args)
}

func (r *usageAnalyticsRepository) scanUsageTimeSeries(ctx context.Context, query string, args []any) ([]types.UsageTimeSeriesPoint, error) {
	if r.db.Dialector.Name() != "sqlite" {
		var rows []types.UsageTimeSeriesPoint
		err := r.db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error
		return rows, err
	}
	var scanned []struct {
		Bucket           string
		InputTokens      int64
		OutputTokens     int64
		TotalTokens      int64
		AssistantTurns   int64
		MCPCalls         int64
		ModelAccesses    int64
		InternalAccesses int64
		ExternalAccesses int64
	}
	if err := r.db.WithContext(ctx).Raw(query, args...).Scan(&scanned).Error; err != nil {
		return nil, err
	}
	rows := make([]types.UsageTimeSeriesPoint, 0, len(scanned))
	for _, row := range scanned {
		bucket, err := time.ParseInLocation("2006-01-02 15:04:05", row.Bucket, time.UTC)
		if err != nil {
			return nil, err
		}
		rows = append(rows, types.UsageTimeSeriesPoint{
			Bucket: bucket, InputTokens: row.InputTokens, OutputTokens: row.OutputTokens,
			TotalTokens: row.TotalTokens, AssistantTurns: row.AssistantTurns, MCPCalls: row.MCPCalls,
			ModelAccesses: row.ModelAccesses, InternalAccesses: row.InternalAccesses, ExternalAccesses: row.ExternalAccesses,
		})
	}
	return rows, nil
}

func (r *usageAnalyticsRepository) Models(ctx context.Context, q types.UsageTimeRange) ([]types.ModelUsageRow, int64, error) {
	w, args := usageWhere("occurred_at", "tenant_id", q)
	p, size := page(q)
	orders := map[string]string{"tokens": "total_tokens DESC", "turns": "assistant_turns DESC", "last_active": "last_active DESC"}
	order := orders[q.Sort]
	if order == "" {
		order = orders["tokens"]
	}
	sql := `SELECT model_id, model_type, COUNT(*) assistant_turns, SUM(input_tokens) input_tokens, SUM(output_tokens) output_tokens, SUM(total_tokens) total_tokens,
		SUM(cache_read_tokens) cache_read_tokens, SUM(cache_write_tokens) cache_write_tokens, MAX(occurred_at) last_active, COUNT(*) OVER() total_count
		FROM model_usage_events WHERE ` + w + ` GROUP BY model_id, model_type ORDER BY ` + order + `, model_id LIMIT ? OFFSET ?`
	args = append(args, size, (p-1)*size)
	var rows []types.ModelUsageRow
	err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error
	var total int64
	if len(rows) > 0 {
		total = rows[0].TotalCount
	}
	return rows, total, err
}

func (r *usageAnalyticsRepository) Operations(ctx context.Context, q types.UsageTimeRange) ([]types.OperationUsageRow, error) {
	w, args := usageWhere("occurred_at", "tenant_id", q)
	var rows []types.OperationUsageRow
	err := r.db.WithContext(ctx).Raw(`WITH grouped AS (
		SELECT operation, COUNT(*) invocations, SUM(total_tokens) total_tokens
		FROM model_usage_events WHERE `+w+` GROUP BY operation), totals AS (
		SELECT COALESCE(SUM(total_tokens),0) total_tokens FROM grouped)
		SELECT operation, CASE WHEN operation IN ('knowledge_qa_turn','agent_turn') THEN 'foreground' ELSE 'background' END usage_class,
		invocations, grouped.total_tokens,
		CASE WHEN totals.total_tokens=0 THEN 0 ELSE 100.0*grouped.total_tokens/totals.total_tokens END percentage
		FROM grouped CROSS JOIN totals ORDER BY grouped.total_tokens DESC, operation`, args...).Scan(&rows).Error
	return rows, err
}

func (r *usageAnalyticsRepository) MCP(ctx context.Context, q types.UsageTimeRange) ([]types.MCPUsageRow, int64, error) {
	w, args := mcpUsageWhere(q)
	p, size := page(q)
	orders := map[string]string{"calls": "calls DESC", "latency": "average_latency_ms DESC", "last_active": "last_active DESC"}
	order := orders[q.Sort]
	if order == "" {
		order = orders["calls"]
	}
	selectSQL := `SELECT tool_name, COUNT(*) calls, SUM(CASE WHEN success THEN 1 ELSE 0 END) successful_calls,
		COALESCE(100.0*SUM(CASE WHEN success THEN 1 ELSE 0 END)/NULLIF(COUNT(*),0),0) success_rate, AVG(latency_ms) average_latency_ms,
		SUM(CASE WHEN caller_tenant_id IS NULL THEN 1 ELSE 0 END) unattributed_calls, MAX(occurred_at) last_active, COUNT(*) OVER() total_count
		FROM mcp_usage_events WHERE ` + w + ` GROUP BY tool_name ORDER BY ` + order + `, tool_name LIMIT ? OFFSET ?`
	if q.GroupBy == "client" {
		selectSQL = `SELECT client_name,client_version,COUNT(*) calls,SUM(CASE WHEN success THEN 1 ELSE 0 END) successful_calls,
			COALESCE(100.0*SUM(CASE WHEN success THEN 1 ELSE 0 END)/NULLIF(COUNT(*),0),0) success_rate,AVG(latency_ms) average_latency_ms,
			SUM(CASE WHEN caller_tenant_id IS NULL THEN 1 ELSE 0 END) unattributed_calls,MAX(occurred_at) last_active,COUNT(*) OVER() total_count
			FROM mcp_usage_events WHERE ` + w + ` GROUP BY client_name,client_version ORDER BY ` + order + `,client_name,client_version LIMIT ? OFFSET ?`
	} else if q.GroupBy == "knowledge_base" {
		w = strings.ReplaceAll(w, "occurred_at", "e.occurred_at")
		w = strings.ReplaceAll(w, "caller_tenant_id", "e.caller_tenant_id")
		selectSQL = `SELECT l.resource_id knowledge_base_id,COALESCE(kb.name,'[deleted]') knowledge_base_name,COUNT(*) calls,
			SUM(CASE WHEN e.success THEN 1 ELSE 0 END) successful_calls,COALESCE(100.0*SUM(CASE WHEN e.success THEN 1 ELSE 0 END)/NULLIF(COUNT(*),0),0) success_rate,
			AVG(e.latency_ms) average_latency_ms,SUM(CASE WHEN e.caller_tenant_id IS NULL THEN 1 ELSE 0 END) unattributed_calls,MAX(e.occurred_at) last_active,COUNT(*) OVER() total_count
			FROM usage_resource_links l JOIN mcp_usage_events e ON l.event_kind='mcp' AND l.event_id=e.id LEFT JOIN knowledge_bases kb ON kb.id=l.resource_id
			WHERE l.resource_type='knowledge_base' AND ` + w + ` GROUP BY l.resource_id,kb.name ORDER BY ` + order + `,l.resource_id LIMIT ? OFFSET ?`
	}
	args = append(args, size, (p-1)*size)
	var rows []types.MCPUsageRow
	err := r.db.WithContext(ctx).Raw(selectSQL, args...).Scan(&rows).Error
	var total int64
	if len(rows) > 0 {
		total = rows[0].TotalCount
	}
	return rows, total, err
}

func (r *usageAnalyticsRepository) KnowledgeBases(ctx context.Context, q types.UsageTimeRange) ([]types.KnowledgeBaseUsageRow, int64, error) {
	if q.GroupBy == "knowledge_base" {
		return r.knowledgeBaseGovernance(ctx, q)
	}
	p, size := page(q)
	orders := map[string]string{"calls": "mcp_calls DESC", "turns": "assistant_turns DESC", "last_active": "last_active DESC"}
	order := orders[q.Sort]
	if order == "" {
		order = orders["calls"]
	}
	modelWhere, ma := usageWhere("e.occurred_at", "e.tenant_id", q)
	mcpWhere, xa := mcpUsageWhere(q)
	mcpWhere = strings.ReplaceAll(mcpWhere, "caller_tenant_id", "e.caller_tenant_id")
	mcpWhere = strings.ReplaceAll(mcpWhere, "occurred_at", "e.occurred_at")
	if q.KnowledgeBaseID != "" {
		modelWhere += " AND l.resource_id = ?"
		ma = append(ma, q.KnowledgeBaseID)
		mcpWhere += " AND l.resource_id = ?"
		xa = append(xa, q.KnowledgeBaseID)
	}
	lastActiveProjection := "agg.last_active,0 last_active_unix"
	if r.db.Dialector.Name() == "sqlite" {
		lastActiveProjection = "NULL last_active,COALESCE(CAST(strftime('%s',agg.last_active) AS INTEGER),0) last_active_unix"
		if order == "last_active DESC" {
			order = "last_active_unix DESC"
		}
	}
	sql := `WITH hits AS (
		SELECT l.resource_id kb_id, e.tenant_id caller_tenant_id, COUNT(*) assistant_turns, 0 mcp_calls, MAX(e.occurred_at) last_active FROM usage_resource_links l JOIN model_usage_events e ON l.event_kind='model' AND l.event_id=e.id WHERE l.resource_type='knowledge_base' AND ` + modelWhere + ` GROUP BY l.resource_id,e.tenant_id
		UNION ALL SELECT l.resource_id, e.caller_tenant_id, 0, COUNT(*), MAX(e.occurred_at) FROM usage_resource_links l JOIN mcp_usage_events e ON l.event_kind='mcp' AND l.event_id=e.id WHERE l.resource_type='knowledge_base' AND ` + mcpWhere + ` GROUP BY l.resource_id,e.caller_tenant_id),
	 agg AS (SELECT kb_id, caller_tenant_id, SUM(assistant_turns) assistant_turns, SUM(mcp_calls) mcp_calls, MAX(last_active) last_active FROM hits GROUP BY kb_id,caller_tenant_id),
	 owners AS (SELECT resource_id kb_id, MAX(resource_tenant_id) owner_tenant_id FROM usage_resource_links WHERE resource_type='knowledge_base' GROUP BY resource_id)
	 SELECT agg.kb_id knowledge_base_id, COALESCE(kb.name,'[deleted]') knowledge_base_name, owners.owner_tenant_id,
		COALESCE(owner.name,'[deleted]') owner_tenant_name, agg.caller_tenant_id, COALESCE(caller.name,'') caller_tenant_name,
		agg.assistant_turns,agg.mcp_calls,` + lastActiveProjection + `,COUNT(*) OVER() total_count
	 FROM agg JOIN owners ON owners.kb_id=agg.kb_id
	 LEFT JOIN knowledge_bases kb ON kb.id=agg.kb_id LEFT JOIN tenants owner ON owner.id=owners.owner_tenant_id LEFT JOIN tenants caller ON caller.id=agg.caller_tenant_id
	 ORDER BY ` + order + `, knowledge_base_id LIMIT ? OFFSET ?`
	args := append(append([]any{}, ma...), xa...)
	args = append(args, size, (p-1)*size)
	var rows []types.KnowledgeBaseUsageRow
	err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error
	restoreSQLiteKBLastActive(rows)
	var total int64
	if len(rows) > 0 {
		total = rows[0].TotalCount
	}
	return rows, total, err
}

func (r *usageAnalyticsRepository) knowledgeBaseGovernance(ctx context.Context, q types.UsageTimeRange) ([]types.KnowledgeBaseUsageRow, int64, error) {
	p, size := page(q)
	orders := map[string]string{
		"accesses": "total_accesses DESC", "model_accesses": "model_accesses DESC", "mcp_accesses": "mcp_calls DESC",
		"unique_tenants": "unique_tenants DESC", "last_active": "last_active DESC", "recent_growth": "recent_growth DESC", "cross_tenant": "cross_tenant_accesses DESC",
	}
	order := orders[q.Sort]
	if order == "" {
		order = orders["unique_tenants"]
	}
	collectingSince := q.To
	if q.CollectingSince != nil {
		collectingSince = *q.CollectingSince
	}
	lastActiveProjection := "all_activity.last_active,0 last_active_unix"
	if r.db.Dialector.Name() == "sqlite" {
		lastActiveProjection = "NULL last_active,COALESCE(CAST(strftime('%s',all_activity.last_active) AS INTEGER),0) last_active_unix"
		if order == "last_active DESC" {
			order = "last_active_unix DESC"
		}
	}
	window := q.To.Sub(q.From)
	previousFrom, previousTo := q.From.Add(-window), q.From
	currentModelWhere, currentModelArgs := usageWhere("e.occurred_at", "e.tenant_id", q)
	currentMCPWhere, currentMCPArgs := mcpUsageWhere(q)
	currentMCPWhere = strings.ReplaceAll(currentMCPWhere, "occurred_at", "e.occurred_at")
	currentMCPWhere = strings.ReplaceAll(currentMCPWhere, "caller_tenant_id", "e.caller_tenant_id")
	sql := `WITH current_hits AS (
		SELECT l.resource_id kb_id,'model' event_kind,e.tenant_id caller_tenant_id,e.principal_type,e.principal_id,l.resource_tenant_id,e.occurred_at FROM usage_resource_links l JOIN model_usage_events e ON l.event_kind='model' AND l.event_id=e.id WHERE l.resource_type='knowledge_base' AND ` + currentModelWhere + `
		UNION ALL SELECT l.resource_id,'mcp',e.caller_tenant_id,e.principal_type,e.principal_id,l.resource_tenant_id,e.occurred_at FROM usage_resource_links l JOIN mcp_usage_events e ON l.event_kind='mcp' AND l.event_id=e.id WHERE l.resource_type='knowledge_base' AND ` + currentMCPWhere + `),
	 agg AS (SELECT kb_id,COUNT(*) total_accesses,SUM(CASE WHEN event_kind='model' THEN 1 ELSE 0 END) model_accesses,SUM(CASE WHEN event_kind='mcp' THEN 1 ELSE 0 END) mcp_calls,
		COUNT(DISTINCT caller_tenant_id) unique_tenants,COUNT(DISTINCT CASE WHEN principal_id IS NOT NULL AND principal_id<>'' AND principal_type<>? THEN principal_type||':'||principal_id END) unique_principals,
		COUNT(DISTINCT CASE WHEN caller_tenant_id IS NOT NULL AND caller_tenant_id<>resource_tenant_id THEN caller_tenant_id END) external_tenants,
		SUM(CASE WHEN caller_tenant_id IS NOT NULL AND caller_tenant_id<>resource_tenant_id THEN 1 ELSE 0 END) cross_tenant_accesses,MAX(occurred_at) last_active FROM current_hits GROUP BY kb_id),
	 previous AS (SELECT kb_id,COUNT(*) previous_accesses FROM (
		SELECT l.resource_id kb_id FROM usage_resource_links l JOIN model_usage_events e ON l.event_kind='model' AND l.event_id=e.id WHERE l.resource_type='knowledge_base' AND e.occurred_at>=? AND e.occurred_at<?
		UNION ALL SELECT l.resource_id FROM usage_resource_links l JOIN mcp_usage_events e ON l.event_kind='mcp' AND l.event_id=e.id WHERE l.resource_type='knowledge_base' AND e.occurred_at>=? AND e.occurred_at<?) p GROUP BY kb_id),
	 all_activity AS (SELECT kb_id,MAX(occurred_at) last_active FROM (
		SELECT l.resource_id kb_id,e.occurred_at FROM usage_resource_links l JOIN model_usage_events e ON l.event_kind='model' AND l.event_id=e.id WHERE l.resource_type='knowledge_base'
		UNION ALL SELECT l.resource_id,e.occurred_at FROM usage_resource_links l JOIN mcp_usage_events e ON l.event_kind='mcp' AND l.event_id=e.id WHERE l.resource_type='knowledge_base') a GROUP BY kb_id),
	 combined AS (SELECT kb.id knowledge_base_id,kb.name knowledge_base_name,kb.tenant_id owner_tenant_id,COALESCE(owner.name,'[deleted]') owner_tenant_name,
		COALESCE(agg.total_accesses,0) total_accesses,COALESCE(agg.model_accesses,0) model_accesses,COALESCE(agg.mcp_calls,0) mcp_calls,
		COALESCE(agg.unique_tenants,0) unique_tenants,COALESCE(agg.unique_principals,0) unique_principals,COALESCE(agg.external_tenants,0) external_tenants,
		COALESCE(agg.cross_tenant_accesses,0) cross_tenant_accesses,CASE WHEN COALESCE(agg.total_accesses,0)=0 THEN 0 ELSE 100.0*COALESCE(agg.cross_tenant_accesses,0)/agg.total_accesses END cross_tenant_share,
		` + lastActiveProjection + `,CASE WHEN COALESCE(previous.previous_accesses,0)=0 THEN CASE WHEN COALESCE(agg.total_accesses,0)>0 THEN 100.0 ELSE 0 END ELSE 100.0*(COALESCE(agg.total_accesses,0)-previous.previous_accesses)/previous.previous_accesses END recent_growth,
		CASE WHEN all_activity.last_active>=? THEN 'active' WHEN all_activity.last_active>=? THEN 'low_activity' WHEN ?>? THEN 'insufficient_data' WHEN all_activity.last_active IS NULL THEN 'never_used' ELSE 'inactive' END usage_status
		FROM knowledge_bases kb LEFT JOIN tenants owner ON owner.id=kb.tenant_id LEFT JOIN agg ON agg.kb_id=kb.id LEFT JOIN previous ON previous.kb_id=kb.id LEFT JOIN all_activity ON all_activity.kb_id=kb.id WHERE kb.deleted_at IS NULL),
	 filtered AS (SELECT * FROM combined WHERE 1=1`
	args := append([]any{}, currentModelArgs...)
	args = append(args, currentMCPArgs...)
	args = append(args, types.UsagePrincipalUnknown, previousFrom, previousTo, previousFrom, previousTo,
		q.To.AddDate(0, 0, -30), q.To.AddDate(0, 0, -90), collectingSince, q.To.AddDate(0, 0, -90))
	if q.KnowledgeBaseID != "" {
		sql += ` AND knowledge_base_id=?`
		args = append(args, q.KnowledgeBaseID)
	}
	if q.OwnerTenantID != nil {
		sql += ` AND owner_tenant_id=?`
		args = append(args, *q.OwnerTenantID)
	}
	if q.Status != "" {
		sql += ` AND usage_status=?`
		args = append(args, q.Status)
	}
	if q.CrossTenant == "with" {
		sql += ` AND cross_tenant_accesses>0`
	} else if q.CrossTenant == "without" {
		sql += ` AND cross_tenant_accesses=0`
	}
	sql += `) SELECT *,COUNT(*) OVER() total_count FROM filtered ORDER BY ` + order + `,knowledge_base_id LIMIT ? OFFSET ?`
	args = append(args, size, (p-1)*size)
	var rows []types.KnowledgeBaseUsageRow
	err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error
	restoreSQLiteKBLastActive(rows)
	var total int64
	if len(rows) > 0 {
		total = rows[0].TotalCount
	}
	return rows, total, err
}

func restoreSQLiteLastActive(rows []types.TenantUsageRow) {
	for i := range rows {
		if rows[i].LastActive == nil && rows[i].LastActiveUnix > 0 {
			value := time.Unix(rows[i].LastActiveUnix, 0).UTC()
			rows[i].LastActive = &value
		}
	}
}

func restoreSQLiteKBLastActive(rows []types.KnowledgeBaseUsageRow) {
	for i := range rows {
		if rows[i].LastActive == nil && rows[i].LastActiveUnix > 0 {
			value := time.Unix(rows[i].LastActiveUnix, 0).UTC()
			rows[i].LastActive = &value
		}
	}
}

func (r *usageAnalyticsRepository) CollectingSince(ctx context.Context) (*time.Time, error) {
	var setting types.SystemSetting
	err := r.db.WithContext(ctx).Where("key = ?", usageCollectingSinceKey).First(&setting).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	raw, err := setting.AsString()
	if err != nil {
		return nil, err
	}
	started, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil, err
	}
	return &started, nil
}

var _ interfaces.UsageAnalyticsRepository = (*usageAnalyticsRepository)(nil)
