const en = {
  browse: 'Browse my Drive', driveScope: 'Shows files listed by Feishu for your authorized account. Other accessible documents can still be imported by link.', root: 'My Drive', parent: 'Back', more: 'Next page', empty: 'No files in this folder.', select: 'Select', openFolder: 'Open folder', unsupported: 'Import by link or export manually',
  title: 'Import Feishu document', connect: 'Connect my Feishu account', disconnect: 'Disconnect', unavailable: 'Ask the system administrator to configure and enable Feishu.',
  url: 'Paste a single Feishu document or file link', import: 'Import', queued: 'Document fetched; parsing and indexing are queued.', failed: 'Check authorization, document access and knowledge base configuration.',
  recent: 'My imports — click to sync again', coverage: 'Exports may omit embedded objects or attachments. Review completeness after parsing.', connected: 'Feishu account connected', callbackFailed: 'Authorization failed or expired. Return and connect again.', back: 'Return to KnowHub',
  app: 'Feishu personal document application', appID: 'App ID', secret: 'App Secret (leave empty to keep)', callback: 'OAuth callback URL', enabled: 'Enable personal imports', save: 'Save', test: 'Test saved application credentials', saved: 'Configuration saved', tested: 'Application credentials valid. User scopes and document access still need authorization.',
}
const zh = {
  browse: '浏览我的云盘', driveScope: '显示飞书为当前授权账号列出的文件。其他有权访问的文档仍可通过链接导入。', root: '我的云盘', parent: '返回上一级', more: '下一页', empty: '此文件夹暂无文件。', select: '选择', openFolder: '打开文件夹', unsupported: '请通过链接或手动导出后导入',
  title: '导入飞书文档', connect: '连接我的飞书账号', disconnect: '解绑', unavailable: '请联系系统管理员配置并启用飞书应用。', url: '粘贴单篇飞书文档或文件链接', import: '导入', queued: '已获取文档，正在进入解析和索引流程。', failed: '请检查飞书授权、文档权限和知识库配置。',
  recent: '我的导入记录 · 点击可重新同步', coverage: '导出可能不包含嵌入对象或附件，请在解析完成后核对完整性。', connected: '飞书账号已连接', callbackFailed: '授权失败或已过期，请返回知识库重新连接。', back: '返回 KnowHub',
  app: '飞书个人文档应用', appID: 'App ID', secret: 'App Secret（留空保留）', callback: 'OAuth 回调地址', enabled: '启用个人文档导入', save: '保存配置', test: '测试已保存的应用凭证', saved: '配置已保存', tested: '应用凭证有效，用户 Scope 和文档权限仍需授权验证。',
}
export const feishuMessages = { 'zh-CN': zh, 'en-US': en, 'ja-JP': en, 'ko-KR': en, 'ru-RU': en }
export const FEISHU_PENDING = 'knowhub_feishu_pending'
