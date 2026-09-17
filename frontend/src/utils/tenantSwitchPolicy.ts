export const TENANT_SWITCH_FALLBACK = '/platform/knowledge-bases'

/** Active workspace switches always land on the target workspace's knowledge base list. */
export function tenantSwitchTargetPath(_currentPath: string): string {
  return TENANT_SWITCH_FALLBACK
}
