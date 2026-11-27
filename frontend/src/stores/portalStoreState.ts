import { computed, ref } from 'vue'
import type * as PortalAPI from '../api/portal'
import type { PortalSpace } from '../api/portal'
import { displayPortalStage, portalCategories, PORTAL_ALL_STAGE, uniquePortalSpaces } from './portalState'
import { createPortalResource } from './portalResource'

export const PORTAL_REFRESH_MS = 300_000
const RETRY_DELAYS = [5_000, 15_000, 30_000]

export function createPortalStoreState(api: Pick<typeof PortalAPI, 'listPortalStages' | 'listPortalSpaces' | 'listMyPortalSpaces' | 'createPortalAccessRequest'>,
  sessionKey: () => string, translate: (key: string) => string, changed: () => void, now = Date.now) {
  const { listPortalStages, listPortalSpaces, listMyPortalSpaces, createPortalAccessRequest } = api
  const stageResource = createPortalResource(async () => {
    const response = await listPortalStages()
    if (!response.success) throw new Error('Portal stages failed')
    return (response.data || []).map(stage => displayPortalStage(stage, translate)).sort((a, b) => a.display_order - b.display_order)
  }, () => [] as PortalAPI.PortalStage[], sessionKey, deny)
  const overviewResource = createPortalResource(async () => {
    const response = await listPortalSpaces()
    if (!response.success) throw new Error('Portal overview failed')
    return uniquePortalSpaces(response.data || [])
  }, () => [] as PortalSpace[], sessionKey, deny)
  const stages = stageResource.data
  const spaces = ref<PortalSpace[]>([])
  const overviewSpaces = overviewResource.data
  const mySpaces = ref<PortalAPI.PortalMySpace[]>([])
  const search = ref('')
  const selectedStage = ref(PORTAL_ALL_STAGE)
  const selectedCategory = ref('')
  const loading = ref(false)
  const stagesLoading = stageResource.initialLoading
  const overviewLoading = overviewResource.initialLoading
  const stagesError = stageResource.error
  const overviewError = overviewResource.error
  const initialLoading = computed(() => stagesLoading.value || overviewLoading.value)
  const refreshing = computed(() => stageResource.refreshing.value || overviewResource.refreshing.value)
  const requestState = ref<Record<number, 'idle' | 'submitting'>>({})
  const initialized = ref(false)
  let epoch = 0
  let listVersion = 0
  let refreshPromise: Promise<void> | null = null
  const nextRefreshAt = ref(0)
  let failures = 0
  const categories = computed(() => portalCategories(spaces.value))
  const hasFilters = () => !!(search.value.trim() || selectedStage.value !== PORTAL_ALL_STAGE || selectedCategory.value)
  const guard = () => {
    const generation = epoch, session = sessionKey()
    return () => generation === epoch && session === sessionKey()
  }

  const loadStages = stageResource.load
  const filteredFlights = new Map<string, Promise<void>>()

  function loadSpaces(): Promise<void> {
    if (!hasFilters()) return loadOverviewSpaces()
    const key = JSON.stringify([epoch, search.value.trim(), selectedStage.value, selectedCategory.value])
    const existing = filteredFlights.get(key)
    if (existing) return existing
    const task = loadFilteredSpaces().finally(() => { if (filteredFlights.get(key) === task) filteredFlights.delete(key) })
    filteredFlights.set(key, task)
    return task
  }
  async function loadFilteredSpaces() {
    const current = guard(), version = ++listVersion
    const filters = JSON.stringify([search.value, selectedStage.value, selectedCategory.value])
    loading.value = true
    try {
      const response = await listPortalSpaces({
        q: search.value.trim() || undefined, stage: selectedStage.value,
        category: selectedCategory.value || undefined,
      })
      if (!response.success) throw new Error('Portal spaces failed')
      if (current() && version === listVersion && filters === JSON.stringify([search.value, selectedStage.value, selectedCategory.value])) {
        const next = uniquePortalSpaces(response.data || [])
        if (JSON.stringify(spaces.value) !== JSON.stringify(next)) spaces.value = next
      }
    } catch (error: any) {
      if (current() && [401, 403].includes(error?.status ?? error?.response?.status)) deny()
      throw error
    } finally {
      if (current() && version === listVersion) loading.value = false
    }
  }

  async function loadOverviewSpaces() {
    const current = guard()
    const listAtStart = hasFilters() ? listVersion : ++listVersion
    if (!hasFilters()) loading.value = false
    await overviewResource.load()
    if (current() && !hasFilters() && listAtStart === listVersion
      && JSON.stringify(spaces.value) !== JSON.stringify(overviewSpaces.value)) spaces.value = overviewSpaces.value
  }

  async function loadMySpaces() {
    const current = guard()
    try {
      const response = await listMyPortalSpaces()
      if (current() && response.success && JSON.stringify(mySpaces.value) !== JSON.stringify(response.data || [])) mySpaces.value = response.data || []
    } catch (error: any) {
      if (current() && [401, 403].includes(error?.status ?? error?.response?.status)) deny()
      throw error
    }
  }

  // All passive triggers share freshness/backoff and the same in-flight work.
  function initialize(force = false): Promise<void> {
    if (refreshPromise) return refreshPromise
    if (!force && now() < nextRefreshAt.value) return Promise.resolve()
    const current = guard()
    const task = (async () => {
      const results = await Promise.allSettled([
        loadStages(),
        loadOverviewSpaces(), ...(hasFilters() ? [loadSpaces()] : []),
      ])
      if (current()) {
        initialized.value = results.every(r => r.status === 'fulfilled')
        if (initialized.value) { failures = 0; nextRefreshAt.value = now() + PORTAL_REFRESH_MS }
        else { nextRefreshAt.value = now() + (RETRY_DELAYS[failures++] ?? PORTAL_REFRESH_MS) }
      }
    })()
    refreshPromise = task
    void task.finally(() => { if (refreshPromise === task) refreshPromise = null })
    return task
  }

  async function requestAccess(tenantId: number, reason: string) {
    const session = sessionKey()
    // The successful write emits an asset invalidation in the transport before
    // this continuation runs. Only a different session cancels its local mark.
    const current = () => session === sessionKey()
    requestState.value[tenantId] = 'submitting'
    try {
      await createPortalAccessRequest(tenantId, reason.trim())
      changed()
      if (!current()) return
      for (const collection of [overviewSpaces.value, spaces.value]) {
        const target = collection.find(s => s.tenant_id === tenantId)
        if (target) { target.access_request_pending = true; target.can_request_access = false }
      }
      invalidate()
      await initialize(true)
    } finally {
      if (session === sessionKey()) requestState.value[tenantId] = 'idle'
    }
  }

  function invalidate() {
    epoch++
    initialized.value = false
    refreshPromise = null
    nextRefreshAt.value = 0
    failures = 0
    loading.value = false
    stageResource.invalidate()
    overviewResource.invalidate()
  }
  function clear() {
    invalidate()
    stageResource.reset(); overviewResource.reset()
    spaces.value = []; mySpaces.value = []
    requestState.value = {}; search.value = ''; selectedStage.value = PORTAL_ALL_STAGE; selectedCategory.value = ''
    stagesError.value = false; overviewError.value = false
  }
  function deny() {
    clear()
    overviewError.value = true
    nextRefreshAt.value = now() + PORTAL_REFRESH_MS
  }
  return {
    stages, spaces, overviewSpaces, mySpaces, search, selectedStage, selectedCategory,
    loading, stagesLoading, overviewLoading, stagesError, overviewError, initialized,
    initialLoading, refreshing, nextRefreshAt,
    requestState, categories, initialize, loadStages, loadSpaces, loadOverviewSpaces,
    loadMySpaces, requestAccess, invalidate, clear,
  }
}
