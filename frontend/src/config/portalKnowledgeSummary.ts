import type { PortalSpace } from '@/api/portal'
import { PUBLIC_KNOWLEDGE_CATEGORY } from './publicKnowledgeSpaces'

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

function uniqueSpaces(spaces: readonly PortalSpace[]): PortalSpace[] {
  const unique = new Map<number, PortalSpace>()
  for (const space of spaces) {
    if (!unique.has(space.tenant_id)) unique.set(space.tenant_id, space)
  }
  return [...unique.values()]
}

export function summarizeKnowledgeSpaces(spaces: readonly PortalSpace[]): KnowledgeScale {
  return uniqueSpaces(spaces).reduce<KnowledgeScale>((summary, space) => ({
    spaces: summary.spaces + 1,
    knowledgeBases: summary.knowledgeBases + space.knowledge_base_count,
    files: summary.files + space.file_count,
  }), { spaces: 0, knowledgeBases: 0, files: 0 })
}

export function buildKnowledgeHierarchySummary(
  spaces: readonly PortalSpace[],
  stageKeys: readonly string[],
): KnowledgeHierarchySummary {
  const unique = uniqueSpaces(spaces)
  const stageSet = new Set(stageKeys)
  const ipdSpaces = unique.filter((space) => space.stages.some((stage) => stageSet.has(stage)))
  const publicSpaces = unique.filter((space) => space.category === PUBLIC_KNOWLEDGE_CATEGORY)
  const overallSpaces = uniqueSpaces([...ipdSpaces, ...publicSpaces])

  return {
    overall: summarizeKnowledgeSpaces(overallSpaces),
    ipd: summarizeKnowledgeSpaces(ipdSpaces),
    publicArea: summarizeKnowledgeSpaces(publicSpaces),
    phases: Object.fromEntries(stageKeys.map((stage) => [
      stage,
      summarizeKnowledgeSpaces(ipdSpaces.filter((space) => space.stages.includes(stage))),
    ])),
  }
}
