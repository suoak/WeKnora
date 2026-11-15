export type KnowledgePollStatus = {
  parse_status?: string
  summary_status?: string
  source_document_reparse?: { state: string }
}

export function isKnowledgeParseInFlight(status?: string): boolean {
  return status === 'pending' || status === 'processing' || status === 'finalizing'
}

export function knowledgeNeedsStatusPolling(item: KnowledgePollStatus): boolean {
  if (item.source_document_reparse?.state === 'queued' || item.source_document_reparse?.state === 'processing') return true
  if (isKnowledgeParseInFlight(item.parse_status)) return true
  return item.parse_status === 'completed' &&
    (item.summary_status === 'pending' || item.summary_status === 'processing')
}

export function shouldRefreshWikiStatusAfterKnowledgePoll(
  before: KnowledgePollStatus,
  after: KnowledgePollStatus,
): boolean {
  return knowledgeNeedsStatusPolling(before) && !knowledgeNeedsStatusPolling(after)
}
