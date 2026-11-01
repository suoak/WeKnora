const en = {
  syncSource: 'Sync source document again', retryParse: 'Retry parsing', reauthorize: 'Authorize Feishu again',
  syncConfirm: 'Check the source and sync updates? The published version is kept until a new version succeeds.',
  retryConfirm: 'Check source access and retry this document? The original file will be kept.',
  retrySubmitted: 'Parsing retry submitted', syncSubmitted: 'Source sync submitted',
  reauthorizeTitle: 'Feishu authorization required',
  safetyFailure: 'Source access failed a security check. Retry later or contact an administrator.',
  permissionFailure: 'Source access is unavailable. Ask the original importer to check Feishu authorization.',
  timeoutFailure: 'Source verification timed out. You can retry later.',
  verificationFailure: 'The source or file version could not be verified. Contact an administrator.',
  processingFailure: 'Document processing failed. Retry parsing or contact an administrator.',
  source: 'Personal Feishu document', waiting: 'Summary not loaded', loading: 'Loading summary…',
  unavailable: 'Summary is currently unavailable', retry: 'Load summary again', empty: 'No document summary',
  openToRead: 'Open the document to view its summary',
}
const zh = {
  syncSource: '重新同步源文档', retryParse: '重试解析', reauthorize: '重新授权',
  syncConfirm: '核验来源并同步更新？新版本成功前，现有已发布内容保持完整。',
  retryConfirm: '核验来源权限后重试此文档？原文件会完整保留。',
  retrySubmitted: '已提交解析重试', syncSubmitted: '已提交源文档同步',
  reauthorizeTitle: '需要重新授权飞书',
  safetyFailure: '访问飞书文档时安全校验未通过，请稍后重试；持续失败请联系管理员。',
  permissionFailure: '当前无法访问来源，请联系原导入用户核验飞书授权。',
  timeoutFailure: '验证来源时请求超时，可稍后重试。',
  verificationFailure: '文档来源或文件版本核验未通过，请联系管理员核验。',
  processingFailure: '文档处理失败，可重试解析；持续失败请联系管理员。',
  source: '飞书个人文档', waiting: '摘要待加载', loading: '正在加载摘要…',
  unavailable: '摘要暂不可用', retry: '重新加载摘要', empty: '暂无文档摘要',
  openToRead: '请打开文档查看摘要',
}
export const personalDocumentMessages = {
  'zh-CN': zh, 'en-US': en, 'ja-JP': en, 'ko-KR': en, 'ru-RU': en,
}
export function personalDocumentSourceLabel(locale: string): string {
  return (personalDocumentMessages[locale as keyof typeof personalDocumentMessages] ?? en).source
}
export function personalDocumentMessage(locale: string, key: keyof typeof en): string {
  return (personalDocumentMessages[locale as keyof typeof personalDocumentMessages] ?? en)[key]
}
