import type { PortalSpace } from '@/api/portal'

export const PUBLIC_KNOWLEDGE_CATEGORY = 'public_knowledge'
export const PUBLIC_KNOWLEDGE_PREVIEW_LIMIT = 6

/** Public knowledge is a Portal grouping, never a workspace-name heuristic. */
export function publicKnowledgeSpaces(spaces: readonly PortalSpace[]): PortalSpace[] {
  return spaces.filter((space) => space.category === PUBLIC_KNOWLEDGE_CATEGORY)
}

/** Keep the API's featured/display_order sequence intact. */
export function visiblePublicKnowledgeSpaces(spaces: readonly PortalSpace[], expanded: boolean): PortalSpace[] {
  const publicSpaces = publicKnowledgeSpaces(spaces)
  return expanded ? publicSpaces : publicSpaces.slice(0, PUBLIC_KNOWLEDGE_PREVIEW_LIMIT)
}
