import type {FeishuBatchItem} from '@/api/feishuPersonal'

const reconnectCategories = new Set([
  'user_scope_missing', 'reconnect_required', 'authorization_expired',
  'authorization_invalid', 'refresh_token_expired', 'refresh_rejected',
])

export function feishuImportNeedsReconnect(item: FeishuBatchItem): boolean {
  return item.user_action === 'connect_feishu' || reconnectCategories.has(item.error_category || '')
}

export function feishuImportRecoveryHint(item: FeishuBatchItem): string {
	if (item.error_category === 'candidate_media_not_supported') {
		return '文档包含当前不支持的媒体或附件，原文件和已有内容已保留。请调整文档或联系管理员；重复导入不能解决。'
	}
  if (item.error_category === 'exact_document_execution_requires_review') {
    return item.can_retry === true
      ? '处理已停止，原文件和中间结果已保留。可点击重新尝试，系统会检查当前授权并继续原任务。'
      : '处理已停止，原文件和中间结果已保留。请联系管理员核查恢复条件。'
  }
  if (['retry_exhausted', 'parse_failed', 'index_failed', 'parse_or_index_failed'].includes(item.error_category || '')) {
    return item.can_retry === true
      ? '文档处理失败，已有内容保留。请检查失败原因后点击重新尝试。'
      : '文档处理失败，已有内容保留。请联系管理员核查；重复导入不会清除已有内容。'
  }
  return ''
}
