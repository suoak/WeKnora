package types

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

const (
	UsageEventKindModel   = "model"
	UsageEventKindMCP     = "mcp"
	UsagePrincipalUnknown = "system/unknown"
)

const (
	UsageClassForeground = "foreground"
	UsageClassBackground = "background"

	UsageGovernanceActive           = "active"
	UsageGovernanceLowActivity      = "low_activity"
	UsageGovernanceInactive         = "inactive"
	UsageGovernanceNeverUsed        = "never_used"
	UsageGovernanceInsufficientData = "insufficient_data"

	ModelUsageOperationKnowledgeQA           = "knowledge_qa_turn"
	ModelUsageOperationAgent                 = "agent_turn"
	ModelUsageOperationAgentCompaction       = "agent_compaction"
	ModelUsageOperationQueryRewrite          = "query_rewrite"
	ModelUsageOperationEntityExtraction      = "entity_extraction"
	ModelUsageOperationDataAnalysisPlanning  = "data_analysis_planning"
	ModelUsageOperationDocumentSummary       = "document_summary"
	ModelUsageOperationGeneratedQuestions    = "generated_questions"
	ModelUsageOperationAutoTag               = "auto_tag"
	ModelUsageOperationGraphExtraction       = "graph_extraction"
	ModelUsageOperationSpreadsheetMetadata   = "spreadsheet_metadata"
	ModelUsageOperationSessionTitle          = "session_title"
	ModelUsageOperationMemoryExtraction      = "memory_extraction"
	ModelUsageOperationMemoryConsolidation   = "memory_consolidation"
	ModelUsageOperationMemoryTopicResolution = "memory_topic_resolution"
	ModelUsageOperationWikiIngestion         = "wiki_ingestion"
	ModelUsageOperationWikiGeneration        = "wiki_generation"
	ModelUsageOperationWikiModification      = "wiki_modification"
	ModelUsageOperationMCPInstruction        = "mcp_instruction_generation"
	ModelUsageOperationEmbedding             = "embedding"
	ModelUsageOperationRerank                = "rerank"
	ModelUsageOperationVLM                   = "vlm"
	ModelUsageOperationASR                   = "asr"
)

var foregroundModelUsageOperations = map[string]struct{}{
	ModelUsageOperationKnowledgeQA: {},
	ModelUsageOperationAgent:       {},
}

var supportedModelUsageOperations = map[string]struct{}{
	ModelUsageOperationKnowledgeQA: {}, ModelUsageOperationAgent: {},
	ModelUsageOperationAgentCompaction: {}, ModelUsageOperationQueryRewrite: {},
	ModelUsageOperationEntityExtraction: {}, ModelUsageOperationDataAnalysisPlanning: {},
	ModelUsageOperationDocumentSummary: {}, ModelUsageOperationGeneratedQuestions: {},
	ModelUsageOperationAutoTag: {}, ModelUsageOperationGraphExtraction: {},
	ModelUsageOperationSpreadsheetMetadata: {}, ModelUsageOperationSessionTitle: {},
	ModelUsageOperationMemoryExtraction: {}, ModelUsageOperationMemoryConsolidation: {},
	ModelUsageOperationMemoryTopicResolution: {}, ModelUsageOperationWikiIngestion: {},
	ModelUsageOperationWikiGeneration: {}, ModelUsageOperationWikiModification: {},
	ModelUsageOperationMCPInstruction: {}, ModelUsageOperationEmbedding: {},
	ModelUsageOperationRerank: {}, ModelUsageOperationVLM: {}, ModelUsageOperationASR: {},
}

func IsModelUsageOperation(operation string) bool {
	_, ok := supportedModelUsageOperations[operation]
	return ok
}

func ModelUsageClass(operation string) string {
	if _, ok := foregroundModelUsageOperations[operation]; ok {
		return UsageClassForeground
	}
	return UsageClassBackground
}

// UsageEventKey returns a deterministic, bounded key without retaining prompt
// or response content. Callers provide stable business IDs or safe hashes.
func UsageEventKey(operation string, parts ...string) string {
	operation = strings.TrimSpace(operation)
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte{0})
		h.Write([]byte(strings.TrimSpace(part)))
	}
	return operation + ":" + hex.EncodeToString(h.Sum(nil))
}

// ModelUsageRecordRequest is the privacy-filtered input to the centralized
// model ledger recorder. It deliberately cannot carry prompts or responses.
type ModelUsageRecordRequest struct {
	EventKey         string
	TenantID         uint64
	Channel          string
	Operation        string
	ModelID          string
	ModelType        string
	Usage            TokenUsage
	SessionID        string
	MessageID        string
	RequestID        string
	TraceID          string
	Status           string
	UsageSource      string
	KnowledgeBaseIDs []string
	KnowledgeIDs     []string
}

