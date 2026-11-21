import { get, post } from '@/utils/request'

export interface UserNotification {
  id: string
  notification_type: string
  title: string
  content: string
  reference_type: string
  reference_id: string
  tenant_id: number
  is_read: boolean
  read_at: string | null
  created_at: string
  request_status?: 'pending' | 'approved' | 'rejected' | 'cancelled'
  reviewed_by?: string
  reviewer_name?: string
  reviewed_at?: string
}

export const listNotifications = (page = 1, unread = false) =>
  get<{ success: boolean; data: UserNotification[]; total: number }>(`/api/v1/me/notifications?page=${page}&page_size=20&unread=${unread}`)
export const notificationUnreadCount = () =>
  get<{ success: boolean; data: { unread_count: number } }>('/api/v1/me/notifications/unread-count')
export const readNotification = (id: string) => post(`/api/v1/me/notifications/${encodeURIComponent(id)}/read`)
export const readAllNotifications = () => post('/api/v1/me/notifications/read-all')
