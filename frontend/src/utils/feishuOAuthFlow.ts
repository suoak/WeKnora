import { FEISHU_PENDING } from '../i18n/feishuPersonal'

export const FEISHU_CALLBACK = 'knowhub_feishu_callback'
export const FEISHU_TARGET = 'feishu-import-target-draft'
const MAX_AGE = 30 * 60 * 1000
export type FeishuDraft = { kbId: string; url: string; path: string; createdAt: number; owner: string }
export type CallbackAttempt = { started: boolean; before: string; confirmed?: string; createdAt: number; owner: string }

export function currentOwner(): string {
  try { return String(JSON.parse(localStorage.getItem('weknora_user') || '{}').id || '') } catch { return '' }
}
export function safeImportPath(value: unknown): string {
  if (typeof value !== 'string' || /[\\\r\n\x00]/.test(value)) return '/platform/knowledge-bases'
  try {
    const u = new URL(value, 'https://knowhub.invalid')
    if (u.origin !== 'https://knowhub.invalid' || !/^\/platform\/(knowledge-bases(?:\/[a-zA-Z0-9_-]+)?|feishu\/imports)$/.test(u.pathname)) return '/platform/knowledge-bases'
    const params = new URLSearchParams()
    for (const key of ['kb', 'batch']) { const v = u.searchParams.get(key); if (v && /^[a-zA-Z0-9_-]+$/.test(v)) params.set(key, v) }
    return u.pathname + (params.size ? `?${params}` : '')
  } catch { return '/platform/knowledge-bases' }
}
export function readFlow<T extends { createdAt: number; owner: string }>(key: string): T | undefined {
  try {
    const v = JSON.parse(sessionStorage.getItem(key) || 'null')
    if (!v || typeof v.createdAt !== 'number' || Date.now() - v.createdAt > MAX_AGE || v.createdAt > Date.now() + 60000 || (v.owner && v.owner !== currentOwner())) { sessionStorage.removeItem(key); return }
    return v
  } catch { return }
}
export function saveFeishuDraft(kbId: string, url: string, path: string, before: string) {
  // If storage is unavailable, do not navigate away and silently lose the draft.
  const createdAt = Date.now(), owner = currentOwner()
  sessionStorage.setItem(FEISHU_PENDING, JSON.stringify({ kbId, url, path: safeImportPath(path), createdAt, owner }))
  sessionStorage.setItem(FEISHU_CALLBACK, JSON.stringify({ started: false, before, createdAt, owner }))
}
export function takeFeishuDraft(path: string, kbId?: string): FeishuDraft | undefined {
  const draft = readFlow<FeishuDraft>(FEISHU_PENDING)
  if (!draft || safeImportPath(draft.path) !== safeImportPath(path) || (kbId !== undefined && draft.kbId !== kbId)) return
  sessionStorage.removeItem(FEISHU_PENDING)
  sessionStorage.removeItem(FEISHU_CALLBACK)
  return draft
}

export function callbackFailure(error: { status?: number; category?: string; error?: string }): string {
  if (error.status === 401) return 'KnowHub 登录已失效，请登录后重新连接飞书。'
  if (error.category === 'cancelled') return '你已取消飞书授权，可以返回后重新连接。'
  if (['invalid_oauth_callback', 'invalid_or_expired_oauth_state'].includes(error.error || '')) return '本次授权已过期或已使用，请重新连接飞书。'
  if (error.error === 'feishu_oauth_application_configuration' || error.error === 'feishu_app_unavailable') return '飞书应用配置暂不可用，请联系管理员。'
  if (error.category === 'network' || error.category === 'timeout') return '暂时无法确认连接结果，请检查网络后查看连接状态。'
  return '本次连接未完成，请返回后重新连接飞书。'
}