type MCPUsageRecordRequest struct {
	EventKey         string
	MCPServiceID     string
	ToolName         string
	Transport        string
	Success          bool
	ErrorCode        string
	LatencyMs        int64
	RequestID        string
	TraceID          string
	KnowledgeBaseIDs []string
	KnowledgeIDs     []string
}

// ModelUsageEvent is the append-only analytics representation of one model
// invocation. It intentionally contains no prompt or response content.
type ModelUsageEvent struct {
	ID               uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	EventKey         string    `json:"event_key" gorm:"type:varchar(255);not null;uniqueIndex"`
	TenantID         uint64    `json:"tenant_id" gorm:"not null;index"`
	PrincipalType    string    `json:"principal_type" gorm:"type:varchar(64);not null"`
	PrincipalID      string    `json:"principal_id,omitempty" gorm:"type:varchar(512)"`
	Channel          string    `json:"channel" gorm:"type:varchar(50);not null"`
	Operation        string    `json:"operation" gorm:"type:varchar(64);not null"`
	ModelID          string    `json:"model_id,omitempty" gorm:"type:varchar(64)"`
	ModelType        string    `json:"model_type,omitempty" gorm:"type:varchar(32)"`
	SessionID        string    `json:"session_id,omitempty" gorm:"type:varchar(36)"`
	MessageID        string    `json:"message_id,omitempty" gorm:"type:varchar(36)"`
	InputTokens      int64     `json:"input_tokens" gorm:"not null"`
	OutputTokens     int64     `json:"output_tokens" gorm:"not null"`
	TotalTokens      int64     `json:"total_tokens" gorm:"not null"`
	CacheReadTokens  int64     `json:"cache_read_tokens" gorm:"not null"`
	CacheWriteTokens int64     `json:"cache_write_tokens" gorm:"not null"`
	ReasoningTokens  int64     `json:"reasoning_tokens" gorm:"not null"`
	UsageSource      string    `json:"usage_source" gorm:"type:varchar(16);not null"`
	Status           string    `json:"status" gorm:"type:varchar(16);not null"`
	RequestID        string    `json:"request_id,omitempty" gorm:"type:varchar(255)"`
	TraceID          string    `json:"trace_id,omitempty" gorm:"type:varchar(255)"`
	OccurredAt       time.Time `json:"occurred_at" gorm:"not null"`
	CreatedAt        time.Time `json:"created_at" gorm:"not null"`
}

func (ModelUsageEvent) TableName() string { return "model_usage_events" }

// MCPUsageEvent represents exactly one inbound MCP tools/call invocation.
type MCPUsageEvent struct {
	ID             uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	EventKey       string    `json:"event_key" gorm:"type:varchar(255);not null;uniqueIndex"`
	CallerTenantID *uint64   `json:"caller_tenant_id,omitempty" gorm:"index"`
	PrincipalType  string    `json:"principal_type" gorm:"type:varchar(64);not null"`
	PrincipalID    string    `json:"principal_id,omitempty" gorm:"type:varchar(512)"`
	APIKeyID       *uint64   `json:"api_key_id,omitempty"`
	Direction      string    `json:"direction" gorm:"type:varchar(16);not null"`
	MCPServiceID   string    `json:"mcp_service_id,omitempty" gorm:"type:varchar(64)"`
	ToolName       string    `json:"tool_name" gorm:"type:varchar(255);not null"`
	ClientName     string    `json:"client_name,omitempty" gorm:"type:varchar(128)"`
	ClientVersion  string    `json:"client_version,omitempty" gorm:"type:varchar(64)"`
	Transport      string    `json:"transport,omitempty" gorm:"type:varchar(16)"`
	Success        bool      `json:"success" gorm:"not null"`
	ErrorCode      string    `json:"error_code,omitempty" gorm:"type:varchar(64)"`
	LatencyMs      int64     `json:"latency_ms" gorm:"not null"`
	RequestID      string    `json:"request_id,omitempty" gorm:"type:varchar(255)"`
	TraceID        string    `json:"trace_id,omitempty" gorm:"type:varchar(255)"`
	OccurredAt     time.Time `json:"occurred_at" gorm:"not null"`
	CreatedAt      time.Time `json:"created_at" gorm:"not null"`
}

func (MCPUsageEvent) TableName() string { return "mcp_usage_events" }

type UsageResourceLink struct {
	ID               uint64 `json:"id" gorm:"primaryKey;autoIncrement"`
	EventKind        string `json:"event_kind" gorm:"type:varchar(16);not null"`
	EventID          uint64 `json:"event_id" gorm:"not null"`
	ResourceType     string `json:"resource_type" gorm:"type:varchar(32);not null"`
	ResourceID       string `json:"resource_id" gorm:"type:varchar(64);not null"`
	ResourceTenantID uint64 `json:"resource_tenant_id" gorm:"not null"`
}

