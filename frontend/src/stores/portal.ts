import { onScopeDispose, watch } from 'vue'
import { defineStore } from 'pinia'
import * as api from '@/api/portal'
import i18n from '@/i18n'
import { useNotificationsStore } from './notifications'
import { portalAssetRevision, portalSessionRevision } from './portalEvents'
import { createPortalStoreState } from './portalStoreState'

export const usePortalStore = defineStore('portal', () => {
  const state = createPortalStoreState(api, () => typeof localStorage === 'undefined' ? '' :
    [localStorage.getItem('weknora_token'), localStorage.getItem('weknora_selected_tenant_id'), localStorage.getItem('weknora_user')].join('|'),
  key => i18n.global.t(key), () => useNotificationsStore().changed())
  watch(portalAssetRevision, state.invalidate, { flush: 'sync' })
  watch(portalSessionRevision, state.clear, { flush: 'sync' })
  if (typeof window !== 'undefined') {
    const changedSession = (event: StorageEvent) => {
      if (event.key === null || ['weknora_token', 'weknora_user', 'weknora_selected_tenant_id', 'weknora_memberships'].includes(event.key)) state.clear()
    }
    window.addEventListener('storage', changedSession)
    onScopeDispose(() => window.removeEventListener('storage', changedSession))
  }
  return state
})
