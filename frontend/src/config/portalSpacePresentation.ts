import type { PortalSpace } from '@/api/portal'

export type PortalResponsibleSpace = Pick<PortalSpace, 'tenant_id' | 'responsible_team'>

/**
 * Resolve the owner displayed for one Portal card from that card's own space
 * record. Deliberately do not fall back to contact, creator or active tenant
 * data: those fields have different product semantics.
 */
export function resolvePortalResponsibleTeam(
  space: PortalResponsibleSpace | undefined,
  unconfigured: string,
): string {
  return space?.responsible_team?.trim() || unconfigured
}
