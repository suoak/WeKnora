package interfaces

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

type UsageAnalyticsRepository interface {
	RecordModelUsage(ctx context.Context, event *types.ModelUsageEvent, resources []types.UsageResourceLink) (bool, error)
	RecordMCPUsage(ctx context.Context, event *types.MCPUsageEvent, resources []types.UsageResourceLink) (bool, error)
	ResolveUsageResources(ctx context.Context, knowledgeBaseIDs, knowledgeIDs []string) ([]types.UsageResourceLink, error)
	Overview(ctx context.Context, q types.UsageTimeRange) (*types.UsageOverview, error)
	Tenants(ctx context.Context, q types.UsageTimeRange) ([]types.TenantUsageRow, int64, error)
	TimeSeries(ctx context.Context, q types.UsageTimeRange) ([]types.UsageTimeSeriesPoint, error)
	Models(ctx context.Context, q types.UsageTimeRange) ([]types.ModelUsageRow, int64, error)
	MCP(ctx context.Context, q types.UsageTimeRange) ([]types.MCPUsageRow, int64, error)
	KnowledgeBases(ctx context.Context, q types.UsageTimeRange) ([]types.KnowledgeBaseUsageRow, int64, error)
	CollectingSince(ctx context.Context) (*time.Time, error)
}

type UsageAnalyticsService interface {
	RecordAssistantTurn(ctx context.Context, tenantID uint64, message *types.Message) error
	RecordInboundMCP(ctx context.Context, report types.MCPUsageReport) error
	Overview(ctx context.Context, q types.UsageTimeRange) (*types.UsageOverview, error)
	Tenants(ctx context.Context, q types.UsageTimeRange) (*types.UsagePage[types.TenantUsageRow], error)
	TimeSeries(ctx context.Context, q types.UsageTimeRange) (*types.UsagePage[types.UsageTimeSeriesPoint], error)
	Models(ctx context.Context, q types.UsageTimeRange) (*types.UsagePage[types.ModelUsageRow], error)
	MCP(ctx context.Context, q types.UsageTimeRange) (*types.UsagePage[types.MCPUsageRow], error)
	KnowledgeBases(ctx context.Context, q types.UsageTimeRange) (*types.UsagePage[types.KnowledgeBaseUsageRow], error)
}
