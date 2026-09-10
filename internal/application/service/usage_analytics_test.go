package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

type usageRepoStub struct {
	model              *types.ModelUsageEvent
	mcp                *types.MCPUsageEvent
	modelHit           int
	mcpHit             int
	links              []types.UsageResourceLink
	resolvedKBs        []string
	resolvedKnowledges []string
	modelErr           error
}

func (r *usageRepoStub) Operations(context.Context, types.UsageTimeRange) ([]types.OperationUsageRow, error) {
	return nil, nil
}

func (r *usageRepoStub) RecordModelUsage(_ context.Context, event *types.ModelUsageEvent, links []types.UsageResourceLink) (bool, error) {
	r.modelHit++
	if r.modelErr != nil {
		return false, r.modelErr
	}
	copy := *event
	r.model, r.links = &copy, links
	return r.modelHit == 1, nil
}
func (r *usageRepoStub) RecordMCPUsage(_ context.Context, event *types.MCPUsageEvent, links []types.UsageResourceLink) (bool, error) {
	r.mcpHit++
	copy := *event
	r.mcp, r.links = &copy, links
	return r.mcpHit == 1, nil
}
func (r *usageRepoStub) ResolveUsageResources(_ context.Context, kbIDs, knowledgeIDs []string) ([]types.UsageResourceLink, error) {
	r.resolvedKBs = append([]string(nil), kbIDs...)
	r.resolvedKnowledges = append([]string(nil), knowledgeIDs...)
	return nil, nil
}
func (r *usageRepoStub) Overview(context.Context, types.UsageTimeRange) (*types.UsageOverview, error) {
	return &types.UsageOverview{}, nil
}
func (r *usageRepoStub) Tenants(context.Context, types.UsageTimeRange) ([]types.TenantUsageRow, int64, error) {
	return nil, 0, nil
}
func (r *usageRepoStub) TimeSeries(context.Context, types.UsageTimeRange) ([]types.UsageTimeSeriesPoint, error) {
	return nil, nil
}
func (r *usageRepoStub) Models(context.Context, types.UsageTimeRange) ([]types.ModelUsageRow, int64, error) {
	return nil, 0, nil
}
func (r *usageRepoStub) MCP(context.Context, types.UsageTimeRange) ([]types.MCPUsageRow, int64, error) {
	return nil, 0, nil
}
func (r *usageRepoStub) KnowledgeBases(context.Context, types.UsageTimeRange) ([]types.KnowledgeBaseUsageRow, int64, error) {
	return nil, 0, nil
}
func (r *usageRepoStub) CollectingSince(context.Context) (*time.Time, error) { return nil, nil }

func TestRecordAssistantTurnPreservesProviderTotalAndCacheBreakdown(t *testing.T) {
	repo := &usageRepoStub{}
	svc := NewUsageAnalyticsService(repo)
	ctx := types.WithPrincipal(context.Background(), types.Principal{Type: types.PrincipalWebUser, ID: "user-1"})
	message := &types.Message{
		ID: "assistant-1", SessionID: "session-1", Channel: "api", ModelID: "model-1",
		Usage: &types.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 20, CacheReadTokens: 7, CacheWriteTokens: 3},
	}
	if err := svc.RecordAssistantTurn(ctx, 42, message); err != nil {
		t.Fatal(err)
	}
	if repo.modelHit != 1 {
		t.Fatalf("record count = %d", repo.modelHit)
	}
	if repo.model.EventKey != "message:assistant-1" || repo.model.TenantID != 42 {
		t.Fatalf("bad identity: %#v", repo.model)
	}
	if repo.model.TotalTokens != 20 {
		t.Fatalf("total = %d, want provider total 20", repo.model.TotalTokens)
	}
	if repo.model.CacheReadTokens != 7 || repo.model.CacheWriteTokens != 3 {
		t.Fatalf("cache breakdown lost: %#v", repo.model)
	}
	if repo.model.PrincipalType != types.PrincipalWebUser || repo.model.PrincipalID != "user-1" {
		t.Fatalf("bad principal: %#v", repo.model)
	}
}

func TestRecordAssistantTurnFallbackTotalAndNoUsage(t *testing.T) {
	repo := &usageRepoStub{}
	svc := NewUsageAnalyticsService(repo)
	if err := svc.RecordAssistantTurn(context.Background(), 9, &types.Message{ID: "none"}); err != nil {
		t.Fatal(err)
	}
	if repo.modelHit != 0 {
		t.Fatal("missing usage must not create an estimated event")
	}
	message := &types.Message{ID: "assistant-2", Usage: &types.TokenUsage{PromptTokens: 11, CompletionTokens: 4, CacheReadTokens: 8}}
	if err := svc.RecordAssistantTurn(context.Background(), 9, message); err != nil {
		t.Fatal(err)
	}
	if repo.model.TotalTokens != 15 {
		t.Fatalf("total = %d, want prompt+completion only", repo.model.TotalTokens)
	}
}

