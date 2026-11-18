type ProviderFailure = { provider_code?: number; provider_message?: string; operation?: string }

// An old audit may have only a code. Do not invent an upstream message for it.
export function feishuProviderFailure(item: ProviderFailure): string {
  const code = item.provider_code
  if (!Number.isInteger(code) || !code || code < 1 || code >= 1000000000) return ''
  const stages: Record<string, string> = {
    create_export_task: '导出失败', get_export_task_status: '获取导出结果失败',
    download_export_file: '下载导出文件失败', download_export_file_range: '下载导出文件失败',
    resolve_personal_resource: '读取文档信息失败', get_wiki_node: '读取飞书知识库节点失败',
  }
  const stage = stages[item.operation || ''] || '飞书请求失败'
  return item.provider_message
    ? `${stage}。飞书返回：“${item.provider_message}”（错误码：${code}）。`
    : `${stage}。飞书返回错误码：${code}。`
}
