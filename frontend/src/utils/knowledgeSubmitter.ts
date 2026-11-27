export interface KnowledgeSubmitterItem {
  submitter?: { kind?: string; name?: string }
  metadata?: { submitter?: { name?: string } }
}

export function knowledgeSubmitterName(item: KnowledgeSubmitterItem, systemSyncLabel: string): string {
  if (item.submitter?.kind === 'user' && typeof item.submitter.name === 'string'
    && item.submitter.name.trim()) return item.submitter.name.trim()
  // Historical metadata is evidence for the server to validate, not a display fallback.
  return item.submitter?.kind === 'system_sync' ? systemSyncLabel : ''
}
