package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const maxUsageIdentifierLength = 255

type usageAnalyticsService struct {
	repo interfaces.UsageAnalyticsRepository
}

func NewUsageAnalyticsService(repo interfaces.UsageAnalyticsRepository) interfaces.UsageAnalyticsService {
	return &usageAnalyticsService{repo: repo}
}

// InitializeUsageAnalytics persists the collection epoch once. The value is
// intentionally independent from the first business event, which may arrive
// long after collection became available.
func InitializeUsageAnalytics(repo interfaces.UsageAnalyticsRepository) error {
	return repo.EnsureCollectingSince(context.Background(), time.Now())
}

func (s *usageAnalyticsService) RecordAssistantTurn(ctx context.Context, tenantID uint64, message *types.Message) error {
	if message == nil || message.Usage == nil || message.ID == "" || tenantID == 0 {
		return nil
	}
	u := message.Usage
	total := int64(u.TotalTokens)
	if total <= 0 {
		total = int64(u.PromptTokens + u.CompletionTokens)
	}
	if total <= 0 {
		return nil
	}
	principalType, principalID := types.UsagePrincipalUnknown, ""
	if principal, ok := types.PrincipalFromContext(ctx); ok {
		principalType, principalID = principal.Type, principal.ID
	}
	operation, source := types.ModelUsageOperationKnowledgeQA, "provider"
	if message.AgentID != "" {
		operation, source = types.ModelUsageOperationAgent, "aggregated"
	}
	channel := strings.TrimSpace(message.Channel)
	if channel == "" {
		channel = "web"
	}
	occurred := message.UpdatedAt
	if occurred.IsZero() {
		occurred = time.Now()
	}
	event := &types.ModelUsageEvent{
		EventKey: "message:" + message.ID, TenantID: tenantID,
		PrincipalType: principalType, PrincipalID: principalID, Channel: channel,
		Operation: operation, ModelID: message.ModelID, ModelType: string(types.ModelTypeKnowledgeQA),
		SessionID: message.SessionID, MessageID: message.ID,
		InputTokens: int64(max(0, u.PromptTokens)), OutputTokens: int64(max(0, u.CompletionTokens)), TotalTokens: total,
		CacheReadTokens: int64(max(0, u.CacheReadTokens)), CacheWriteTokens: int64(max(0, u.CacheWriteTokens)),
		ReasoningTokens: 0, UsageSource: source, Status: "completed", RequestID: message.RequestID,
		TraceID: message.ExecutionContext.LangfuseTraceparent, OccurredAt: occurred, CreatedAt: time.Now(),
	}
	kbIDs := append([]string(nil), message.ExecutionContext.KnowledgeBaseIDs...)
	knowledgeIDs := append([]string(nil), message.ExecutionContext.KnowledgeIDs...)
	for _, ref := range message.KnowledgeReferences {
		if ref == nil {
			continue
		}
		kbIDs = append(kbIDs, ref.KnowledgeBaseID)
		knowledgeIDs = append(knowledgeIDs, ref.KnowledgeID)
	}
	resources, err := s.repo.ResolveUsageResources(ctx, kbIDs, knowledgeIDs)
	if err != nil {
		return err
	}
	_, err = s.repo.RecordModelUsage(ctx, event, resources)
	return err
}

func (s *usageAnalyticsService) RecordModelInvocation(ctx context.Context, request types.ModelUsageRecordRequest) error {
	request.EventKey = strings.TrimSpace(request.EventKey)
	request.Operation = strings.TrimSpace(request.Operation)
	if request.EventKey == "" || len(request.EventKey) > maxUsageIdentifierLength || request.Operation == "" || len(request.Operation) > 64 {
		return errors.New("invalid model usage identity")
	}
	tenantID := request.TenantID
	if tenantID == 0 {
		tenantID = types.CallerFromContext(ctx).TenantID
	}
	if tenantID == 0 {
		return nil
	}
	u := request.Usage
	total := int64(u.TotalTokens)
	if total <= 0 {
		total = int64(u.PromptTokens + u.CompletionTokens)
	}
	if total <= 0 {
		return nil
	}
	principalType, principalID := types.UsagePrincipalUnknown, ""
	if principal, ok := types.PrincipalFromContext(ctx); ok {
		principalType, principalID = principal.Type, principal.ID
	}
	channel := strings.TrimSpace(request.Channel)
	if channel == "" {
		channel = types.UsageClassBackground
	}
	status := strings.TrimSpace(request.Status)
	if status == "" {
		status = "completed"
	}
	source := strings.TrimSpace(request.UsageSource)
	if source == "" {
		source = "provider"
	}
	requestID := request.RequestID
	if requestID == "" {
		requestID, _ = types.RequestIDFromContext(ctx)
	}
	now := time.Now()
	event := &types.ModelUsageEvent{
		EventKey: request.EventKey, TenantID: tenantID, PrincipalType: principalType, PrincipalID: principalID,
		Channel: channel, Operation: request.Operation, ModelID: request.ModelID, ModelType: request.ModelType,
		SessionID: request.SessionID, MessageID: request.MessageID,
		InputTokens: int64(max(0, u.PromptTokens)), OutputTokens: int64(max(0, u.CompletionTokens)), TotalTokens: total,
		CacheReadTokens: int64(max(0, u.CacheReadTokens)), CacheWriteTokens: int64(max(0, u.CacheWriteTokens)),
		UsageSource: source, Status: status, RequestID: requestID, TraceID: request.TraceID,
		OccurredAt: now, CreatedAt: now,
	}
	resources, err := s.repo.ResolveUsageResources(ctx, request.KnowledgeBaseIDs, request.KnowledgeIDs)
	if err != nil {
		return err
	}
	_, err = s.repo.RecordModelUsage(ctx, event, resources)
	return err
}

