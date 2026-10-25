import { get, post, put, del } from '@/utils/request'
export type FeishuAuthMode = 'user_oauth' | 'application_grant'
export type FeishuCapabilities = { application_enabled: boolean; personal_enabled: boolean }
export type FeishuStatus = { auth_mode?: FeishuAuthMode; v2?: boolean; capabilities?: FeishuCapabilities; configured: boolean; available: boolean; connected: boolean; supported_resource_types?: string[]; test_http_callback?: boolean; test_http_callback_expires_at?: string }
export type FeishuImport = { id: string; url: string; resource_type: string; knowledge_id: string; status: string; coverage: string; index_status?: string; wiki_status?: string; error_category?: string; data_source_id?: string; source_status?: string }
export const feishuStatus = () => get<FeishuStatus>('/api/v1/me/feishu')
export const authorizeFeishu = () => post<{ authorization_url: string }>('/api/v1/me/feishu/authorize')
export const disconnectFeishu = () => del('/api/v1/me/feishu')
export const completeFeishu = (state: string, code: string) => post('/api/v1/me/feishu/callback', {}, { params: { state, code } })
export const importFeishu = (kb: string, url: string, auth_mode?: FeishuAuthMode) => post<FeishuImport>(`/api/v1/knowledge-bases/${encodeURIComponent(kb)}/feishu`, { url, auth_mode }, { timeout: 180000 })
export const listFeishuImports = (kb: string) => get<FeishuImport[]>(`/api/v1/knowledge-bases/${encodeURIComponent(kb)}/feishu`)
export const enableFeishuSource = (kb: string, id: string) => post<{ id: string; status: string; schedule: string }>(`/api/v1/knowledge-bases/${encodeURIComponent(kb)}/feishu/imports/${encodeURIComponent(id)}/source`, {})
export const feishuSourceAction = (kb: string, id: string, action: 'sync' | 'pause' | 'resume' | 'rebind') => post(`/api/v1/knowledge-bases/${encodeURIComponent(kb)}/feishu/imports/${encodeURIComponent(id)}/source/${action}`, {})
export const getFeishuApp = () => get<{ configured: boolean; v2_available?: boolean; capabilities?: FeishuCapabilities; auth_mode?: FeishuAuthMode; app_id?: string; redirect_uri?: string; enabled?: boolean }>('/api/v1/system/admin/feishu')
export const saveFeishuApp = (config: { capabilities?: FeishuCapabilities; auth_mode?: FeishuAuthMode; app_id: string; client_secret: string; redirect_uri: string; enabled: boolean }) => put('/api/v1/system/admin/feishu', config)
export const testFeishuApp = () => post('/api/v1/system/admin/feishu/test')
export type FeishuConnection = { id?: string; kind: FeishuAuthMode; connected: boolean }
export type FeishuResource = { id: string; name: string; type: string; url?: string; has_children: boolean; space_id?: string; is_shortcut?: boolean }
export type FeishuResourcePage = { items: FeishuResource[]; has_more: boolean; cursor?: string; coverage: string }
export type FeishuPreview = { url: string; resource_type?: string; revision?: string; status: string; error_category?: string }
export type FeishuBatchItem = { id: string; ordinal: number; url: string; status: string; error_category?: string; import_id?: string; retry_count: number }
export type FeishuBatch = { batch: { id: string; kb_id: string; auth_mode: FeishuAuthMode }; items: FeishuBatchItem[]; counts: Record<string, number>; status: string }
export const feishuConnections = () => get<FeishuConnection[]>('/api/v1/me/feishu/connections')
export const createPrivateFeishuKB = (name: string) => post<{ id: string }>('/api/v1/me/feishu/private-knowledge-base', { name })
export const feishuResources = (params: Record<string, string | number>) => get<FeishuResourcePage>('/api/v1/me/feishu/resources', { params })
export const previewFeishu = (kb: string, auth_mode: FeishuAuthMode, connection_id: string, links: string[]) => post<FeishuPreview[]>(`/api/v1/knowledge-bases/${encodeURIComponent(kb)}/feishu/preview`, { auth_mode, connection_id, links }, { timeout: 45000 })
export const createFeishuBatch = (kb: string, auth_mode: FeishuAuthMode, connection_id: string, links: string[], key: string) => post<FeishuBatch>(`/api/v1/knowledge-bases/${encodeURIComponent(kb)}/feishu/batches`, { auth_mode, connection_id, links }, { headers: { 'Idempotency-Key': key } })
export const getFeishuBatch = (id: string) => get<FeishuBatch>(`/api/v1/feishu/batches/${encodeURIComponent(id)}`)
export type FeishuBatchHistory = { id: string; kb_id: string; auth_mode: FeishuAuthMode; created_at: string }
export const listFeishuBatches = (kb: string, offset = 0) => get<FeishuBatchHistory[]>(`/api/v1/knowledge-bases/${encodeURIComponent(kb)}/feishu/batches`, { params: { offset } })
export const retryFeishuBatch = (id: string) => post(`/api/v1/feishu/batches/${encodeURIComponent(id)}/retry`)
export type FeishuCollaboration = { id: string; item_id: string; action: string; contact: string; note: string; result: string; created_at: string }
export const listFeishuCollaboration = (id: string) => get<FeishuCollaboration[]>(`/api/v1/feishu/batches/${encodeURIComponent(id)}/collaboration`)
export const recordFeishuCollaboration = (id: string, item: string, action: 'note' | 'reported' | 'recheck', contact: string, note: string) => post<FeishuCollaboration>(`/api/v1/feishu/batches/${encodeURIComponent(id)}/items/${encodeURIComponent(item)}/collaboration`, { action, contact, note }, { timeout: 45000 })
export const approveFeishuSharing = (kb: string, connection_id: string, url: string, reason: string, confirmed: boolean) => post(`/api/v1/knowledge-bases/${encodeURIComponent(kb)}/feishu/sharing-approvals`, { connection_id, url, reason, confirmed }, { timeout: 45000 })
export type FeishuSharingApproval = { id: string; resource_type: string; revision: string; state: string; reason: string; created_at: string; expires_at: string }
export const listFeishuSharingApprovals = (kb: string, offset = 0) => get<FeishuSharingApproval[]>(`/api/v1/knowledge-bases/${encodeURIComponent(kb)}/feishu/sharing-approvals`, { params: { offset } })
export const revokeFeishuSharingApproval = (kb: string, id: string) => post(`/api/v1/knowledge-bases/${encodeURIComponent(kb)}/feishu/sharing-approvals/${encodeURIComponent(id)}/revoke`, {})
export function isFeishuDocumentURL(value: string): boolean {
  try { const u = new URL(value); return u.protocol === 'https:' && (u.hostname === 'feishu.cn' || u.hostname.endsWith('.feishu.cn')) } catch { return false }
}
