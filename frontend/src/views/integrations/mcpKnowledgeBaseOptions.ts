// Presentation only. Endpoint save and execution still authorize IDs server-side.
export interface ScopeKnowledgeBase {
  id: string
  name: string
  space: string
  sources: string[]
}
type Reply = { success?: boolean; data?: unknown }
export async function loadMcpKnowledgeBaseOptions(
  owned: () => Promise<Reply>, shared: () => Promise<Reply>, space: string,
): Promise<{ rows: ScopeKnowledgeBase[]; failed: boolean }> {
  const results = await Promise.allSettled([owned(), shared()])
  const rows = new Map<string, ScopeKnowledgeBase>()
  let failed = false
  for (let index = 0; index < results.length; index++) {
    const result = results[index]
    if (result.status !== 'fulfilled' || result.value.success !== true || !Array.isArray(result.value.data)) {
      failed = true
      continue
    }
    for (const item of result.value.data) {
      if (!item || typeof item !== 'object') continue
      const kb = index === 0 ? item : item.knowledge_base
      if (!kb || !['string', 'number'].includes(typeof kb.id) || !String(kb.id).trim()) continue
      if (index === 1 && (!['admin', 'editor', 'viewer'].includes(item.permission)
        || !Number.isSafeInteger(item.source_tenant_id) || item.source_tenant_id <= 0)) continue
      const id = String(kb.id), source = index === 1 ? String(item.org_name || item.organization_id || '') : ''
      const existing = rows.get(id)
      if (existing) {
        // Owned entries take precedence; merge repeated shared origins deterministically.
        if (existing.sources.length && source && !existing.sources.includes(source)) existing.sources.push(source)
      } else {
        rows.set(id, { id, name: String(kb.name || id), space: index === 0 ? space : String(item.source_tenant_id),
          sources: index === 0 ? [] : [source] })
      }
    }
  }
  // No previous snapshot is accepted. Failure is explicit and blocks saving.
  return { rows: [...rows.values()], failed }
}
