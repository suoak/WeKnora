import assert from 'node:assert/strict'
import test from 'node:test'
import type { PortalSpace } from '@/api/portal'
import { buildKnowledgeHierarchySummary } from './portalKnowledgeSummary'

const space = (
  tenantId: number,
  stages: string[],
  category: string,
  knowledgeBases: number,
  documents: number,
  accessState: 'accessible' | 'discoverable' = 'accessible',
) => ({
  tenant_id: tenantId,
  stages,
  category,
  access_state: accessState,
  knowledge_base_count: knowledgeBases,
  file_count: documents,
  document_entry_count: documents,
}) as PortalSpace

test('overall totals distinct spaces while public excludes every valid lifecycle mapping', () => {
  const summary = buildKnowledgeHierarchySummary([
    space(1, ['architecture'], 'public_knowledge', 3, 100),
    space(2, ['design'], 'development_assets', 2, 20),
    space(3, [], 'public_knowledge', 4, 40),
  ], ['architecture', 'design'])

  assert.deepEqual(summary.ipd, { spaces: 2, knowledgeBases: 5, documents: 120 })
  assert.deepEqual(summary.publicArea, { spaces: 1, knowledgeBases: 4, documents: 40 })
  assert.deepEqual(summary.overall, { spaces: 3, knowledgeBases: 9, documents: 160 })
})

test('null, non-finite, and negative asset counts are normalized to zero', () => {
  const rows = [
    space(20, [], 'uncategorized', null as unknown as number, Number.NaN),
    space(21, [], 'uncategorized', -2, -9, 'discoverable'),
  ]
  const summary = buildKnowledgeHierarchySummary(rows, ['insight'])

  assert.deepEqual(summary.overall, { spaces: 2, knowledgeBases: 0, documents: 0 })
  assert.deepEqual(summary.publicArea, { spaces: 2, knowledgeBases: 0, documents: 0 })
})

test('overall and stage totals include accessible and discoverable asset counts', () => {
  const summary = buildKnowledgeHierarchySummary([
    space(1, ['development'], 'development_assets', 5, 680),
    space(2, ['development'], 'development_assets', 4, 302, 'discoverable'),
  ], ['development'])

  assert.deepEqual(summary.overall, { spaces: 2, knowledgeBases: 9, documents: 982 })
  assert.deepEqual(summary.ipd, { spaces: 2, knowledgeBases: 9, documents: 982 })
  assert.deepEqual(summary.phases.development, { spaces: 2, knowledgeBases: 9, documents: 982 })
})

test('overall totals remain available when stage metadata has not loaded', () => {
  const summary = buildKnowledgeHierarchySummary([
    space(1, ['development'], 'development_assets', 5, 680),
    space(2, [], 'uncategorized', 2, 20),
  ], [])

  assert.deepEqual(summary.overall, { spaces: 2, knowledgeBases: 7, documents: 700 })
  assert.deepEqual(summary.ipd, { spaces: 0, knowledgeBases: 0, documents: 0 })
})

test('phase totals include a multi-phase space in each phase but once in IPD', () => {
  const summary = buildKnowledgeHierarchySummary([
    space(1, ['architecture', 'design'], 'development_assets', 3, 100),
    space(2, ['architecture'], 'development_assets', 2, 20),
  ], ['architecture', 'design'])

  assert.deepEqual(summary.phases.architecture, { spaces: 2, knowledgeBases: 5, documents: 120 })
  assert.deepEqual(summary.phases.design, { spaces: 1, knowledgeBases: 3, documents: 100 })
  assert.deepEqual(summary.ipd, { spaces: 2, knowledgeBases: 5, documents: 120 })
})

test('new public spaces are included dynamically and duplicate tenant rows are distinct', () => {
  const summary = buildKnowledgeHierarchySummary([
    space(10, [], 'public_knowledge', 6, 60),
    space(10, [], 'public_knowledge', 6, 60),
    space(11, [], 'public_knowledge', 1, 10),
  ], ['architecture'])

  assert.deepEqual(summary.publicArea, { spaces: 2, knowledgeBases: 7, documents: 70 })
  assert.deepEqual(summary.overall, { spaces: 2, knowledgeBases: 7, documents: 70 })
})

test('omitted private totals remain unknown and never fall back to legacy files', () => {
  const restricted = space(20, [], 'private', 3, 999)
  delete restricted.document_entry_count
  const summary = buildKnowledgeHierarchySummary([restricted, space(21, [], 'public', 1, 7)], [])
  assert.equal(summary.overall.documents, null)
})