func TestRecordInboundMCPUsesAuthenticatedCallerAndSharedIsUnattributed(t *testing.T) {
	repo := &usageRepoStub{}
	svc := NewUsageAnalyticsService(repo)
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 17, UserID: "u"})
	ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalAPITenant, ID: "key-principal"})
	if err := svc.RecordInboundMCP(ctx, types.MCPUsageReport{EventKey: "mcp:1", ToolName: "search", Success: true, LatencyMs: 12}); err != nil {
		t.Fatal(err)
	}
	if repo.mcp.CallerTenantID == nil || *repo.mcp.CallerTenantID != 17 {
		t.Fatalf("caller tenant not derived from context: %#v", repo.mcp)
	}
	if repo.mcp.PrincipalID != "key-principal" {
		t.Fatalf("principal not derived from context: %#v", repo.mcp)
	}
	if err := svc.RecordInboundMCP(ctx, types.MCPUsageReport{EventKey: "mcp:2", ToolName: "search", SharedGateway: true}); err != nil {
		t.Fatal(err)
	}
	if repo.mcp.CallerTenantID != nil || repo.mcp.PrincipalType != types.UsagePrincipalUnknown || repo.mcp.PrincipalID != "" {
		t.Fatalf("shared call must be unattributed: %#v", repo.mcp)
	}
}

func TestRecordModelInvocationUsesFrozenCallerAndProviderUsage(t *testing.T) {
	repo := &usageRepoStub{}
	svc := NewUsageAnalyticsService(repo)
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 17, UserID: "caller"})
	ctx = types.WithExecutionTenant(ctx, 99)
	err := svc.RecordModelInvocation(ctx, types.ModelUsageRecordRequest{
		EventKey: "query_rewrite:stable", Operation: types.ModelUsageOperationQueryRewrite,
		ModelID: "chat-1", ModelType: string(types.ModelTypeKnowledgeQA),
		Usage:            types.TokenUsage{PromptTokens: 13, CompletionTokens: 5, TotalTokens: 18, CacheReadTokens: 4},
		KnowledgeBaseIDs: []string{"kb-1"}, KnowledgeIDs: []string{"doc-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if repo.model.TenantID != 17 || repo.model.Operation != types.ModelUsageOperationQueryRewrite {
		t.Fatalf("bad background attribution: %#v", repo.model)
	}
	if repo.model.TotalTokens != 18 || repo.model.CacheReadTokens != 4 {
		t.Fatalf("provider usage changed: %#v", repo.model)
	}
	if len(repo.resolvedKBs) != 1 || len(repo.resolvedKnowledges) != 1 {
		t.Fatalf("resource links not resolved: kb=%v knowledge=%v", repo.resolvedKBs, repo.resolvedKnowledges)
	}
}

func TestRecordModelInvocationSkipsMissingProviderUsage(t *testing.T) {
	repo := &usageRepoStub{}
	svc := NewUsageAnalyticsService(repo)
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 17})
	if err := svc.RecordModelInvocation(ctx, types.ModelUsageRecordRequest{
		EventKey: "summary:stable", Operation: types.ModelUsageOperationDocumentSummary,
	}); err != nil {
		t.Fatal(err)
	}
	if repo.modelHit != 0 {
		t.Fatal("missing provider usage must not be represented as zero tokens")
	}
}

func TestRecordOutboundMCPUsesSafeLogicalInvocationFields(t *testing.T) {
	repo := &usageRepoStub{}
	svc := NewUsageAnalyticsService(repo)
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 23})
	err := svc.RecordOutboundMCP(ctx, types.MCPUsageRecordRequest{
		EventKey: "mcp-outbound:stable", MCPServiceID: "service-1", ToolName: "search",
		Transport: "http", Success: false, ErrorCode: "tool_error", LatencyMs: 44,
	})
	if err != nil {
		t.Fatal(err)
	}
	if repo.mcp.Direction != "outbound" || repo.mcp.MCPServiceID != "service-1" || repo.mcp.Success {
		t.Fatalf("bad outbound event: %#v", repo.mcp)
	}
	if repo.mcp.CallerTenantID == nil || *repo.mcp.CallerTenantID != 23 {
		t.Fatalf("bad outbound caller: %#v", repo.mcp)
	}
	if repo.mcp.ErrorCode != "tool_error" || repo.mcp.LatencyMs != 44 {
		t.Fatalf("bad outbound result: %#v", repo.mcp)
	}
}

func TestUsageRecorderErrorCanBeIgnoredByCallBoundary(t *testing.T) {
	repo := &usageRepoStub{modelErr: errors.New("ledger unavailable")}
	svc := NewUsageAnalyticsService(repo)
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 1})
	err := svc.RecordModelInvocation(ctx, types.ModelUsageRecordRequest{
		EventKey: "query_rewrite:stable", Operation: types.ModelUsageOperationQueryRewrite,
		Usage: types.TokenUsage{TotalTokens: 1},
	})
	if err == nil {
		t.Fatal("recorder must surface persistence errors to its best-effort caller")
	}
}
