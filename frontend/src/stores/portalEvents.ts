import { ref } from 'vue'

// Shared by auth and transport without a dependency on their stores.
export const portalAssetRevision = ref(0)
export const portalSessionRevision = ref(0)
export const invalidatePortalAssets = () => { portalAssetRevision.value++ }
export const resetPortalSession = () => { portalSessionRevision.value++; portalAssetRevision.value++ }

export function isAssetMutation(method: string | undefined, url: string | undefined): boolean {
  return /^(post|put|patch|delete)$/i.test(method || '')
    && /\/api\/v1\/(?:knowledge(?:-bases)?(?:\/|$)|data-sources(?:\/|$))/.test(url || '')
}