func (UsageResourceLink) TableName() string { return "usage_resource_links" }

// MCPUsageReport is the deliberately narrow DTO accepted from the Python MCP
// adapter. Identity and tenant fields are absent: the Go authentication
// context is the only authority for them.
type MCPUsageReport struct {
	EventKey         string   `json:"event_key"`
	ToolName         string   `json:"tool_name"`
	ClientName       string   `json:"client_name,omitempty"`
	ClientVersion    string   `json:"client_version,omitempty"`
	Transport        string   `json:"transport,omitempty"`
	SharedGateway    bool     `json:"-"`
	Success          bool     `json:"success"`
	ErrorCode        string   `json:"error_code,omitempty"`
	LatencyMs        int64    `json:"latency_ms"`
	RequestID        string   `json:"request_id,omitempty"`
	KnowledgeBaseIDs []string `json:"knowledge_base_ids,omitempty"`
	KnowledgeIDs     []string `json:"knowledge_ids,omitempty"`
}

type UsageTimeRange struct {
	From            time.Time
	To              time.Time
	TenantID        *uint64
	Interval        string
	Page            int
	PageSize        int
	Sort            string
	Operation       string
	UsageClass      string
	Channel         string
	ModelType       string
	Direction       string
	GroupBy         string
	Status          string
	MCPAdoption     string
	CrossTenant     string
	KnowledgeBaseID string
	OwnerTenantID   *uint64
	Metric          string
	Dimension       string
	IncludeInactive bool
	CollectingSince *time.Time
}

type UsageOverview struct {
	TotalTokens            int64                   `json:"total_tokens"`
	ForegroundTokens       int64                   `json:"foreground_tokens"`
	BackgroundTokens       int64                   `json:"background_tokens"`
	InputTokens            int64                   `json:"input_tokens"`
	OutputTokens           int64                   `json:"output_tokens"`
	CacheReadTokens        int64                   `json:"cache_read_tokens"`
	CacheWriteTokens       int64                   `json:"cache_write_tokens"`
	AssistantTurns         int64                   `json:"assistant_turns"`
	MCPCalls               int64                   `json:"mcp_calls"`
	MCPSuccessRate         float64                 `json:"mcp_success_rate"`
	MCPAverageLatencyMs    float64                 `json:"mcp_average_latency_ms"`
	UnattributedMCPCalls   int64                   `json:"unattributed_mcp_calls"`
	ActiveTenants          int64                   `json:"active_tenants"`
	ActivePrincipals       int64                   `json:"active_principals"`
	ActiveKnowledgeBases   int64                   `json:"active_knowledge_bases"`
	CrossTenantUsage       int64                   `json:"cross_tenant_usage"`
	UnattributedMCPRatio   float64                 `json:"unattributed_mcp_ratio"`
	InactiveKnowledgeBases int64                   `json:"inactive_knowledge_bases"`
	MCPActiveTenants       int64                   `json:"mcp_active_tenants"`
	MCPPlatformPenetration float64                 `json:"mcp_platform_penetration"`
	MCPAdoptionAmongActive float64                 `json:"mcp_adoption_among_active_spaces"`
	TokenGrowthPercent     float64                 `json:"token_growth_percent"`
	TopKnowledgeBases      []KnowledgeBaseUsageRow `json:"top_knowledge_bases" gorm:"-"`
	AttentionNeeded        []UsageAttention        `json:"attention_needed" gorm:"-"`
	CollectingSince        *time.Time              `json:"collecting_since,omitempty"`
}

type UsageAttention struct {
	Code       string  `json:"code"`
	EntityType string  `json:"entity_type,omitempty"`
	EntityID   string  `json:"entity_id,omitempty"`
	EntityName string  `json:"entity_name,omitempty"`
	Value      float64 `json:"value,omitempty"`
}

type TenantUsageRow struct {
	TenantID                   uint64     `json:"tenant_id"`
	TenantName                 string     `json:"tenant_name"`
	ActivePrincipals           int64      `json:"active_principals"`
	AssistantTurns             int64      `json:"assistant_turns"`
	AgentTurns                 int64      `json:"agent_turns"`
	InputTokens                int64      `json:"input_tokens"`
	OutputTokens               int64      `json:"output_tokens"`
	TotalTokens                int64      `json:"total_tokens"`
	MCPCalls                   int64      `json:"mcp_calls"`
	MCPSuccessRate             float64    `json:"mcp_success_rate"`
	MCPAvgLatencyMs            float64    `json:"mcp_avg_latency_ms"`
	LastActive                 *time.Time `json:"last_active,omitempty"`
	LastActiveUnix             int64      `json:"-" gorm:"column:last_active_unix"`
	UsageStatus                string     `json:"usage_status"`
	KnowledgeBasesUsed         int64      `json:"kb_used_count" gorm:"column:kb_used_count"`
	KnowledgeBasesOwned        int64      `json:"kb_owned_count" gorm:"column:kb_owned_count"`
	ExternalKnowledgeBasesUsed int64      `json:"external_kb_used_count" gorm:"column:external_kb_used_count"`
	CrossTenantAccesses        int64      `json:"cross_tenant_accesses"`
	OwnedKBCrossTenantAccesses int64      `json:"owned_kb_cross_tenant_accesses" gorm:"column:owned_kb_cross_tenant_accesses"`
	MCPAdopted                 bool       `json:"mcp_adopted"`
	TotalCount                 int64      `json:"-"`
}

