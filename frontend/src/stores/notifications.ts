import { defineStore } from 'pinia'
import { ref } from 'vue'
import { notificationUnreadCount } from '@/api/notifications'
import { invalidatePortalAssets } from './portalEvents'

export const useNotificationsStore = defineStore('notifications', () => {
  const unreadCount = ref(0)
  const revision = ref(0)
  let session = 0
  let fetchVersion = 0

  async function refresh(invalidateList = true) {
    const version = ++fetchVersion
    const currentSession = session
    try {
      const response = await notificationUnreadCount()
      if (session === currentSession && fetchVersion === version && response.success) {
        if (unreadCount.value !== response.data.unread_count) invalidatePortalAssets()
        unreadCount.value = response.data.unread_count
        // A request may be resolved after its reminder was already read. Count
        // equality must not prevent open inboxes and approval panels refreshing.
        if (invalidateList) revision.value++
      }
    } catch { /* Keep the last successful count when a background poll fails. */ }
  }

  function changed() { revision.value++; void refresh(false) }
  function reset() { session++; fetchVersion++; unreadCount.value = 0; revision.value++ }
  return { unreadCount, revision, refresh, changed, reset }
})
