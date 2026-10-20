const en = {
  applicationTablesInstructions: 'Add 文档小助手 to the Feishu document first. Supports Docx, Sheet and Bitable; media requires complete parsing.',
  sharingShort: 'Imported content is shared according to this knowledge base’s permissions.', source: 'Original document',
  candidate_media_not_supported: 'Images or attachments cannot be fully imported yet. Export and review the file before uploading.', candidate_content_incomplete: 'Content was incomplete or changed during reading. Retry after the source is stable.', recoveryReview: 'Processing needs review. Existing content has been preserved.',
  applicationMode: 'Document application authorization', personalMode: 'Personal Feishu OAuth', applicationRestricted: 'Import requires write access to the destination knowledge base.', applicationInstructions: 'Add 文档小助手 to the Feishu document first. Supports Docx text and tables.',
  title: 'Import Feishu document', connect: 'Connect my Feishu account', disconnect: 'Disconnect', unavailable: 'Ask the system administrator to configure and enable Feishu.',
  url: 'Paste a single Feishu document or file link', import: 'Import', queued: 'Submitted. Import will finish in the background.', failed: 'Check authorization, document access and knowledge base configuration.',
  recent: 'My imports · retry the same document', coverage: 'Exports may omit embedded objects or attachments. Review completeness after parsing.', connected: 'Feishu account connected', callbackFailed: 'Authorization failed or expired. Return and connect again.', back: 'Return to KnowHub',
  sharing: 'Imported content follows this knowledge base’s permissions. Other readers of a shared knowledge base may read it; Feishu private ACLs are not inherited.',
  updates: 'First import only. Document updates are not open yet; existing knowledge will be preserved.',
  open: 'View knowledge', indexState: 'Index', wikiState: 'Wiki post-processing',
  pending: 'Processing', verified: 'Index verified', indexed: 'Index verified; post-processing continues', synced: 'Completed', attention: 'Import failed', needs_attention: 'Import failed', disabled: 'Not enabled', completed: 'Completed', not_started: 'Not started', index_pending: 'Queued for parsing', accepted: 'Accepted',
  app: 'Feishu document application', appID: 'App ID', secret: 'App Secret (leave empty to keep)', callback: 'OAuth callback URL', enabled: 'Enable personal imports', save: 'Save', test: 'Test saved application credentials', saved: 'Configuration saved', tested: 'Application credentials valid.',
}
const zh = {
  applicationTablesInstructions: '请先在飞书文档中添加“文档小助手”。支持 Docx、电子表格和多维表格，图片及附件需完整解析。',
  sharingShort: '导入内容按当前知识库权限共享。', source: '查看原文',
  candidate_media_not_supported: '图片或附件尚不能完整导入，请手动导出核对后上传。', candidate_content_incomplete: '读取不完整或源内容发生变化，请稍后重试。', recoveryReview: '处理需要核查，已有内容已保留。',
  applicationMode: '文档应用授权', personalMode: '个人飞书 OAuth', applicationRestricted: '导入用户需拥有目标知识库的写入权限。', applicationInstructions: '请先在飞书文档中添加“文档小助手”。支持 Docx 正文和表格。',
  title: '导入飞书文档', connect: '连接我的飞书账号', disconnect: '解绑', unavailable: '请联系系统管理员配置并启用飞书应用。', url: '粘贴单篇飞书文档或文件链接', import: '导入', queued: '已提交，系统将在后台完成导入。', failed: '请检查飞书授权、文档权限和知识库配置。',
  recent: '我的导入记录 · 可重试同一文档', coverage: '导出可能不包含嵌入对象或附件，请在解析完成后核对完整性。', connected: '飞书账号已连接', callbackFailed: '授权失败或已过期，请返回知识库重新连接。', back: '返回 KnowHub',
  sharing: '导入后内容按当前知识库权限访问。共享知识库的其他读者可能读取这份文档，不会自动继承飞书个人文档权限。',
  updates: '当前仅支持首次导入，文档更新暂未开放；已有入库内容会保留。',
  open: '查看知识条目', indexState: '索引', wikiState: 'Wiki 后处理',
  pending: '处理中', verified: '索引已验证', indexed: '索引已验证，后处理继续中', synced: '已完成', attention: '导入失败', needs_attention: '导入失败', disabled: '未启用', completed: '已完成', not_started: '尚未开始', index_pending: '等待解析', accepted: '已接纳',
  app: '飞书文档应用', appID: 'App ID', secret: 'App Secret（留空保留）', callback: 'OAuth 回调地址', enabled: '启用飞书文档导入', save: '保存配置', test: '测试已保存的应用凭证', saved: '配置已保存', tested: '应用凭证有效，用户 Scope 和文档权限仍需授权验证。',
}
export const feishuMessages = { 'zh-CN': zh, 'en-US': en, 'ja-JP': en, 'ko-KR': en, 'ru-RU': en }
export const FEISHU_PENDING = 'knowhub_feishu_pending'
