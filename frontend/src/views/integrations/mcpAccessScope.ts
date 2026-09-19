import type { MCPAccessKey, MCPKBRef, MCPScopeOption } from '@/api/mcpAccessKeys'

const refKey = (ref: MCPKBRef) => `${ref.source_type}:${ref.knowledge_base_id}:${ref.kb_share_id || ''}`

export function availableKBRefs(option?: MCPScopeOption): Set<string> {
  if (!option) return new Set()
  return new Set([
    ...option.owned_knowledge_bases.map(kb => `owned:${kb.id}:`),
    ...option.shared_knowledge_bases.map(share => `shared:${share.knowledge_base.id}:${share.share_id}`),
  ])
}

export function invalidCredentialScopes(key: Pick<MCPAccessKey, 'tenant_scopes'>, options: MCPScopeOption[]) {
  const byTenant = new Map(options.map(option => [option.tenant_id, option]))
  const invalidTenantIds: number[] = []
  const invalidKBRefs: Record<number, MCPKBRef[]> = {}
  for (const scope of key.tenant_scopes) {
    const option = byTenant.get(scope.tenant_id)
    if (!option) {
      invalidTenantIds.push(scope.tenant_id)
      continue
    }
    if (scope.kb_scope_mode !== 'selected') continue
    const available = availableKBRefs(option)
    const invalid = scope.knowledge_bases.filter(ref => !available.has(refKey(ref)))
    if (invalid.length) invalidKBRefs[scope.tenant_id] = invalid
  }
  return { invalidTenantIds, invalidKBRefs }
}
