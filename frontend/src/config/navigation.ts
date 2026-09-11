import type { DeploymentCapabilityKey } from './deploymentCapabilities'

export type NavigationGroup = 'primary' | 'knowledge' | 'ai' | 'workspace' | 'system'
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
  visibility?: 'all' | 'system-admin'
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
  { id: 'portal', labelKey: 'navigation.portalHome', icon: 'home', route: '/portal', group: 'primary', order: 10 },
  { id: 'knowledge-bases', labelKey: 'navigation.knowledgeBases', icon: 'folder', route: '/platform/knowledge-bases', group: 'knowledge', order: 20 },
  { id: 'agents', labelKey: 'navigation.agents', icon: 'robot', route: '/platform/agents', group: 'ai', requiredCapabilities: ['agents'], order: 40 },
  { id: 'members', labelKey: 'navigation.members', icon: 'usergroup', route: '/platform/settings?section=members', settingsSection: 'members', group: 'workspace', minimumRole: 'admin', order: 50 },
  { id: 'integrations', labelKey: 'navigation.integrations', icon: 'connection', route: '/platform/settings?section=integration-im', settingsSection: 'integration-im', group: 'workspace', minimumRole: 'admin', requiredAnyCapabilities: ['integrations.im', 'integrations.embed', 'integrations.api', 'settings.mcp'], order: 60 },
  { id: 'workspace-settings', labelKey: 'navigation.workspaceSettings', icon: 'setting', route: '/platform/settings?section=tenant', settingsSection: 'tenant', group: 'workspace', minimumRole: 'admin', order: 70 },
  { id: 'system-admin', labelKey: 'settings.navGroups.systemAdministration', icon: 'dashboard', route: '/platform/settings?section=system-admin', settingsSection: 'system-admin', group: 'system', visibility: 'system-admin', order: 75 },
  { id: 'usage-analytics', labelKey: 'navigation.usageAnalytics', icon: 'chart-bar', route: '/platform/settings?section=usage-analytics', settingsSection: 'usage-analytics', group: 'system', visibility: 'system-admin', order: 80 },
  { id: 'models-defaults', labelKey: 'navigation.modelsDefaults', icon: 'control-platform', route: '/platform/settings?section=models', settingsSection: 'models', group: 'system', visibility: 'system-admin', order: 90 },
  { id: 'runtime', labelKey: 'navigation.runtime', icon: 'server', route: '/platform/settings?section=runtime-queues', settingsSection: 'runtime-queues', group: 'system', visibility: 'system-admin', order: 100 },
  { id: 'audit', labelKey: 'navigation.audit', icon: 'root-list', route: '/platform/settings?section=system-audit-log', settingsSection: 'system-audit-log', group: 'system', visibility: 'system-admin', order: 110 },
  { id: 'system-settings', labelKey: 'navigation.systemSettings', icon: 'setting', route: '/platform/settings?section=system-global', settingsSection: 'system-global', group: 'system', visibility: 'system-admin', order: 120 },
  { id: 'platform-api-keys', labelKey: 'navigation.platformApiKeys', icon: 'secured', route: '/platform/settings?section=platform-api-keys', settingsSection: 'platform-api-keys', group: 'system', visibility: 'system-admin', order: 130 },
] as const

const ROLE_LEVEL: Record<string, number> = { viewer: 10, contributor: 20, admin: 30, owner: 40 }

export function visibleNavigationEntries(context: NavigationContext): NavigationEntry[] {
  return NAVIGATION_REGISTRY.filter((entry) => {
    if (context.isLiteMode && (entry.id === 'portal' || entry.group === 'workspace' || entry.group === 'system')) return false
    if (entry.visibility === 'system-admin' && !context.isSystemAdmin) return false
    if (entry.minimumRole && (ROLE_LEVEL[context.role] || 0) < ROLE_LEVEL[entry.minimumRole]) return false
    if (!(entry.requiredCapabilities || []).every(context.supports)) return false
    return !entry.requiredAnyCapabilities || entry.requiredAnyCapabilities.some(context.supports)
  }).sort((a, b) => a.order - b.order)
}

export function isNavigationEntryActive(entry: NavigationEntry, path: string, section?: unknown): boolean {
  if (entry.settingsSection) return path === '/platform/settings' && section === entry.settingsSection
  if (entry.id === 'knowledge-bases') return path.startsWith('/platform/knowledge-bases')
  return path === entry.route
}
