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
  const mySpaces = ref<PortalMySpace[]>([])
  const search = ref('')
  const selectedStage = ref(PORTAL_ALL_STAGE)
  const selectedCategory = ref('')
  const loading = ref(false)
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

  async function loadMySpaces() {
    const response = await listMyPortalSpaces()
    mySpaces.value = response.data || []
  }

  async function initialize() {
    if (initialized.value) return
    initialized.value = true
    try {
      await Promise.all([loadStages(), loadMySpaces(), loadSpaces()])
    } catch (error) {
      initialized.value = false
      throw error
    }
  }

  async function requestAccess(tenantId: number, reason: string) {
    requestState.value[tenantId] = 'submitting'
    try {
      await createPortalAccessRequest(tenantId, reason.trim())
      await loadSpaces()
    } finally {
      requestState.value[tenantId] = 'idle'
    }
  }

  function invalidate() {
    initialized.value = false
    loadVersion++
    if (typeof window !== 'undefined' && window.location.pathname === '/portal') {
      void loadSpaces()
    }
  }

  return {
    stages, spaces, mySpaces, search, selectedStage, selectedCategory,
    loading, requestState, categories, initialize, loadStages, loadSpaces,
    loadMySpaces, requestAccess, invalidate,
  }
})
