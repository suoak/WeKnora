export const DEFAULT_AUTHENTICATED_LANDING = '/portal'
export const AUTH_RETURN_TARGET_KEY = 'weknora_auth_return_target'
export const LITE_LAST_PATH_KEY = 'weknora_lite_last_path'

export interface AuthRedirectStorage {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
  removeItem(key: string): void
}

function sessionStore(): AuthRedirectStorage | null {
  return typeof sessionStorage === 'undefined' ? null : sessionStorage
}

/** Only same-origin SPA paths may cross an authentication boundary. */
export function normalizeAuthReturnTarget(value: unknown): string | null {
  if (typeof value !== 'string') return null
  const target = value.trim()
  if (!target.startsWith('/') || target.startsWith('//') || target.includes('\\')) return null

  try {
    const base = 'https://weknora.invalid'
    const parsed = new URL(target, base)
    if (parsed.origin !== base) return null
    if (/^\/(login|register)\/?$/.test(parsed.pathname)) return null
    if (parsed.pathname === '/') return DEFAULT_AUTHENTICATED_LANDING
    return `${parsed.pathname}${parsed.search}${parsed.hash}`
  } catch {
    return null
  }
}

export function isSafeLiteRestoreTarget(value: unknown): value is string {
  const target = normalizeAuthReturnTarget(value)
  return !!target && target.startsWith('/platform/') && !target.startsWith('/platform/organizations')
}

export function rememberAuthReturnTarget(
  value: unknown,
  storage: AuthRedirectStorage | null = sessionStore(),
): void {
  const target = normalizeAuthReturnTarget(value)
  if (!target || !storage) return
  try {
    storage.setItem(AUTH_RETURN_TARGET_KEY, target)
  } catch {
    // Authentication still works when sessionStorage is unavailable.
  }
}

export function consumeAuthReturnTarget(
  storage: AuthRedirectStorage | null = sessionStore(),
): string | null {
  if (!storage) return null
  try {
    const target = normalizeAuthReturnTarget(storage.getItem(AUTH_RETURN_TARGET_KEY))
    storage.removeItem(AUTH_RETURN_TARGET_KEY)
    return target
  } catch {
    return null
  }
}

export function clearAuthReturnTarget(
  storage: AuthRedirectStorage | null = sessionStore(),
): void {
  try {
    storage?.removeItem(AUTH_RETURN_TARGET_KEY)
  } catch {
    // Best-effort cleanup only.
  }
}

interface PostAuthLandingOptions {
  explicitTarget?: unknown
  storedTarget?: unknown
  liteMode?: boolean
  liteRecentTarget?: unknown
}

/** Explicit business target > captured deep link > Lite recovery > Portal. */
export function resolvePostAuthLanding(options: PostAuthLandingOptions = {}): string {
  const explicitTarget = normalizeAuthReturnTarget(options.explicitTarget)
  if (explicitTarget) return explicitTarget

  const storedTarget = normalizeAuthReturnTarget(options.storedTarget)
  if (storedTarget) return storedTarget

  if (options.liteMode && isSafeLiteRestoreTarget(options.liteRecentTarget)) {
    return normalizeAuthReturnTarget(options.liteRecentTarget)!
  }

  return DEFAULT_AUTHENTICATED_LANDING
}

export function resolveRootLanding(liteMode: boolean, liteRecentTarget: unknown): string {
  return resolvePostAuthLanding({ liteMode, liteRecentTarget })
}
