import { del, get, patch, post } from '@/utils/request'

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
  scope_type: 'user_mcp'
  client_type: MCPClientType
  status: MCPAccessStatus
  token_hint: string
  capabilities: MCPCapability[]
  token?: string
  tenant_scopes: MCPTenantScope[]
  expires_at?: string
  revoked_at?: string
  last_used_at?: string
  created_at: string
  updated_at: string
}

export type MCPClientType = 'workbuddy' | 'workmate' | 'cursor' | 'codebuddy' | 'generic'
export type MCPAccessStatus = 'active' | 'expired' | 'revoked'
export type MCPCapability = 'retrieve' | 'chat' | 'read_agents'

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
  client_type: MCPClientType
  capabilities: MCPCapability[]
  expires_at_unix?: number
  never_expires?: boolean
  tenant_scopes: MCPTenantScope[]
}

export type MCPKeyPatchPayload = Partial<MCPKeyPayload>

export interface MCPRotatePayload {
  expires_at_unix?: number
  never_expires?: boolean
}

export interface MCPSecretResult extends MCPAccessKey {
  credential?: MCPAccessKey
  token: string
  mcp_public_url?: string
}

export interface MCPKeyResponse<T> {
  success: boolean
  data?: T
  mcp_public_url?: string
}

export const listMCPAccessKeys = () => get('/api/v1/mcp-api-keys') as Promise<MCPKeyResponse<MCPAccessKey[]>>
export const createMCPAccessKey = (payload: MCPKeyPayload) => post('/api/v1/mcp-api-keys', payload) as Promise<MCPKeyResponse<MCPSecretResult>>
export const updateMCPAccessKey = (id: number, payload: MCPKeyPatchPayload) => patch(`/api/v1/mcp-api-keys/${id}`, payload) as Promise<MCPKeyResponse<MCPAccessKey>>
export const rotateMCPAccessKey = (id: number, payload: MCPRotatePayload = {}) => post(`/api/v1/mcp-api-keys/${id}/rotate`, payload) as Promise<MCPKeyResponse<MCPSecretResult>>
export const revokeMCPAccessKey = (id: number) => del(`/api/v1/mcp-api-keys/${id}`) as Promise<MCPKeyResponse<never>>
export const getMCPAccessKeyScopeOptions = () => get('/api/v1/mcp-api-keys/scope-options') as Promise<MCPKeyResponse<MCPScopeOption[]>>
