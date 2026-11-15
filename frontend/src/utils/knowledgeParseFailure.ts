export interface ParseFailureProgress {
  code: string
  state: 'retry_scheduled' | 'needs_attention'
  action: string
  attempts: number
  max_attempts: number
  auto_retry: boolean
}

const actions = new Set([
  'provide_unencrypted_copy', 'check_file_format', 'adjust_table_chunking',
  'check_source_address', 'split_document', 'review_processing_trace', 'wait_for_automatic_retry',
])

// Never infer that a task is scheduled from raw error strings or a failed row.
export function parseFailureProgress(item: {
  parse_status?: string
  metadata?: Record<string, unknown>
}): ParseFailureProgress | null {
  const value = item.metadata?.parse_failure_v1 as Partial<ParseFailureProgress> | undefined
  if (!value || typeof value.code !== 'string' || !actions.has(value.action || '')
    || !Number.isInteger(value.attempts) || !Number.isInteger(value.max_attempts)
    || value.attempts! < 1 || value.max_attempts! < value.attempts! || value.max_attempts! > 100) return null
  if (value.state === 'retry_scheduled') {
    if (item.parse_status !== 'processing' || value.auto_retry !== true
      || value.action !== 'wait_for_automatic_retry' || value.attempts! >= value.max_attempts!) return null
  } else if (value.state === 'needs_attention') {
    if (item.parse_status !== 'failed' || value.auto_retry !== false) return null
  } else return null
  return value as ParseFailureProgress
}
