export interface PublicationSnapshot {
  tenant_id: number; knowledge_base_id: string; state: string; proposed_by: string
  reviewed_by: string | null; reviewed_at: string | null; reason: string; inherit_new_ordinary: boolean
  proposer_active_owner: boolean
  permissions: { owner: boolean; reviewer: boolean; mutations_enabled: boolean }
  resources: { kind: string; id: string; revision: number; version: string }[]
}
export function publicationBase(tenant: number, owner: boolean): string {
  if (!Number.isSafeInteger(tenant) || tenant <= 0) throw new Error('invalid tenant')
  return owner ? `/api/v1/tenants/${tenant}/knowledge-publication` : `/api/v1/system/admin/portal/spaces/${tenant}/knowledge-publication`
}
export function publicationActionAllowed(s: PublicationSnapshot, action: 'propose' | 'approve' | 'revoke', reason: string, ordinary: boolean): boolean {
  if (!s.permissions.mutations_enabled || !reason.trim() || reason.length > 512) return false
  if (action === 'revoke') return s.permissions.owner || s.permissions.reviewer
  if (!ordinary || s.resources.length < 3 || s.resources.some(r => !r.version)) return false
  return action === 'propose' ? s.permissions.owner : s.permissions.reviewer && s.state === 'pending_review' && !!s.proposed_by && s.proposer_active_owner
}