func (s *usageAnalyticsService) RecordInboundMCP(ctx context.Context, report types.MCPUsageReport) error {
	if err := validateMCPUsageReport(report); err != nil {
		return err
	}
	principalType, principalID := types.UsagePrincipalUnknown, ""
	var tenantID, keyID *uint64
	if !report.SharedGateway {
		caller := types.CallerFromContext(ctx)
		if caller.TenantID > 0 {
			value := caller.TenantID
			tenantID = &value
		}
		if principal, ok := types.PrincipalFromContext(ctx); ok {
			principalType, principalID = principal.Type, principal.ID
		}
		if scope, ok := types.TenantAPIKeyScopeFromContext(ctx); ok && scope.KeyID > 0 {
			value := scope.KeyID
			keyID = &value
		}
	}
	now := time.Now()
	requestID := report.RequestID
	if requestID == "" {
		requestID, _ = types.RequestIDFromContext(ctx)
	}
	event := &types.MCPUsageEvent{
		EventKey: report.EventKey, CallerTenantID: tenantID, PrincipalType: principalType, PrincipalID: principalID, APIKeyID: keyID,
		Direction: "inbound", ToolName: report.ToolName, ClientName: report.ClientName, ClientVersion: report.ClientVersion,
		Transport: report.Transport, Success: report.Success, ErrorCode: report.ErrorCode, LatencyMs: report.LatencyMs,
		RequestID: requestID, OccurredAt: now, CreatedAt: now,
	}
	resources, err := s.repo.ResolveUsageResources(ctx, report.KnowledgeBaseIDs, report.KnowledgeIDs)
	if err != nil {
		return err
	}
	_, err = s.repo.RecordMCPUsage(ctx, event, resources)
	return err
}

func (s *usageAnalyticsService) RecordOutboundMCP(ctx context.Context, request types.MCPUsageRecordRequest) error {
	report := types.MCPUsageReport{EventKey: request.EventKey, ToolName: request.ToolName, Transport: request.Transport,
		Success: request.Success, ErrorCode: request.ErrorCode, LatencyMs: request.LatencyMs, RequestID: request.RequestID}
	if err := validateMCPUsageReport(report); err != nil {
		return err
	}
	principalType, principalID := types.UsagePrincipalUnknown, ""
	caller := types.CallerFromContext(ctx)
	var tenantID *uint64
	if caller.TenantID > 0 {
		value := caller.TenantID
		tenantID = &value
	}
	if principal, ok := types.PrincipalFromContext(ctx); ok {
		principalType, principalID = principal.Type, principal.ID
	}
	requestID := request.RequestID
	if requestID == "" {
		requestID, _ = types.RequestIDFromContext(ctx)
	}
	now := time.Now()
	event := &types.MCPUsageEvent{
		EventKey: request.EventKey, CallerTenantID: tenantID, PrincipalType: principalType, PrincipalID: principalID,
		Direction: "outbound", MCPServiceID: request.MCPServiceID, ToolName: request.ToolName, Transport: request.Transport,
		Success: request.Success, ErrorCode: request.ErrorCode, LatencyMs: request.LatencyMs,
		RequestID: requestID, TraceID: request.TraceID, OccurredAt: now, CreatedAt: now,
	}
	resources, err := s.repo.ResolveUsageResources(ctx, request.KnowledgeBaseIDs, request.KnowledgeIDs)
	if err != nil {
		return err
	}
	_, err = s.repo.RecordMCPUsage(ctx, event, resources)
	return err
}

func validateMCPUsageReport(r types.MCPUsageReport) error {
	if strings.TrimSpace(r.EventKey) == "" || len(r.EventKey) > maxUsageIdentifierLength {
		return errors.New("invalid event_key")
	}
	if strings.TrimSpace(r.ToolName) == "" || len(r.ToolName) > maxUsageIdentifierLength {
		return errors.New("invalid tool_name")
	}
	if r.LatencyMs < 0 {
		return errors.New("invalid latency_ms")
	}
	for _, value := range []string{r.EventKey, r.ToolName, r.ClientName, r.ClientVersion, r.Transport, r.ErrorCode, r.RequestID} {
		if strings.ContainsAny(value, "\r\n\x00") {
			return errors.New("usage metadata contains control characters")
		}
	}
	if len(r.ClientName) > 128 || len(r.ClientVersion) > 64 || len(r.Transport) > 16 || len(r.ErrorCode) > 64 {
		return errors.New("usage metadata is too long")
	}
	return nil
}

