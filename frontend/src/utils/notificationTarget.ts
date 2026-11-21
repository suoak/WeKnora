import type { UserNotification } from '../api/notifications'

// Only known server business types map to fixed internal routes.
export function notificationTarget(row: Pick<UserNotification, 'reference_type' | 'notification_type' | 'tenant_id'>): string | null {
  if (row.reference_type !== 'tenant_access_request' || !Number.isSafeInteger(row.tenant_id) || row.tenant_id <= 0) return null
  if (row.notification_type === 'access_request_submitted') return '/platform/settings?section=members&access_requests=1'
  if (row.notification_type === 'access_request_approved') return '/platform/knowledge-bases'
  return null
}
