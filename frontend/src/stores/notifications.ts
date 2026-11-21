import { defineStore } from 'pinia'
import { ref } from 'vue'
import { notificationUnreadCount } from '@/api/notifications'

export const useNotificationsStore = defineStore('notifications', () => {
  const unreadCount = ref(0)
  const revision = ref(0)
  let session = 0
  let fetchVersion = 0

  async function refresh() {
    const version = ++fetchVersion
    const currentSession = session
    try {
      const response = await notificationUnreadCount()
      if (session === currentSession && fetchVersion === version && response.success) {
        unreadCount.value = response.data.unread_count
      }
    } catch { /* Keep the last successful count when a background poll fails. */ }
  }

  function changed() { revision.value++; void refresh() }
  function reset() { session++; fetchVersion++; unreadCount.value = 0; revision.value++ }
  return { unreadCount, revision, refresh, changed, reset }
})
