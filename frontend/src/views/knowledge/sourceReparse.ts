const channels = new Set(['feishu', 'feishu_drive', 'lark', 'lark_drive'])
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

export interface SourceDocumentReparseStatus {
  id: string
  knowledge_id: string
  state: 'queued' | 'processing' | 'completed' | 'failed'
  attempts: number
  error_code?: string
}

export interface SourceReparseDocument {
  id: string
  channel?: string
  metadata?: Record<string, unknown>
  source_document_reparse?: SourceDocumentReparseStatus
}

export function sourceReparseFailureAction(code?: string): string {
  const actions: Record<string, string> = {
    encrypted_file: 'provide_unencrypted_copy',
    safe_staging_encrypted_file: 'provide_unencrypted_copy',
    file_format_mismatch: 'check_file_format',
    safe_staging_file_format_mismatch: 'check_file_format',
    safe_staging_invalid_xlsx_package: 'check_file_format',
    semantic_chunk_limit: 'adjust_table_chunking',
    safe_staging_parser_semantic_row_too_large: 'adjust_table_chunking',
    source_address_blocked: 'check_source_address',
    safe_staging_source_address_blocked: 'check_source_address',
    parser_resource_limit: 'split_document',
    safe_staging_parser_resource_limit: 'split_document',
    safe_staging_chunk_count_out_of_bounds: 'split_document',
  }
  return code && Object.hasOwn(actions, code)
    ? 'knowledgeBase.parseRecoveryActions.' + actions[code]
    : 'knowledgeBase.sourceDocumentReparseReason'
}

// Presentation only. The source API rederives the binding and current authority.
export function feishuSourceId(item: SourceReparseDocument): string | null {
  const source = item.metadata?.datasource_id
  return channels.has(item.channel || '') && typeof source === 'string' && uuid.test(source)
    && typeof item.metadata?.external_id === 'string' && item.metadata.external_id.length > 0 ? source : null
}

export function sourceIdFromReparseConflict(error: any): string | null {
  const details = error?.error?.details ?? error?.details
  return ['feishu_source_sync_required', 'feishu_source_reparse_required'].includes(details?.reason)
    && typeof details?.data_source_id === 'string' && uuid.test(details.data_source_id) ? details.data_source_id : null
}
