export type KnowledgeProcessingState =
  | 'waiting'
  | 'processing'
  | 'finalizing'
  | 'completed'
  | 'failed'
  | 'cancelled'
  | 'unknown'

export type KnowledgeProcessingItem = {
  parse_status?: string | null
}

export type KnowledgeProcessingSummary = {
  total: number
  completed: number
  processing: number
  failed: number
  cancelled: number
}

export function presentKnowledgeProcessingState(status?: string | null): KnowledgeProcessingState {
  switch ((status || '').toLowerCase()) {
    case 'pending':
    case 'queued':
      return 'waiting'
    case 'processing':
    case 'running':
      return 'processing'
    case 'finalizing':
      return 'finalizing'
    case 'completed':
    case 'done':
      return 'completed'
    case 'failed':
    case 'error':
      return 'failed'
    case 'cancelled':
    case 'canceled':
      return 'cancelled'
    default:
      return 'unknown'
  }
}

export function summarizeKnowledgeProcessing(items: KnowledgeProcessingItem[]): KnowledgeProcessingSummary {
  const summary: KnowledgeProcessingSummary = {
    total: items.length,
    completed: 0,
    processing: 0,
    failed: 0,
    cancelled: 0,
  }

  for (const item of items) {
    const state = presentKnowledgeProcessingState(item.parse_status)
    if (state === 'completed') summary.completed += 1
    else if (state === 'failed') summary.failed += 1
    else if (state === 'cancelled') summary.cancelled += 1
    else if (state === 'waiting' || state === 'processing' || state === 'finalizing') summary.processing += 1
  }

  return summary
}

export function conciseProcessingError(message?: string | null, maxLength = 120): string {
  const firstLine = (message || '').split(/\r?\n/, 1)[0].replace(/^error:\s*/i, '').trim()
  if (!firstLine) return ''
  if (firstLine.length <= maxLength) return firstLine
  return `${firstLine.slice(0, Math.max(0, maxLength - 1)).trimEnd()}…`
}
