export interface KnowledgeSubmitterItem {
  submitter?: { kind?: string; name?: string }
  metadata?: { submitter?: { name?: string } }
}

export function knowledgeSubmitterName(item: KnowledgeSubmitterItem, systemSyncLabel: string): string {
  if (item.submitter?.kind === 'user' && typeof item.submitter.name === 'string'
    && item.submitter.name.trim()) return item.submitter.name.trim()
  const storedName = item.metadata?.submitter?.name
  if (typeof storedName === 'string' && storedName.trim()) return storedName.trim()
  return item.submitter?.kind === 'system_sync' ? systemSyncLabel : ''
}
