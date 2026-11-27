import { computed, ref } from 'vue'
import { createVersionedRequestCoordinator } from './versionedRequest'

// A successful empty response is a snapshot too. Only reset() discards it.
export function createPortalResource<T>(request: () => Promise<T>, initial: () => T,
  sessionKey: () => string, denied: () => void) {
  const data = ref<T>(initial())
  const loaded = ref(false), pending = ref(false), error = ref(false)
  let generation = 0, needsFreshRequest = false
  const coordinator = createVersionedRequestCoordinator(async () => {
    const version = generation, session = sessionKey()
    const current = () => version === generation && session === sessionKey()
    pending.value = true
    try {
      const value = await request()
      return { value, current }
    } catch (cause: any) {
      if (current()) {
        if ([401, 403].includes(cause?.status ?? cause?.response?.status)) denied()
        else error.value = !loaded.value
      }
      throw cause
    } finally {
      if (current()) pending.value = false
    }
  }, ({ value, current }) => {
    if (!current()) return
    if (JSON.stringify(data.value) !== JSON.stringify(value)) data.value = value as typeof data.value
    loaded.value = true
    error.value = false
  })
  function invalidate() {
    generation++
    coordinator.invalidate()
    needsFreshRequest = coordinator.hasInFlightRequest()
    pending.value = false
  }
  return {
    data, loaded, pending, error,
    initialLoading: computed(() => pending.value && !loaded.value),
    refreshing: computed(() => pending.value && loaded.value),
    load() {
      const force = needsFreshRequest
      needsFreshRequest = false
      return coordinator.fetch(force)
    },
    invalidate,
    reset() { invalidate(); data.value = initial() as typeof data.value; loaded.value = false; error.value = false },
  }
}
