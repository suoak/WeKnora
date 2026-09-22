import type {
  MCPAccessKey,
  MCPAccessStatus,
  MCPKeyPayload,
  MCPKeyResponse,
  MCPSecretResult,
  MCPCapability,
  MCPTenantScope,
} from '@/api/mcpAccessKeys'

export type CredentialLifecycleState = 'active' | 'expired' | 'revoked'
export type CredentialStatusFilter = 'all' | 'active' | 'revoked'
export type CredentialAction = 'edit' | 'rotate' | 'revoke' | 'reconnect'

type ReconnectableCredential = Omit<MCPAccessKey, 'capabilities' | 'tenant_scopes' | 'expires_at' | 'status'> & {
  capabilities?: MCPCapability[] | null
  tenant_scopes?: MCPTenantScope[] | null
  expires_at?: string | null
  status?: MCPAccessStatus | null
}

/** One lifecycle source of truth for badges, filters and available actions. */
export function getCredentialLifecycleState(
  credential: Pick<ReconnectableCredential, 'status' | 'expires_at' | 'revoked_at'>,
  now = Date.now(),
): CredentialLifecycleState {
  if (credential.status === 'revoked' || credential.revoked_at) return 'revoked'
  if (credential.status === 'expired') return 'expired'
  if (credential.expires_at) {
    const expiry = new Date(credential.expires_at).getTime()
    if (Number.isFinite(expiry) && expiry <= now) return 'expired'
  }
  return 'active'
}

export function credentialLifecycleActions(
  credential: Pick<ReconnectableCredential, 'status' | 'expires_at' | 'revoked_at'>,
): CredentialAction[] {
  const state = getCredentialLifecycleState(credential)
  if (state === 'revoked') return ['reconnect']
  return ['edit', 'rotate', 'revoke']
}

export function credentialMatchesStatusFilter(
  credential: Pick<ReconnectableCredential, 'status' | 'expires_at' | 'revoked_at'>,
  filter: CredentialStatusFilter,
): boolean {
  return filter === 'all' || getCredentialLifecycleState(credential) === filter
}

function cloneTenantScopes(scopes?: MCPTenantScope[] | null): MCPTenantScope[] {
  if (!Array.isArray(scopes)) return []
  return scopes.map(scope => ({
    tenant_id: scope.tenant_id,
    kb_scope_mode: scope.kb_scope_mode,
    knowledge_bases: Array.isArray(scope.knowledge_bases)
      ? scope.knowledge_bases.map(kb => ({ ...kb }))
      : [],
  }))
}

/**
 * Build an exact create payload from non-sensitive revoked credential data.
 * Invalid or inaccessible scopes are intentionally retained for live backend
 * validation; reconnect must never silently narrow the original grant.
 */
export function credentialToReconnectPayload(
  credential: ReconnectableCredential,
  now = Date.now(),
): MCPKeyPayload {
  const payload: MCPKeyPayload = {
    name: credential.name,
    client_type: credential.client_type,
    capabilities: Array.isArray(credential.capabilities) ? [...credential.capabilities] : [],
    tenant_scopes: cloneTenantScopes(credential.tenant_scopes),
  }

  const hasExpiryField = Object.prototype.hasOwnProperty.call(credential, 'expires_at')
  if (!hasExpiryField && credential.expires_at === undefined) {
    payload.never_expires = true
    return payload
  }

  if (typeof credential.expires_at === 'string') {
    const expiry = new Date(credential.expires_at).getTime()
    if (Number.isFinite(expiry) && expiry > now) {
      payload.expires_at_unix = Math.floor(expiry / 1000)
    }
  }

  return payload
}

export async function createReconnectAndReveal(
  payload: MCPKeyPayload,
  create: (value: MCPKeyPayload) => Promise<MCPKeyResponse<MCPSecretResult>>,
  reveal: (value: MCPSecretResult) => void,
  reload: () => Promise<unknown>,
): Promise<void> {
  const response = await create(payload)
  if (!response.data?.token) throw new Error('服务端未返回一次性 Secret')

  // The plaintext must reach component-local state before any fallible reload.
  reveal(response.data)
  try {
    await reload()
  } catch {
    // A list refresh failure must never hide or discard the one-time Secret.
  }
}
