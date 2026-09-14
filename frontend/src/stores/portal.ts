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

export const usePortalStore = defineStore('portal', () => {
  const stages = ref<PortalStage[]>([])
  const spaces = ref<PortalSpace[]>([])
  const overviewSpaces = ref<PortalSpace[]>([])
  const mySpaces = ref<PortalMySpace[]>([])
  const search = ref('')
  const selectedStage = ref(PORTAL_ALL_STAGE)
  const selectedCategory = ref('')
  const loading = ref(false)
  const overviewLoading = ref(false)
  const requestState = ref<Record<number, 'idle' | 'submitting'>>({})
  const initialized = ref(false)
  let loadVersion = 0

  const categories = computed(() => portalCategories(spaces.value))

  async function loadStages() {
    const response = await listPortalStages()
    stages.value = (response.data || []).map(displayPortalStage)
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
    try {
      const response = await listPortalSpaces()
      overviewSpaces.value = uniquePortalSpaces(response.data || [])
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
      if (overviewResult.status === 'rejected') throw overviewResult.reason
      if (!search.value.trim() && selectedStage.value === PORTAL_ALL_STAGE && !selectedCategory.value) {
        spaces.value = overviewSpaces.value
      } else {
        await loadSpaces()
      }
    } catch (error) {
      initialized.value = false
      throw error
    } finally {
      loading.value = false
    }
  }

  async function requestAccess(tenantId: number, reason: string) {
    requestState.value[tenantId] = 'submitting'
    try {
      await createPortalAccessRequest(tenantId, reason.trim())
      await Promise.all([loadOverviewSpaces(), loadSpaces()])
    } finally {
      requestState.value[tenantId] = 'idle'
    }
  }

  function invalidate() {
    initialized.value = false
    loadVersion++
    if (typeof window !== 'undefined' && window.location.pathname === '/portal') {
      void Promise.all([loadOverviewSpaces(), loadSpaces()])
    }
  }

  return {
    stages, spaces, overviewSpaces, mySpaces, search, selectedStage, selectedCategory,
    loading, overviewLoading, requestState, categories, initialize, loadStages, loadSpaces,
    loadOverviewSpaces, loadMySpaces, requestAccess, invalidate,
  }
})
