import type { DeploymentCapabilityKey } from './deploymentCapabilities'

export type NavigationGroup = 'global' | 'workspace' | 'management'
export type NavigationRole = 'viewer' | 'contributor' | 'admin' | 'owner'

export interface NavigationEntry {
  id: string
  labelKey: string
  icon: string
  route: string
  group: NavigationGroup
  requiredCapabilities?: DeploymentCapabilityKey[]
  requiredAnyCapabilities?: DeploymentCapabilityKey[]
  minimumRole?: NavigationRole
  visibility?: 'all' | 'management'
  order: number
  settingsSection?: string
}

export interface NavigationContext {
  role: string
  isSystemAdmin: boolean
  isLiteMode?: boolean
  supports: (capability: DeploymentCapabilityKey) => boolean
}

export const NAVIGATION_REGISTRY: readonly NavigationEntry[] = [
  { id: 'portal', labelKey: 'navigation.portalHome', icon: 'home', route: '/portal', group: 'global', order: 10 },
  { id: 'knowledge-bases', labelKey: 'navigation.knowledgeBases', icon: 'knowledge-base', route: '/platform/knowledge-bases', group: 'workspace', order: 20 },
  { id: 'agents', labelKey: 'navigation.agents', icon: 'robot', route: '/platform/agents', group: 'workspace', requiredCapabilities: ['agents'], order: 30 },
  { id: 'members', labelKey: 'navigation.members', icon: 'usergroup', route: '/platform/settings?section=members', settingsSection: 'members', group: 'workspace', order: 40 },
  { id: 'management-center', labelKey: 'navigation.managementCenter', icon: 'setting', route: '/platform/settings?section=tenant', group: 'management', visibility: 'management', order: 50 },
] as const

const ROLE_LEVEL: Record<string, number> = { viewer: 10, contributor: 20, admin: 30, owner: 40 }

export function visibleNavigationEntries(context: NavigationContext): NavigationEntry[] {
  return NAVIGATION_REGISTRY.filter((entry) => {
    if (context.isLiteMode && (entry.id === 'portal' || entry.group === 'workspace' || entry.group === 'management')) return false
    if (entry.visibility === 'management' && !context.isSystemAdmin && (ROLE_LEVEL[context.role] || 0) < ROLE_LEVEL.admin) return false
    if (entry.minimumRole && (ROLE_LEVEL[context.role] || 0) < ROLE_LEVEL[entry.minimumRole]) return false
    if (!(entry.requiredCapabilities || []).every(context.supports)) return false
    return !entry.requiredAnyCapabilities || entry.requiredAnyCapabilities.some(context.supports)
  }).sort((a, b) => a.order - b.order)
}

export function isNavigationEntryActive(entry: NavigationEntry, path: string, section?: unknown): boolean {
  if (entry.id === 'management-center') return path === '/platform/settings'
  if (entry.settingsSection) return path === '/platform/settings' && section === entry.settingsSection
  if (entry.id === 'knowledge-bases') return path.startsWith('/platform/knowledge-bases')
  return path === entry.route
}
