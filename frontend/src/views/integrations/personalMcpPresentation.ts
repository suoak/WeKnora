import type { MCPAccessKey, MCPScopeOption, MCPTenantScope } from '../../api/mcpAccessKeys'

// Client labels are preferences, never proof of personal/public authorization.
export function isUnifiedPersonalKey(key: Pick<MCPAccessKey, 'scope_type' | 'capabilities'>): boolean {
  return key.scope_type === 'user_mcp' && Array.isArray(key.capabilities)
    && key.capabilities.includes('retrieve') && key.capabilities.includes('enterprise_retrieve')
    && key.capabilities.every(value => value === 'retrieve' || value === 'enterprise_retrieve')
}

// An explicit update may add currently authorized spaces. Existing selected KB
// restrictions and invalid historical scopes survive unchanged for server-side
// validation; the UI must never silently replace them with all.
export function personalPrivateScopeUpdate(original: MCPTenantScope[], additions: number[], options: MCPScopeOption[]): MCPTenantScope[] {
  const result = original.map(scope => ({ ...scope, knowledge_bases: (scope.knowledge_bases || []).map(kb => ({ ...kb })) }))
  const existing = new Set(result.map(scope => scope.tenant_id))
  const available = new Set(options.map(option => option.tenant_id))
  for (const id of additions) {
    if (!available.has(id)) throw new Error('私人空间已失效，请刷新后重试')
    if (!existing.has(id)) { result.push({ tenant_id: id, kb_scope_mode: 'all', knowledge_bases: [] }); existing.add(id) }
  }
  return result
}