type UsageTimeSeriesPoint struct {
	Bucket           time.Time `json:"bucket"`
	InputTokens      int64     `json:"input_tokens"`
	OutputTokens     int64     `json:"output_tokens"`
	TotalTokens      int64     `json:"total_tokens"`
	AssistantTurns   int64     `json:"assistant_turns"`
	MCPCalls         int64     `json:"mcp_calls"`
	ModelAccesses    int64     `json:"model_accesses"`
	InternalAccesses int64     `json:"internal_accesses"`
	ExternalAccesses int64     `json:"external_accesses"`
}

type ModelUsageRow struct {
	ModelID          string     `json:"model_id"`
	ModelType        string     `json:"model_type"`
	AssistantTurns   int64      `json:"assistant_turns"`
	InputTokens      int64      `json:"input_tokens"`
	OutputTokens     int64      `json:"output_tokens"`
	TotalTokens      int64      `json:"total_tokens"`
	CacheReadTokens  int64      `json:"cache_read_tokens"`
	CacheWriteTokens int64      `json:"cache_write_tokens"`
	LastActive       *time.Time `json:"last_active,omitempty"`
	TotalCount       int64      `json:"-"`
}

type OperationUsageRow struct {
	Operation   string  `json:"operation"`
	UsageClass  string  `json:"usage_class"`
	Invocations int64   `json:"invocations"`
	TotalTokens int64   `json:"total_tokens"`
	Percentage  float64 `json:"percentage"`
}

type MCPUsageRow struct {
	ToolName          string     `json:"tool_name"`
	Calls             int64      `json:"calls"`
	SuccessfulCalls   int64      `json:"successful_calls"`
	SuccessRate       float64    `json:"success_rate"`
	AverageLatencyMs  float64    `json:"average_latency_ms"`
	UnattributedCalls int64      `json:"unattributed_calls"`
	LastActive        *time.Time `json:"last_active,omitempty"`
	ClientName        string     `json:"client_name,omitempty"`
	ClientVersion     string     `json:"client_version,omitempty"`
	KnowledgeBaseID   string     `json:"knowledge_base_id,omitempty"`
	KnowledgeBaseName string     `json:"knowledge_base_name,omitempty"`
	TotalCount        int64      `json:"-"`
}

type KnowledgeBaseUsageRow struct {
	KnowledgeBaseID     string     `json:"knowledge_base_id"`
	KnowledgeBaseName   string     `json:"knowledge_base_name"`
	OwnerTenantID       uint64     `json:"owner_tenant_id"`
	OwnerTenantName     string     `json:"owner_tenant_name"`
	CallerTenantID      *uint64    `json:"caller_tenant_id,omitempty"`
	CallerTenantName    string     `json:"caller_tenant_name,omitempty"`
	AssistantTurns      int64      `json:"assistant_turns"`
	MCPCalls            int64      `json:"mcp_calls"`
	LastActive          *time.Time `json:"last_active,omitempty"`
	LastActiveUnix      int64      `json:"-" gorm:"column:last_active_unix"`
	UsageStatus         string     `json:"usage_status,omitempty"`
	TotalAccesses       int64      `json:"total_accesses"`
	ModelAccesses       int64      `json:"model_accesses"`
	UniqueTenants       int64      `json:"unique_tenants"`
	UniquePrincipals    int64      `json:"unique_principals"`
	ExternalTenants     int64      `json:"external_tenants"`
	CrossTenantAccesses int64      `json:"cross_tenant_accesses"`
	CrossTenantShare    float64    `json:"cross_tenant_share"`
	RecentGrowth        float64    `json:"recent_growth"`
	TotalCount          int64      `json:"-"`
}

type UsagePage[T any] struct {
	Data            []T        `json:"data"`
	Page            int        `json:"page"`
	PageSize        int        `json:"page_size"`
	Total           int64      `json:"total"`
	CollectingSince *time.Time `json:"collecting_since,omitempty"`
}
