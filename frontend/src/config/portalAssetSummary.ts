import type { KnowledgeScale } from './portalKnowledgeSummary'

type Translate = (key: string, values: Record<string, string | number>) => string

const count = (value: number | null, formatter: Intl.NumberFormat) => value === null ? '—' : formatter.format(value)

export function formatAssetSummary(
  scale: KnowledgeScale,
  translate: Translate,
  formatter = new Intl.NumberFormat(),
) {
  return translate('portalMap.stageInventory', {
    spaces: count(scale.spaces, formatter),
    kb: count(scale.knowledgeBases, formatter),
    documents: count(scale.documents, formatter),
  })
}

export function formatSpaceAssets(
  scale: Pick<KnowledgeScale, 'knowledgeBases' | 'documents'>,
  translate: Translate,
  formatter = new Intl.NumberFormat(),
) {
  return translate('portalMap.spaceInventoryCompact', {
    kb: count(scale.knowledgeBases, formatter),
    documents: count(scale.documents, formatter),
  })
}
