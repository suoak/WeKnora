import { del, get, post, put } from '@/utils/request'

export type KBScopeMode = 'all' | 'selected'
export type KBSourceType = 'owned' | 'shared'

export interface MCPKBRef {
  knowledge_base_id: string
  source_type: KBSourceType
  kb_share_id?: string
}

export interface MCPTenantScope {
  tenant_id: number
  kb_scope_mode: KBScopeMode
  knowledge_bases: MCPKBRef[]
}

export interface MCPAccessKey {
  id: number
  name: string
  api_key: string
  token?: string
  tenant_scopes: MCPTenantScope[]
  expires_at?: string
  last_used_at?: string
  created_at: string
  mcp_public_url?: string
}

export interface KnowledgeBaseOption {
  id: string
  name: string
}

export interface SharedKnowledgeBaseOption {
  knowledge_base: KnowledgeBaseOption
  share_id: string
  organization_id: string
  org_name: string
  permission: string
}

export interface MCPScopeOption {
  tenant_id: number
  tenant_name: string
  role: string
  owned_knowledge_bases: KnowledgeBaseOption[]
  shared_knowledge_bases: SharedKnowledgeBaseOption[]
}

export interface MCPKeyPayload {
  name: string
  expires_at_unix?: number
  never_expires?: boolean
  tenant_scopes: MCPTenantScope[]
}

export interface MCPKeyResponse<T> {
  success: boolean
  data?: T
  mcp_public_url?: string
}

export const listMCPAccessKeys = () => get('/api/v1/mcp-api-keys') as Promise<MCPKeyResponse<MCPAccessKey[]>>
export const createMCPAccessKey = (payload: MCPKeyPayload) => post('/api/v1/mcp-api-keys', payload) as Promise<MCPKeyResponse<MCPAccessKey>>
export const updateMCPAccessKey = (id: number, payload: MCPKeyPayload) => put(`/api/v1/mcp-api-keys/${id}`, payload) as Promise<MCPKeyResponse<MCPAccessKey>>
export const revokeMCPAccessKey = (id: number) => del(`/api/v1/mcp-api-keys/${id}`) as Promise<MCPKeyResponse<never>>
export const getMCPAccessKeyScopeOptions = () => get('/api/v1/mcp-api-keys/scope-options') as Promise<MCPKeyResponse<MCPScopeOption[]>>