func normalizeUsageQuery(q types.UsageTimeRange) types.UsageTimeRange {
	now := time.Now()
	if q.To.IsZero() {
		q.To = now
	}
	if q.From.IsZero() {
		q.From = q.To.AddDate(0, 0, -30)
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 {
		q.PageSize = 20
	}
	if q.PageSize > 100 {
		q.PageSize = 100
	}
	if q.Interval == "" {
		q.Interval = "day"
	}
	return q
}

func (s *usageAnalyticsService) prepareUsageQuery(ctx context.Context, q types.UsageTimeRange) (types.UsageTimeRange, error) {
	q = normalizeUsageQuery(q)
	since, err := s.repo.CollectingSince(ctx)
	if err != nil {
		return q, err
	}
	q.CollectingSince = since
	return q, nil
}

func (s *usageAnalyticsService) Overview(ctx context.Context, q types.UsageTimeRange) (*types.UsageOverview, error) {
	q, err := s.prepareUsageQuery(ctx, q)
	if err != nil {
		return nil, err
	}
	out, err := s.repo.Overview(ctx, q)
	if err == nil {
		out.CollectingSince = q.CollectingSince
	}
	return out, err
}
func (s *usageAnalyticsService) Tenants(ctx context.Context, q types.UsageTimeRange) (*types.UsagePage[types.TenantUsageRow], error) {
	q, err := s.prepareUsageQuery(ctx, q)
	if err != nil {
		return nil, err
	}
	rows, total, err := s.repo.Tenants(ctx, q)
	if err != nil {
		return nil, err
	}
	return &types.UsagePage[types.TenantUsageRow]{Data: rows, Page: q.Page, PageSize: q.PageSize, Total: total, CollectingSince: q.CollectingSince}, nil
}
func (s *usageAnalyticsService) TimeSeries(ctx context.Context, q types.UsageTimeRange) (*types.UsagePage[types.UsageTimeSeriesPoint], error) {
	q, err := s.prepareUsageQuery(ctx, q)
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.TimeSeries(ctx, q)
	if err != nil {
		return nil, err
	}
	return &types.UsagePage[types.UsageTimeSeriesPoint]{Data: rows, Page: 1, PageSize: len(rows), Total: int64(len(rows)), CollectingSince: q.CollectingSince}, nil
}
func (s *usageAnalyticsService) Models(ctx context.Context, q types.UsageTimeRange) (*types.UsagePage[types.ModelUsageRow], error) {
	q, err := s.prepareUsageQuery(ctx, q)
	if err != nil {
		return nil, err
	}
	rows, total, err := s.repo.Models(ctx, q)
	if err != nil {
		return nil, err
	}
	return &types.UsagePage[types.ModelUsageRow]{Data: rows, Page: q.Page, PageSize: q.PageSize, Total: total, CollectingSince: q.CollectingSince}, nil
}
func (s *usageAnalyticsService) Operations(ctx context.Context, q types.UsageTimeRange) (*types.UsagePage[types.OperationUsageRow], error) {
	q, err := s.prepareUsageQuery(ctx, q)
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.Operations(ctx, q)
	if err != nil {
		return nil, err
	}
	return &types.UsagePage[types.OperationUsageRow]{Data: rows, Page: 1, PageSize: len(rows), Total: int64(len(rows)), CollectingSince: q.CollectingSince}, nil
}
func (s *usageAnalyticsService) MCP(ctx context.Context, q types.UsageTimeRange) (*types.UsagePage[types.MCPUsageRow], error) {
	q, err := s.prepareUsageQuery(ctx, q)
	if err != nil {
		return nil, err
	}
	rows, total, err := s.repo.MCP(ctx, q)
	if err != nil {
		return nil, err
	}
	return &types.UsagePage[types.MCPUsageRow]{Data: rows, Page: q.Page, PageSize: q.PageSize, Total: total, CollectingSince: q.CollectingSince}, nil
}
func (s *usageAnalyticsService) KnowledgeBases(ctx context.Context, q types.UsageTimeRange) (*types.UsagePage[types.KnowledgeBaseUsageRow], error) {
	q, err := s.prepareUsageQuery(ctx, q)
	if err != nil {
		return nil, err
	}
	rows, total, err := s.repo.KnowledgeBases(ctx, q)
	if err != nil {
		return nil, err
	}
	return &types.UsagePage[types.KnowledgeBaseUsageRow]{Data: rows, Page: q.Page, PageSize: q.PageSize, Total: total, CollectingSince: q.CollectingSince}, nil
}

var _ interfaces.UsageAnalyticsService = (*usageAnalyticsService)(nil)
