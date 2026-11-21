import { useNotificationsStore } from '@/stores/notifications'
import { computed, ref, watch } from 'vue'
import { defineStore } from 'pinia'
import {
  createPortalAccessRequest, listMyPortalSpaces, listPortalSpaces, listPortalStages,
  type PortalMySpace, type PortalSpace, type PortalStage,
} from '@/api/portal'
import { displayPortalStage, portalCategories, PORTAL_ALL_STAGE, uniquePortalSpaces } from './portalState'
import { portalAssetRevision, portalSessionRevision } from './portalEvents'
import i18n from '@/i18n'

export const usePortalStore = defineStore('portal', () => {
  const stages = ref<PortalStage[]>([])
  const spaces = ref<PortalSpace[]>([])
  const overviewSpaces = ref<PortalSpace[]>([])
  const mySpaces = ref<PortalMySpace[]>([])
  const search = ref('')
  const selectedStage = ref(PORTAL_ALL_STAGE)
  const selectedCategory = ref('')
  const loading = ref(false)
  const stagesLoading = ref(false)
  const overviewLoading = ref(false)
  const stagesError = ref(false)
  const overviewError = ref(false)
  const requestState = ref<Record<number, 'idle' | 'submitting'>>({})
  const initialized = ref(false)
  let epoch = 0
  let listVersion = 0
  let overviewVersion = 0
  let stageVersion = 0
  let refreshPromise: Promise<void> | null = null
  const categories = computed(() => portalCategories(spaces.value))
  const hasFilters = () => !!(search.value.trim() || selectedStage.value !== PORTAL_ALL_STAGE || selectedCategory.value)
  const sessionKey = () => typeof localStorage === 'undefined' ? '' :
    [localStorage.getItem('weknora_token'), localStorage.getItem('weknora_selected_tenant_id'), localStorage.getItem('weknora_user')].join('|')
  const guard = () => {
    const generation = epoch, session = sessionKey()
    return () => generation === epoch && session === sessionKey()
  }

  async function loadStages() {
    const current = guard(), version = ++stageVersion
    stagesLoading.value = true
    stagesError.value = false
    try {
      const response = await listPortalStages()
      if (current() && version === stageVersion) stages.value = (response.data || [])
        .map(stage => displayPortalStage(stage, key => i18n.global.t(key)))
        .sort((a, b) => a.display_order - b.display_order)
    } catch (error) {
      if (current() && version === stageVersion) stagesError.value = true
      throw error
    } finally {
      if (current() && version === stageVersion) stagesLoading.value = false
    }
  }

  async function loadSpaces() {
    const current = guard(), version = ++listVersion
    const filters = JSON.stringify([search.value, selectedStage.value, selectedCategory.value])
    loading.value = true
    try {
      const response = await listPortalSpaces({
        q: search.value.trim() || undefined, stage: selectedStage.value,
        category: selectedCategory.value || undefined,
      })
      if (current() && version === listVersion && filters === JSON.stringify([search.value, selectedStage.value, selectedCategory.value])) spaces.value = uniquePortalSpaces(response.data || [])
    } finally {
      if (current() && version === listVersion) loading.value = false
    }
  }

  async function loadOverviewSpaces() {
    const current = guard(), version = ++overviewVersion
    const listAtStart = hasFilters() ? listVersion : ++listVersion
    if (!hasFilters()) loading.value = false
    overviewLoading.value = true
    overviewError.value = false
    try {
      const response = await listPortalSpaces()
      if (!current() || version !== overviewVersion) return
      overviewSpaces.value = uniquePortalSpaces(response.data || [])
      if (!hasFilters() && listAtStart === listVersion) spaces.value = overviewSpaces.value
    } catch (error) {
      if (current() && version === overviewVersion) overviewError.value = true
      throw error
    } finally {
      if (current() && version === overviewVersion) overviewLoading.value = false
    }
  }

  async function loadMySpaces() {
    const current = guard()
    const response = await listMyPortalSpaces()
    if (current()) mySpaces.value = response.data || []
  }

  // Every page entry refreshes; mount/activation bursts share one request.
  // Failed initialization remains retryable without a page reload.
  function initialize(): Promise<void> {
    if (refreshPromise) return refreshPromise
    const current = guard()
    const task = (async () => {
      const results = await Promise.allSettled([
        ...(stages.value.length && !stagesError.value ? [] : [loadStages()]),
        loadOverviewSpaces(), ...(hasFilters() ? [loadSpaces()] : []),
      ])
      if (current()) initialized.value = results.every(r => r.status === 'fulfilled')
    })()
    refreshPromise = task
    void task.finally(() => { if (refreshPromise === task) refreshPromise = null })
    return task
  }

  async function requestAccess(tenantId: number, reason: string) {
    const current = guard()
    requestState.value[tenantId] = 'submitting'
    try {
      await createPortalAccessRequest(tenantId, reason.trim())
      useNotificationsStore().changed()
      if (!current()) return
      for (const collection of [overviewSpaces.value, spaces.value]) {
        const target = collection.find(s => s.tenant_id === tenantId)
        if (target) { target.access_request_pending = true; target.can_request_access = false }
      }
      await Promise.allSettled([loadOverviewSpaces(), loadSpaces()])
    } finally {
      if (current()) requestState.value[tenantId] = 'idle'
    }
  }

  function invalidate() {
    epoch++
    initialized.value = false
    refreshPromise = null
    loading.value = false
    overviewLoading.value = false
    stagesLoading.value = false
  }
  function clear() {
    invalidate()
    stages.value = []; spaces.value = []; overviewSpaces.value = []; mySpaces.value = []
    requestState.value = {}; search.value = ''; selectedStage.value = PORTAL_ALL_STAGE; selectedCategory.value = ''
    stagesError.value = false; overviewError.value = false
  }
  watch(portalAssetRevision, invalidate, { flush: 'sync' })
  watch(portalSessionRevision, clear, { flush: 'sync' })
  return {
    stages, spaces, overviewSpaces, mySpaces, search, selectedStage, selectedCategory,
    loading, stagesLoading, overviewLoading, stagesError, overviewError, initialized,
    requestState, categories, initialize, loadStages, loadSpaces, loadOverviewSpaces,
    loadMySpaces, requestAccess, invalidate, clear,
  }
})
