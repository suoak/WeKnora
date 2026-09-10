// Tenant-switch navigation helper.
//
// Resource-free list pages are preserved across a switch. Resource-bound
// contexts fall back to their list or Portal. A full navigation then resets
// tenant-scoped stores, SSE connections, and in-flight requests.

import { updateMyPreferences } from '@/api/auth'
import { useAuthStore } from '@/stores/auth'
import { usePortalStore } from '@/stores/portal'
import { tenantSwitchTargetPath } from './tenantSwitchPolicy'

export { tenantSwitchTargetPath } from './tenantSwitchPolicy'

/** Perform a hard navigation after changing the active tenant. */
export function navigateAfterTenantSwitch(): void {
  window.location.href = tenantSwitchTargetPath(window.location.pathname)
}

export interface WorkspaceSwitchTarget {
  tenantId: number
  tenantName: string
  role?: string
  roleLabel?: string
}

/** Shared tenant-switch workflow used by the existing switcher and Portal. */
export function switchWorkspaceAndNavigate(target: WorkspaceSwitchTarget): void {
  const authStore = useAuthStore()
  const homeTenantId = Number(authStore.user?.tenant_id ?? 0)
  authStore.setSelectedTenant(target.tenantId, target.tenantName)
  usePortalStore().invalidate()
  stashTenantSwitchToast({
    name: target.tenantName || `#${target.tenantId}`,
    role: target.roleLabel,
    roleEnum: target.role,
  })
  const persist = persistLastActiveTenantPreference(target.tenantId === homeTenantId ? null : target.tenantId)
  Promise.race([persist, new Promise((resolve) => setTimeout(resolve, 500))])
    .finally(() => navigateAfterTenantSwitch())
}

// 切换成功后的 toast 跨 hard reload 传递：调用方在 reload 前把信息塞进
// sessionStorage，App.vue 启动时 consume 一次再弹出。直接在 reload 前调
// NotifyPlugin 会被刷掉，根本来不及看清。
const PENDING_TOAST_KEY = 'weknora_pending_tenant_switch_toast'

export interface PendingTenantSwitchToast {
  name: string
  /** Human-readable role label, e.g. "所有者" / "Owner". Drives the role chip text. */
  role?: string
  /** Raw role enum, e.g. "owner". Drives the role chip color + icon. */
  roleEnum?: string
}

export function stashTenantSwitchToast(payload: PendingTenantSwitchToast): void {
  try {
    sessionStorage.setItem(PENDING_TOAST_KEY, JSON.stringify(payload))
  } catch {
    // sessionStorage 写失败（隐私模式等）就静默放弃，toast 是锦上添花
  }
}

export function consumePendingTenantSwitchToast(): PendingTenantSwitchToast | null {
  try {
    const raw = sessionStorage.getItem(PENDING_TOAST_KEY)
    if (!raw) return null
    sessionStorage.removeItem(PENDING_TOAST_KEY)
    const parsed = JSON.parse(raw)
    if (parsed && typeof parsed.name === 'string') {
      return {
        name: parsed.name,
        role: typeof parsed.role === 'string' ? parsed.role : undefined,
        roleEnum: typeof parsed.roleEnum === 'string' ? parsed.roleEnum : undefined,
      }
    }
    return null
  } catch {
    return null
  }
}

/**
 * Persist the user's "last active tenant" preference server-side so a
 * fresh login (new device, new refresh token, cleared browser) drops
 * them back into this workspace instead of always bouncing to their
 * home tenant.
 *
 * Conventions:
 *   - Pass the target tenant id when switching to a peer tenant.
 *   - Pass `null` (which sends `0`) when switching back to the home
 *     tenant — that clears the preference and reverts the user to the
 *     "always start at home" default.
 *
 * Fire-and-forget: callers usually trigger a full-page reload right
 * after; this returns the in-flight promise so callers can race it
 * against a small budget if they want best-effort completion before
 * navigation. Failures are logged but never thrown — losing one
 * persist is recoverable on the next switch.
 */
export function persistLastActiveTenantPreference(
  tenantId: number | null,
): Promise<void> {
  const payload = { last_active_tenant_id: tenantId == null ? 0 : tenantId }
  return updateMyPreferences(payload)
    .then((res) => {
      if (!res.success) {
        console.warn('persistLastActiveTenantPreference: server rejected update', res.message)
      }
    })
    .catch((err) => {
      console.warn('persistLastActiveTenantPreference: network/serialization error', err)
    })
}
