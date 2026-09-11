export type KnowledgeBaseAccessFilter = 'all' | 'shared' | 'editable' | 'readonly'
export type KnowledgeBaseStatusFilter = 'all' | 'ready' | 'processing' | 'setup'

export interface KnowledgeBaseExperienceItem {
  name?: string
  description?: string
  permission?: string
  isMine?: boolean
  is_mine?: boolean
  is_processing?: boolean
  isProcessing?: boolean
  processing_count?: number
  embedding_model_id?: string
  summary_model_id?: string
  type?: string
  editable?: boolean
  sharedWithMe?: boolean
}

const EDITABLE_PERMISSIONS = new Set(['owner', 'admin', 'editor'])

export function isKnowledgeBaseEditable(item: KnowledgeBaseExperienceItem): boolean {
  if (typeof item.editable === 'boolean') return item.editable
  if (item.isMine === true || item.is_mine === true) return true
  return EDITABLE_PERMISSIONS.has(item.permission || '')
}

export function getKnowledgeBaseExperienceStatus(
  item: KnowledgeBaseExperienceItem,
): Exclude<KnowledgeBaseStatusFilter, 'all'> {
  if (item.is_processing || item.isProcessing || Number(item.processing_count || 0) > 0) {
    return 'processing'
  }
  if (item.type !== 'faq' && (!item.embedding_model_id || !item.summary_model_id)) {
    return 'setup'
  }
  return 'ready'
}

export function matchesKnowledgeBaseFilters(
  item: KnowledgeBaseExperienceItem,
  query: string,
  access: KnowledgeBaseAccessFilter,
  status: KnowledgeBaseStatusFilter,
): boolean {
  const needle = query.trim().toLocaleLowerCase()
  if (needle) {
    const haystack = `${item.name || ''}\n${item.description || ''}`.toLocaleLowerCase()
    if (!haystack.includes(needle)) return false
  }

  if (access !== 'all') {
    if (access === 'shared') {
      const sharedWithMe =
        item.sharedWithMe ??
        (item.isMine === false || Boolean(item.permission && item.is_mine !== true))
      if (!sharedWithMe) return false
    } else {
      const editable = isKnowledgeBaseEditable(item)
      if ((access === 'editable') !== editable) return false
    }
  }

  return status === 'all' || getKnowledgeBaseExperienceStatus(item) === status
}
