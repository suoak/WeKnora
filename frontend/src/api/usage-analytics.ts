import { get } from '@/utils/request'

export interface UsageQuery {
  from?: string
  to?: string
  tenant_id?: number
  interval?: 'hour' | 'day' | 'week' | 'month'
  page?: number
  page_size?: number
  sort?: string
  operation?: string
  usage_class?: 'foreground' | 'background'
  channel?: string
  model_type?: string
  direction?: 'inbound' | 'outbound'
  group_by?: 'caller' | 'knowledge_base' | 'tool' | 'client'
  status?: UsageGovernanceStatus
  include_inactive?: boolean
  mcp_adoption?: 'adopted' | 'not_adopted'
  cross_tenant?: 'with' | 'without'
  owner_tenant_id?: number
  knowledge_base_id?: string
  metric?: 'tokens' | 'accesses'
  dimension?: 'total' | 'source' | 'scope'
}

export type UsageGovernanceStatus = 'active' | 'low_activity' | 'inactive' | 'never_used' | 'insufficient_data'

export interface UsageAttention {
  code: string
  entity_type?: string
  entity_id?: string
  entity_name?: string
  value?: number
}

export interface UsageOverview {
  total_tokens: number
  foreground_tokens: number
  background_tokens: number
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  assistant_turns: number
  mcp_calls: number
  mcp_success_rate: number
  mcp_average_latency_ms: number
  unattributed_mcp_calls: number
  active_tenants: number
  active_principals: number
  active_knowledge_bases: number
  cross_tenant_usage: number
  unattributed_mcp_ratio: number
  inactive_knowledge_bases: number
  mcp_active_tenants: number
  mcp_platform_penetration: number
  mcp_adoption_among_active_spaces: number
  token_growth_percent: number
  top_knowledge_bases: KnowledgeBaseUsageRow[]
  attention_needed: UsageAttention[]
  collecting_since?: string
}

export interface UsagePage<T> {
  data: T[]
  page: number
  page_size: number
  total: number
  collecting_since?: string
}

export interface TenantUsageRow {
  tenant_id: number
  tenant_name: string
  active_principals: number
  assistant_turns: number
  agent_turns: number
  input_tokens: number
  output_tokens: number
  total_tokens: number
  mcp_calls: number
  mcp_success_rate: number
  mcp_avg_latency_ms: number
  last_active?: string
  usage_status: UsageGovernanceStatus
  kb_used_count: number
  kb_owned_count: number
  external_kb_used_count: number
  cross_tenant_accesses: number
  owned_kb_cross_tenant_accesses: number
  mcp_adopted: boolean
}

export interface UsageTimeSeriesPoint {
  bucket: string
  input_tokens: number
  output_tokens: number
  total_tokens: number
  assistant_turns: number
  mcp_calls: number
  model_accesses: number
  internal_accesses: number
  external_accesses: number
}

export interface ModelUsageRow {
  model_id: string
  model_type: string
  assistant_turns: number
  input_tokens: number
  output_tokens: number
  total_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  last_active?: string
}

export interface OperationUsageRow {
  operation: string
  usage_class: 'foreground' | 'background'
  invocations: number
  total_tokens: number
  percentage: number
}

export interface MCPUsageRow {
  tool_name: string
  calls: number
  successful_calls: number
  success_rate: number
  average_latency_ms: number
  unattributed_calls: number
  last_active?: string
  client_name?: string
  client_version?: string
  knowledge_base_id?: string
  knowledge_base_name?: string
}

export interface KnowledgeBaseUsageRow {
  knowledge_base_id: string
  knowledge_base_name: string
  owner_tenant_id: number
  owner_tenant_name: string
  caller_tenant_id?: number
  caller_tenant_name?: string
  assistant_turns: number
  mcp_calls: number
  last_active?: string
  usage_status: UsageGovernanceStatus
  total_accesses: number
  model_accesses: number
  unique_tenants: number
  unique_principals: number
  external_tenants: number
  cross_tenant_accesses: number
  cross_tenant_share: number
  recent_growth: number
}

function params(query: UsageQuery): string {
  const value = new URLSearchParams()
  Object.entries(query).forEach(([key, item]) => {
    if (item !== undefined && item !== '') value.set(key, String(item))
  })
  const encoded = value.toString()
  return encoded ? `?${encoded}` : ''
}

const base = '/api/v1/system/admin/usage'

export const getUsageOverview = (query: UsageQuery) => get(`${base}/overview${params(query)}`) as Promise<UsageOverview>
export const getUsageTenants = (query: UsageQuery) =>
  get(`${base}/tenants${params(query)}`) as Promise<UsagePage<TenantUsageRow>>
export const getUsageTimeSeries = (query: UsageQuery) =>
  get(`${base}/timeseries${params(query)}`) as Promise<UsagePage<UsageTimeSeriesPoint>>
export const getUsageModels = (query: UsageQuery) =>
  get(`${base}/models${params(query)}`) as Promise<UsagePage<ModelUsageRow>>
export const getUsageOperations = (query: UsageQuery) =>
  get(`${base}/operations${params(query)}`) as Promise<UsagePage<OperationUsageRow>>
export const getMCPUsage = (query: UsageQuery) => get(`${base}/mcp${params(query)}`) as Promise<UsagePage<MCPUsageRow>>
export const getKnowledgeBaseUsage = (query: UsageQuery) =>
  get(`${base}/knowledge-bases${params(query)}`) as Promise<UsagePage<KnowledgeBaseUsageRow>>
