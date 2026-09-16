import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import {
  createPortalAccessRequest,
  listMyPortalSpaces,
  listPortalSpaces,
  listPortalStages,
  type PortalMySpace,
  type PortalSpace,
  type PortalStage,
} from '@/api/portal'
import { displayPortalStage, portalCategories, PORTAL_ALL_STAGE, uniquePortalSpaces } from './portalState'
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
  let loadVersion = 0

  const categories = computed(() => portalCategories(spaces.value))

  async function loadStages() {
    stagesLoading.value = true
    stagesError.value = false
    try {
      const response = await listPortalStages()
      stages.value = (response.data || [])
        .map((stage)=>displayPortalStage(stage,(key)=>i18n.global.t(key)))
        .sort((left,right)=>left.display_order-right.display_order)
    } catch (error) {
      stagesError.value = true
      throw error
    } finally {
      stagesLoading.value = false
    }
  }

  async function loadSpaces() {
    const version = ++loadVersion
    loading.value = true
    try {
      const response = await listPortalSpaces({
        q: search.value.trim() || undefined,
        stage: selectedStage.value,
        category: selectedCategory.value || undefined,
      })
      if (version === loadVersion) spaces.value = uniquePortalSpaces(response.data || [])
    } finally {
      if (version === loadVersion) loading.value = false
    }
  }

  async function loadOverviewSpaces() {
    overviewLoading.value = true
    overviewError.value = false
    try {
      const response = await listPortalSpaces()
      overviewSpaces.value = uniquePortalSpaces(response.data || [])
      if (!search.value.trim() && selectedStage.value === PORTAL_ALL_STAGE && !selectedCategory.value) {
        spaces.value = overviewSpaces.value
      }
    } catch (error) {
      overviewError.value = true
      throw error
    } finally {
      overviewLoading.value = false
    }
  }

  async function loadMySpaces() {
    const response = await listMyPortalSpaces()
    mySpaces.value = response.data || []
  }

  async function initialize() {
    if (initialized.value) return
    initialized.value = true
    loading.value = true
    try {
      const [stageResult, overviewResult] = await Promise.allSettled([loadStages(), loadOverviewSpaces()])
      if (stageResult.status === 'rejected') console.warn('Portal stages failed to load', stageResult.reason)
      if (overviewResult.status === 'rejected') console.warn('Portal overview failed to load', overviewResult.reason)
      if (overviewResult.status === 'fulfilled'
        && (search.value.trim() || selectedStage.value !== PORTAL_ALL_STAGE || selectedCategory.value)) {
        await loadSpaces().catch((error) => console.warn('Portal filtered spaces failed to load', error))
      }
    } finally {
      loading.value = false
    }
  }

  async function requestAccess(tenantId: number, reason: string) {
    requestState.value[tenantId] = 'submitting'
    try {
      await createPortalAccessRequest(tenantId, reason.trim())
      // The write succeeded even if the follow-up refresh has a transient
      // failure. Mark both cached collections immediately so the dialog can
      // never offer a duplicate submission for an already-pending request.
      for (const collection of [overviewSpaces.value, spaces.value]) {
        const target = collection.find((space) => space.tenant_id === tenantId)
        if (target) {
          target.access_request_pending = true
          target.can_request_access = false
        }
      }
      await Promise.allSettled([loadOverviewSpaces(), loadSpaces()])
    } finally {
      requestState.value[tenantId] = 'idle'
    }
  }

  function invalidate() {
    initialized.value = false
    loadVersion++
    if (typeof window !== 'undefined' && window.location.pathname === '/portal') {
      void Promise.allSettled([loadOverviewSpaces(), loadSpaces()])
    }
  }

  return {
    stages, spaces, overviewSpaces, mySpaces, search, selectedStage, selectedCategory,
    loading, stagesLoading, overviewLoading, stagesError, overviewError,
    requestState, categories, initialize, loadStages, loadSpaces,
    loadOverviewSpaces, loadMySpaces, requestAccess, invalidate,
  }
})
