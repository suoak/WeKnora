import { get, post, put } from '@/utils/request'
import { buildPortalSpacesPath, portalAccessRequestBody } from './portalContracts'
export { buildPortalSpacesPath, portalAccessRequestBody } from './portalContracts'

export type PortalAccessState = 'member' | 'not_member' | 'pending' | 'suspended'
export type PortalStatus = 'draft' | 'published' | 'archived'

export interface PortalStage {
  key: string
  name?: string
  description?: string
  display_order: number
}

export interface PortalSpace {
  tenant_id: number
  display_name: string
  description: string
  category: string
  responsible_team: string
  contact: string
  stages: string[]
  featured: boolean
  access_state: PortalAccessState
  current_role: 'owner' | 'admin' | 'contributor' | 'viewer' | null
  can_request_access: boolean
  interaction_action: 'enter' | 'none'
}

export interface PortalMySpace {
  tenant_id: number
  tenant_name: string
  role: 'owner' | 'admin' | 'contributor' | 'viewer'
}

export interface TenantAccessRequest {
  id: string
  tenant_id: number
  applicant_user_id: string
  source: 'portal'
  status: 'pending' | 'approved' | 'rejected' | 'cancelled'
  reason: string
  requested_role: 'viewer'
  reviewed_by?: string
  reviewed_at?: string
  review_note?: string
  created_at: string
  updated_at: string
}

export interface PortalAdminSpace {
  tenant_id: number
  tenant_name: string
  status: PortalStatus
  display_name: string
  description: string
  category: string
  responsible_team: string
  contact: string
  stages: string[]
  featured: boolean
  display_order: number
  allow_access_request: boolean
  interaction_organization_id?: string | null
  published_at?: string | null
}

export interface PortalConfigPayload {
  display_name: string
  description: string
  category: string
  responsible_team: string
  contact: string
  stages: string[]
  featured: boolean
  display_order: number
  allow_access_request: boolean
  interaction_organization_id: string | null
}

export interface PortalOrganizationOption { id: string; name: string }
type ListResponse<T> = { success: boolean; data: T[]; total?: number }

export const listPortalStages = () => get<{ success: boolean; data: PortalStage[] }>('/api/v1/portal/stages')

export const listPortalSpaces = (params: { q?: string; stage?: string; category?: string } = {}) =>
  get<ListResponse<PortalSpace>>(buildPortalSpacesPath(params))

export const listMyPortalSpaces = () => get<ListResponse<PortalMySpace>>('/api/v1/portal/my-spaces')
export const createPortalAccessRequest = (tenantId: number, reason: string) =>
  post<{ success: boolean; data: TenantAccessRequest }>(`/api/v1/portal/spaces/${tenantId}/access-requests`, portalAccessRequestBody(reason))
export const resolvePortalInteraction = (tenantId: number) =>
  post<{ success: boolean; data: { action: 'navigate'; path: string } }>(`/api/v1/portal/spaces/${tenantId}/interaction/resolve`)

export const listTenantPortalAccessRequests = (tenantId: number) =>
  get<ListResponse<TenantAccessRequest>>(`/api/v1/tenants/${tenantId}/access-requests`)
export const approveTenantPortalAccessRequest = (tenantId: number, requestId: string, reviewNote = '') =>
  post<{ success: boolean }>(`/api/v1/tenants/${tenantId}/access-requests/${requestId}/approve`, { review_note: reviewNote })
export const rejectTenantPortalAccessRequest = (tenantId: number, requestId: string, reviewNote = '') =>
  post<{ success: boolean }>(`/api/v1/tenants/${tenantId}/access-requests/${requestId}/reject`, { review_note: reviewNote })

export const listAdminPortalSpaces = () =>
  get<ListResponse<PortalAdminSpace>>('/api/v1/system/admin/portal/spaces')
export const getAdminPortalSpace = (tenantId: number) =>
  get<{ success: boolean; data: PortalAdminSpace }>(`/api/v1/system/admin/portal/spaces/${tenantId}`)
export const updateAdminPortalConfig = (tenantId: number, payload: PortalConfigPayload) =>
  put<{ success: boolean; data: PortalAdminSpace }>(`/api/v1/system/admin/portal/spaces/${tenantId}`, payload)
export const transitionAdminPortalStatus = (tenantId: number, action: 'publish' | 'unpublish' | 'archive') =>
  post<{ success: boolean; data: PortalAdminSpace }>(`/api/v1/system/admin/portal/spaces/${tenantId}/${action}`)
export const listPortalOrganizationOptions = () =>
  get<ListResponse<PortalOrganizationOption>>('/api/v1/system/admin/portal/organization-options')
