import type { PortalSpace, PortalStage } from '@/api/portal'

export const PORTAL_ALL_STAGE = 'all'

const STAGE_FALLBACK: Record<string, { name: string; description: string }> = {
  insight: { name: 'Insight', description: 'Market insight' },
  concept_market: { name: 'Concept 1', description: 'Opportunity identification' },
  concept_product: { name: 'Concept 2', description: 'Requirements definition' },
  architecture: { name: 'Architecture', description: 'Technical architecture' },
  design: { name: 'Design', description: 'Solution design' },
  development: { name: 'Development', description: 'R&D implementation' },
  testing: { name: 'Testing', description: 'Quality validation' },
  lmt: { name: 'LMT', description: 'Lifecycle' },
}

export function displayPortalStage(stage: PortalStage, translate?: (key: string) => string) {
  const fallback = STAGE_FALLBACK[stage.key]
  const localizedName=translate?.(`portal.stages.${stage.key}.name`)
  const localizedDescription=translate?.(`portal.stages.${stage.key}.description`)
  return {
    ...stage,
    name: stage.name?.trim() || (localizedName && localizedName !== `portal.stages.${stage.key}.name` ? localizedName : '') || fallback?.name || stage.key,
    description: stage.description?.trim() || (localizedDescription && localizedDescription !== `portal.stages.${stage.key}.description` ? localizedDescription : '') || fallback?.description || '',
  }
}

export function uniquePortalSpaces(spaces: PortalSpace[]): PortalSpace[] {
  const seen = new Set<number>()
  return spaces.filter((space) => {
    if (seen.has(space.tenant_id)) return false
    seen.add(space.tenant_id)
    return true
  })
}

export function portalCategories(spaces: PortalSpace[]): string[] {
  return [...new Set(spaces.map((space) => space.category.trim()).filter(Boolean))].sort((a, b) => a.localeCompare(b))
}

export function defaultPortalStageKey(stages: PortalStage[], spaces: PortalSpace[], activeTenantId: number): string {
  const currentStage = stages.find((stage) => spaces.some((space) =>
    space.tenant_id === activeTenantId && space.stages.includes(stage.key),
  ))
  if (currentStage) return currentStage.key
  return stages.find((stage) => spaces.some((space) => space.stages.includes(stage.key)))?.key || stages[0]?.key || ''
}

export function validAccessReason(reason: string): boolean {
  const length = [...reason.trim()].length
  return length > 0 && length <= 1000
}

export function canReviewPortalRequests(role: string | null | undefined): boolean {
  return role === 'owner'
}
