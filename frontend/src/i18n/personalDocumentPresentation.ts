const en = {
  source: 'Personal Feishu document', waiting: 'Summary not loaded', loading: 'Loading summary…',
  unavailable: 'Summary is currently unavailable', retry: 'Load summary again', empty: 'No document summary',
  openToRead: 'Open the document to view its summary',
}
const zh = {
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
