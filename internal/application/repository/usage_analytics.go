package repository

import (
	"context"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type usageAnalyticsRepository struct{ db *gorm.DB }

func NewUsageAnalyticsRepository(db *gorm.DB) interfaces.UsageAnalyticsRepository {
	return &usageAnalyticsRepository{db: db}
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
	return where, args
}

func mcpUsageWhere(q types.UsageTimeRange) (string, []any) {
	where, args := "occurred_at >= ? AND occurred_at < ?", []any{q.From, q.To}
	if q.TenantID != nil {
		where += " AND caller_tenant_id = ?"
		args = append(args, *q.TenantID)
	}
	return where, args
}

func (r *usageAnalyticsRepository) Overview(ctx context.Context, q types.UsageTimeRange) (*types.UsageOverview, error) {
	out := &types.UsageOverview{}
	mw, ma := usageWhere("occurred_at", "tenant_id", q)
	if err := r.db.WithContext(ctx).Raw(`SELECT COALESCE(SUM(total_tokens),0) total_tokens, COALESCE(SUM(input_tokens),0) input_tokens,
		COALESCE(SUM(output_tokens),0) output_tokens, COALESCE(SUM(cache_read_tokens),0) cache_read_tokens,
		COALESCE(SUM(cache_write_tokens),0) cache_write_tokens, COUNT(*) assistant_turns,
		COUNT(DISTINCT tenant_id) active_tenants FROM model_usage_events WHERE `+mw, ma...).Scan(out).Error; err != nil {
		return nil, err
	}
	mcpw, mcpa := mcpUsageWhere(q)
	if err := r.db.WithContext(ctx).Raw(`SELECT COUNT(*) mcp_calls,
		COALESCE(100.0 * SUM(CASE WHEN success THEN 1 ELSE 0 END) / NULLIF(COUNT(*),0),0) mcp_success_rate,
		COALESCE(AVG(latency_ms),0) mcp_average_latency_ms,
		SUM(CASE WHEN caller_tenant_id IS NULL THEN 1 ELSE 0 END) unattributed_mcp_calls
		FROM mcp_usage_events WHERE `+mcpw, mcpa...).Scan(out).Error; err != nil {
		return nil, err
	}
	args := append(append([]any{}, ma...), mcpa...)
	activeSQL := `SELECT COUNT(*) active_principals FROM (
		SELECT tenant_id, principal_type, principal_id FROM model_usage_events WHERE ` + mw + ` GROUP BY tenant_id, principal_type, principal_id
		UNION SELECT caller_tenant_id, principal_type, principal_id FROM mcp_usage_events WHERE ` + mcpw + ` GROUP BY caller_tenant_id, principal_type, principal_id) p`
	if err := r.db.WithContext(ctx).Raw(activeSQL, args...).Scan(&out.ActivePrincipals).Error; err != nil {
		return nil, err
	}
	activeTenantSQL := `SELECT COUNT(*) FROM (
		SELECT tenant_id FROM model_usage_events WHERE ` + mw + ` GROUP BY tenant_id
		UNION SELECT caller_tenant_id FROM mcp_usage_events WHERE ` + mcpw + ` AND caller_tenant_id IS NOT NULL GROUP BY caller_tenant_id) t`
	if err := r.db.WithContext(ctx).Raw(activeTenantSQL, args...).Scan(&out.ActiveTenants).Error; err != nil {
		return nil, err
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
	orders := map[string]string{"tokens": "total_tokens DESC", "mcp_calls": "mcp_calls DESC", "active_principals": "active_principals DESC", "last_active": "last_active DESC"}
	order := orders[q.Sort]
	if order == "" {
		order = orders["tokens"]
	}
	sql := `WITH model AS (SELECT tenant_id, COUNT(*) assistant_turns, SUM(CASE WHEN operation='agent_turn' THEN 1 ELSE 0 END) agent_turns,
		SUM(input_tokens) input_tokens, SUM(output_tokens) output_tokens, SUM(total_tokens) total_tokens, MAX(occurred_at) last_active
		FROM model_usage_events WHERE ` + mw + ` GROUP BY tenant_id),
	 mcp AS (SELECT caller_tenant_id tenant_id, COUNT(*) mcp_calls, 100.0*SUM(CASE WHEN success THEN 1 ELSE 0 END)/COUNT(*) mcp_success_rate,
		AVG(latency_ms) mcp_avg_latency_ms, MAX(occurred_at) last_active FROM mcp_usage_events WHERE ` + xw + ` AND caller_tenant_id IS NOT NULL GROUP BY caller_tenant_id),
	 principals AS (SELECT tenant_id, COUNT(*) active_principals FROM (SELECT tenant_id, principal_type, principal_id FROM model_usage_events WHERE ` + mw + `
		UNION SELECT caller_tenant_id, principal_type, principal_id FROM mcp_usage_events WHERE ` + xw + ` AND caller_tenant_id IS NOT NULL) u GROUP BY tenant_id),
	 combined AS (SELECT t.id tenant_id, t.name tenant_name, COALESCE(p.active_principals,0) active_principals,
		COALESCE(model.assistant_turns,0) assistant_turns, COALESCE(model.agent_turns,0) agent_turns,
		COALESCE(model.input_tokens,0) input_tokens, COALESCE(model.output_tokens,0) output_tokens, COALESCE(model.total_tokens,0) total_tokens,
		COALESCE(mcp.mcp_calls,0) mcp_calls, COALESCE(mcp.mcp_success_rate,0) mcp_success_rate, COALESCE(mcp.mcp_avg_latency_ms,0) mcp_avg_latency_ms,
		CASE WHEN model.last_active IS NULL THEN mcp.last_active WHEN mcp.last_active IS NULL OR model.last_active >= mcp.last_active THEN model.last_active ELSE mcp.last_active END last_active
		FROM tenants t LEFT JOIN model ON model.tenant_id=t.id LEFT JOIN mcp ON mcp.tenant_id=t.id LEFT JOIN principals p ON p.tenant_id=t.id
		WHERE model.tenant_id IS NOT NULL OR mcp.tenant_id IS NOT NULL)
	 SELECT *, COUNT(*) OVER() total_count FROM combined ORDER BY ` + order + `, tenant_id LIMIT ? OFFSET ?`
	args := append(append(append(append([]any{}, ma...), xa...), ma...), xa...)
	args = append(args, size, (p-1)*size)
	var rows []types.TenantUsageRow
	if err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
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
	sql := `SELECT bucket, SUM(input_tokens) input_tokens, SUM(output_tokens) output_tokens, SUM(total_tokens) total_tokens,
		SUM(assistant_turns) assistant_turns, SUM(mcp_calls) mcp_calls FROM (
		SELECT ` + bucket + ` bucket, SUM(input_tokens) input_tokens, SUM(output_tokens) output_tokens, SUM(total_tokens) total_tokens, COUNT(*) assistant_turns, 0 mcp_calls FROM model_usage_events WHERE ` + mw + ` GROUP BY bucket
		UNION ALL SELECT ` + bucket + ` bucket, 0,0,0,0,COUNT(*) FROM mcp_usage_events WHERE ` + xw + ` GROUP BY bucket) s GROUP BY bucket ORDER BY bucket`
	args := append(append([]any{}, ma...), xa...)
	var rows []types.UsageTimeSeriesPoint
	err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error
	return rows, err
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

func (r *usageAnalyticsRepository) MCP(ctx context.Context, q types.UsageTimeRange) ([]types.MCPUsageRow, int64, error) {
	w, args := mcpUsageWhere(q)
	p, size := page(q)
	orders := map[string]string{"calls": "calls DESC", "latency": "average_latency_ms DESC", "last_active": "last_active DESC"}
	order := orders[q.Sort]
	if order == "" {
		order = orders["calls"]
	}
	sql := `SELECT tool_name, COUNT(*) calls, SUM(CASE WHEN success THEN 1 ELSE 0 END) successful_calls,
		COALESCE(100.0*SUM(CASE WHEN success THEN 1 ELSE 0 END)/NULLIF(COUNT(*),0),0) success_rate, AVG(latency_ms) average_latency_ms,
		SUM(CASE WHEN caller_tenant_id IS NULL THEN 1 ELSE 0 END) unattributed_calls, MAX(occurred_at) last_active, COUNT(*) OVER() total_count
		FROM mcp_usage_events WHERE ` + w + ` GROUP BY tool_name ORDER BY ` + order + `, tool_name LIMIT ? OFFSET ?`
	args = append(args, size, (p-1)*size)
	var rows []types.MCPUsageRow
	err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error
	var total int64
	if len(rows) > 0 {
		total = rows[0].TotalCount
	}
	return rows, total, err
}

func (r *usageAnalyticsRepository) KnowledgeBases(ctx context.Context, q types.UsageTimeRange) ([]types.KnowledgeBaseUsageRow, int64, error) {
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
	sql := `WITH hits AS (
		SELECT l.resource_id kb_id, e.tenant_id caller_tenant_id, COUNT(*) assistant_turns, 0 mcp_calls, MAX(e.occurred_at) last_active FROM usage_resource_links l JOIN model_usage_events e ON l.event_kind='model' AND l.event_id=e.id WHERE l.resource_type='knowledge_base' AND ` + modelWhere + ` GROUP BY l.resource_id,e.tenant_id
		UNION ALL SELECT l.resource_id, e.caller_tenant_id, 0, COUNT(*), MAX(e.occurred_at) FROM usage_resource_links l JOIN mcp_usage_events e ON l.event_kind='mcp' AND l.event_id=e.id WHERE l.resource_type='knowledge_base' AND ` + mcpWhere + ` GROUP BY l.resource_id,e.caller_tenant_id),
	 agg AS (SELECT kb_id, caller_tenant_id, SUM(assistant_turns) assistant_turns, SUM(mcp_calls) mcp_calls, MAX(last_active) last_active FROM hits GROUP BY kb_id,caller_tenant_id),
	 owners AS (SELECT resource_id kb_id, MAX(resource_tenant_id) owner_tenant_id FROM usage_resource_links WHERE resource_type='knowledge_base' GROUP BY resource_id)
	 SELECT agg.kb_id knowledge_base_id, COALESCE(kb.name,'[deleted]') knowledge_base_name, owners.owner_tenant_id,
		COALESCE(owner.name,'[deleted]') owner_tenant_name, agg.caller_tenant_id, COALESCE(caller.name,'') caller_tenant_name,
		agg.assistant_turns,agg.mcp_calls,agg.last_active,COUNT(*) OVER() total_count
	 FROM agg JOIN owners ON owners.kb_id=agg.kb_id
	 LEFT JOIN knowledge_bases kb ON kb.id=agg.kb_id LEFT JOIN tenants owner ON owner.id=owners.owner_tenant_id LEFT JOIN tenants caller ON caller.id=agg.caller_tenant_id
	 ORDER BY ` + order + `, knowledge_base_id LIMIT ? OFFSET ?`
	args := append(append([]any{}, ma...), xa...)
	args = append(args, size, (p-1)*size)
	var rows []types.KnowledgeBaseUsageRow
	err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error
	var total int64
	if len(rows) > 0 {
		total = rows[0].TotalCount
	}
	return rows, total, err
}

func (r *usageAnalyticsRepository) CollectingSince(ctx context.Context) (*time.Time, error) {
	var row struct{ Started *time.Time }
	err := r.db.WithContext(ctx).Raw(`SELECT MIN(started) started FROM (SELECT MIN(created_at) started FROM model_usage_events UNION ALL SELECT MIN(created_at) FROM mcp_usage_events) s`).Scan(&row).Error
	return row.Started, err
}

var _ interfaces.UsageAnalyticsRepository = (*usageAnalyticsRepository)(nil)
