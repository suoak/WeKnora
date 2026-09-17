import type { PortalSpace } from '@/api/portal'

export interface KnowledgeScale {
  spaces: number
  knowledgeBases: number
  files: number
}

export interface KnowledgeHierarchySummary {
  overall: KnowledgeScale
  ipd: KnowledgeScale
  publicArea: KnowledgeScale
  phases: Record<string, KnowledgeScale>
}

export function uniquePortalSpaces(spaces: readonly PortalSpace[]): PortalSpace[] {
  const unique = new Map<number, PortalSpace>()
  for (const space of spaces) {
    if (!unique.has(space.tenant_id)) unique.set(space.tenant_id, space)
  }
  return [...unique.values()]
}

function safeCount(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0 ? value : 0
}

export function summarizeKnowledgeSpaces(spaces: readonly PortalSpace[]): KnowledgeScale {
  return uniquePortalSpaces(spaces).reduce<KnowledgeScale>((summary, space) => ({
    spaces: summary.spaces + 1,
    knowledgeBases: summary.knowledgeBases + safeCount(space.knowledge_base_count),
    files: summary.files + safeCount(space.file_count),
  }), { spaces: 0, knowledgeBases: 0, files: 0 })
}

export function buildKnowledgeHierarchySummary(
  spaces: readonly PortalSpace[],
  stageKeys: readonly string[],
): KnowledgeHierarchySummary {
  const unique = uniquePortalSpaces(spaces)
  const stageSet = new Set(stageKeys)
  const hasLifecycleStage = (space: PortalSpace) => space.stages.some((stage) => stageSet.has(stage))
  const ipdSpaces = unique.filter(hasLifecycleStage)
  const publicSpaces = unique.filter((space) => !hasLifecycleStage(space))

  return {
    // Overall describes the unique published Portal spaces visible to this user.
    overall: summarizeKnowledgeSpaces(unique),
    ipd: summarizeKnowledgeSpaces(ipdSpaces),
    publicArea: summarizeKnowledgeSpaces(publicSpaces),
    phases: Object.fromEntries(stageKeys.map((stage) => [
      stage,
      summarizeKnowledgeSpaces(ipdSpaces.filter((space) => space.stages.includes(stage))),
    ])),
  }
}
