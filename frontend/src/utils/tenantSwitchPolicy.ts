export const TENANT_SWITCH_FALLBACK = '/portal'

/** Resource-free pages that are safe to reload under another tenant. */
export function tenantSwitchTargetPath(currentPath: string): string {
  if (currentPath === '/portal') return '/portal'
  if (currentPath === '/platform/knowledge-bases') return currentPath
  if (currentPath === '/platform/agents') return currentPath
  if (/^\/platform\/knowledge-bases\/[^/]+/.test(currentPath)) return '/platform/knowledge-bases'
  return TENANT_SWITCH_FALLBACK
}
