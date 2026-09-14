export const PORTAL_INTENT_STORAGE_KEY = 'weknora_portal_switch_intent'
export const PORTAL_INTENT_TTL_MS = 2 * 60 * 1000

export type PortalIntentAction = 'search' | 'ask'

export interface PortalIntent {
  version: 1
  action: PortalIntentAction
  targetTenantId: number
  createdAt: number
}

export interface PortalIntentStorage {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
  removeItem(key: string): void
}

const sessionStorageOrNull = (): PortalIntentStorage | null => {
  try {
    return typeof sessionStorage === 'undefined' ? null : sessionStorage
  } catch {
    return null
  }
}

const isStrictPortalIntent = (value: unknown): value is PortalIntent => {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false
  const record = value as Record<string, unknown>
  if (Object.keys(record).sort().join(',') !== 'action,createdAt,targetTenantId,version') return false
  return record.version === 1
    && (record.action === 'search' || record.action === 'ask')
    && typeof record.targetTenantId === 'number'
    && Number.isSafeInteger(record.targetTenantId)
    && record.targetTenantId > 0
    && typeof record.createdAt === 'number'
    && Number.isFinite(record.createdAt)
}

export function clearPortalIntent(storage: PortalIntentStorage | null = sessionStorageOrNull()): void {
  if (!storage) return
  try {
    storage.removeItem(PORTAL_INTENT_STORAGE_KEY)
  } catch {
    // Continuation state must never break app startup or logout.
  }
}

export function createPortalIntent(
  action: PortalIntentAction,
  targetTenantId: number,
  storage: PortalIntentStorage | null = sessionStorageOrNull(),
  now = Date.now(),
): boolean {
  if (!storage || !Number.isSafeInteger(targetTenantId) || targetTenantId <= 0) return false
  try {
    const intent: PortalIntent = { version: 1, action, targetTenantId, createdAt: now }
    storage.setItem(PORTAL_INTENT_STORAGE_KEY, JSON.stringify(intent))
    return true
  } catch {
    clearPortalIntent(storage)
    return false
  }
}

export function readPortalIntent(
  storage: PortalIntentStorage | null = sessionStorageOrNull(),
  now = Date.now(),
): PortalIntent | null {
  if (!storage) return null
  try {
    const raw = storage.getItem(PORTAL_INTENT_STORAGE_KEY)
    if (!raw) return null
    const parsed: unknown = JSON.parse(raw)
    if (!isStrictPortalIntent(parsed)
      || parsed.createdAt > now
      || now - parsed.createdAt > PORTAL_INTENT_TTL_MS) {
      clearPortalIntent(storage)
      return null
    }
    return parsed
  } catch {
    clearPortalIntent(storage)
    return null
  }
}

export type PortalIntentConsumeResult =
  | 'none'
  | 'tenant-mismatch'
  | 'inaccessible'
  | 'action-unavailable'
  | 'executed'
  | 'execution-failed'

export function consumePortalIntent(options: {
  activeTenantId: number | null
  isTargetAccessible: (tenantId: number) => boolean
  actions: Partial<Record<PortalIntentAction, () => void>>
  storage?: PortalIntentStorage | null
  now?: number
}): PortalIntentConsumeResult {
  const storage = options.storage === undefined ? sessionStorageOrNull() : options.storage
  const intent = readPortalIntent(storage, options.now)
  if (!intent) return 'none'
  if (Number(options.activeTenantId) !== intent.targetTenantId) {
    clearPortalIntent(storage)
    return 'tenant-mismatch'
  }
  if (!options.isTargetAccessible(intent.targetTenantId)) {
    clearPortalIntent(storage)
    return 'inaccessible'
  }
  const execute = options.actions[intent.action]
  clearPortalIntent(storage)
  if (!execute) return 'action-unavailable'
  try {
    execute()
    return 'executed'
  } catch {
    return 'execution-failed'
  }
}
