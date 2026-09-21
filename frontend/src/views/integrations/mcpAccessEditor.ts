import type { KBScopeMode, MCPAccessKey, MCPCapability, MCPClientType, MCPKBRef } from '@/api/mcpAccessKeys'

export interface MCPAccessDraftScope {
  mode: KBScopeMode
  selected: string[]
}

type NullableTenantScope = Omit<MCPAccessKey['tenant_scopes'][number], 'knowledge_bases'> & {
  knowledge_bases?: MCPKBRef[] | null
}

export type EditableMCPAccessKey = Omit<MCPAccessKey, 'capabilities' | 'tenant_scopes'> & {
  capabilities?: MCPCapability[] | null
  tenant_scopes?: NullableTenantScope[] | null
}

export interface MCPAccessEditDraft {
  id: number
  name: string
  clientType: MCPClientType
  capabilities: MCPCapability[]
  selectedTenantIds: number[]
  scopes: Record<number, MCPAccessDraftScope>
  expiryMode: 'custom' | 'never'
  customExpiry: string
}

const ownedRef = (id: string) => `owned:${id}`
const sharedRef = (id: string, shareID: string) => `shared:${id}:${shareID}`

function toDatetimeLocal(value?: string) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 16)
}

/** Normalize the real API response shape before populating the edit wizard. */
export function credentialToEditDraft(key: EditableMCPAccessKey): MCPAccessEditDraft {
  const tenantScopes = Array.isArray(key.tenant_scopes) ? key.tenant_scopes : []
  const customExpiry = toDatetimeLocal(key.expires_at)
  const scopes: Record<number, MCPAccessDraftScope> = {}

  for (const scope of tenantScopes) {
    const knowledgeBases = Array.isArray(scope.knowledge_bases) ? scope.knowledge_bases : []
    scopes[scope.tenant_id] = {
      mode: scope.kb_scope_mode,
      selected: knowledgeBases.map(kb => kb.source_type === 'shared'
        ? sharedRef(kb.knowledge_base_id, kb.kb_share_id || '')
        : ownedRef(kb.knowledge_base_id)),
    }
  }

  return {
    id: key.id,
    name: key.name || '',
    clientType: key.client_type || 'generic',
    capabilities: Array.isArray(key.capabilities) ? [...key.capabilities] : [],
    selectedTenantIds: tenantScopes.map(scope => scope.tenant_id),
    scopes,
    expiryMode: customExpiry ? 'custom' : 'never',
    customExpiry,
  }
}
