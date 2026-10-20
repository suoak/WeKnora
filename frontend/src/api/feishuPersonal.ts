import { get, post, put, del } from '@/utils/request'
export type FeishuAuthMode = 'user_oauth' | 'application_grant'
export type FeishuStatus = { auth_mode?: FeishuAuthMode; configured: boolean; available: boolean; connected: boolean; supported_resource_types?: string[] }
export type FeishuImport = { id: string; url: string; resource_type: string; knowledge_id: string; status: string; coverage: string; index_status?: string; wiki_status?: string; error_category?: string }
export const feishuStatus = () => get<FeishuStatus>('/api/v1/me/feishu')
export const authorizeFeishu = () => post<{ authorization_url: string }>('/api/v1/me/feishu/authorize')
export const disconnectFeishu = () => del('/api/v1/me/feishu')
export const completeFeishu = (state: string, code: string) => post('/api/v1/me/feishu/callback', {}, { params: { state, code } })
export const importFeishu = (kb: string, url: string) => post<FeishuImport>(`/api/v1/knowledge-bases/${encodeURIComponent(kb)}/feishu`, { url }, { timeout: 180000 })
export const listFeishuImports = (kb: string) => get<FeishuImport[]>(`/api/v1/knowledge-bases/${encodeURIComponent(kb)}/feishu`)
export const getFeishuApp = () => get<{ configured: boolean; auth_mode?: FeishuAuthMode; app_id?: string; redirect_uri?: string; enabled?: boolean }>('/api/v1/system/admin/feishu')
export const saveFeishuApp = (config: { auth_mode?: FeishuAuthMode; app_id: string; client_secret: string; redirect_uri: string; enabled: boolean }) => put('/api/v1/system/admin/feishu', config)
export const testFeishuApp = () => post('/api/v1/system/admin/feishu/test')
export function isFeishuDocumentURL(value: string): boolean {
  try { const u = new URL(value); return u.protocol === 'https:' && (u.hostname === 'feishu.cn' || u.hostname.endsWith('.feishu.cn')) } catch { return false }
}
