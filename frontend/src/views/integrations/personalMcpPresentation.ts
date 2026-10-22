import type { MCPAccessKey, MCPKBRef, MCPKeyPayload, MCPScopeOption, MCPTenantScope } from '../../api/mcpAccessKeys'

// Client labels are preferences, never proof of personal/public authorization.
export function isUnifiedPersonalKey(key: Pick<MCPAccessKey, 'scope_type' | 'capabilities'> & Partial<Pick<MCPAccessKey, 'client_type'>>): boolean {
  return key.scope_type === 'user_mcp' && Array.isArray(key.capabilities)
    && key.capabilities.includes('retrieve') && (key.capabilities.includes('enterprise_retrieve') || key.client_type === 'generic')
    && key.capabilities.every(value => value === 'retrieve' || value === 'enterprise_retrieve')
}

export type PersonalKnowledgeRange = 'default' | 'selected'

// Selected credentials deliberately omit enterprise_retrieve. The existing
// server then requires private membership/share AND the exact persisted scope;
// a client cannot add a public-only KB through tool arguments.
export function personalKnowledgeRangePayload(mode: PersonalKnowledgeRange, selections: Record<number, string[]>, options: MCPScopeOption[]): Pick<MCPKeyPayload, 'personal_scope_mode' | 'capabilities' | 'tenant_scopes'> {
  if (mode === 'default') {
    if (!options.length) throw new Error('当前没有可用的授权空间')
    return { personal_scope_mode: 'dynamic_all', capabilities: ['retrieve', 'enterprise_retrieve'], tenant_scopes: options.map(option => ({ tenant_id: option.tenant_id, kb_scope_mode: 'all', knowledge_bases: [] })) }
  }
  const scopes: MCPTenantScope[] = []
  for (const [tenant, values] of Object.entries(selections)) {
    if (!values.length) continue
    const option = options.find(item => item.tenant_id === Number(tenant))
    if (!option) throw new Error('所选空间已失效，请刷新后重试')
    const available = new Map<string, MCPKBRef>([
      ...option.owned_knowledge_bases.map(kb => [`owned:${kb.id}`, { knowledge_base_id: kb.id, source_type: 'owned' as const }] as const),
      ...option.shared_knowledge_bases.map(share => [`shared:${share.knowledge_base.id}:${share.share_id}`, { knowledge_base_id: share.knowledge_base.id, source_type: 'shared' as const, kb_share_id: share.share_id }] as const),
    ])
    const knowledge_bases = [...new Set(values)].map(value => {
      const kb = available.get(value)
      if (!kb) throw new Error('所选知识库已失效，请刷新后重试')
      return { ...kb }
    })
    scopes.push({ tenant_id: option.tenant_id, kb_scope_mode: 'selected', knowledge_bases })
  }
  if (!scopes.length) throw new Error('请至少选择一个当前有权读取的知识库')
  return { personal_scope_mode: 'fixed', capabilities: ['retrieve'], tenant_scopes: scopes }
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
