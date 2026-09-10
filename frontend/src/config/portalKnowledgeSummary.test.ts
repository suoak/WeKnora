import assert from 'node:assert/strict'
import test from 'node:test'
import type { PortalSpace } from '@/api/portal'
import { buildKnowledgeHierarchySummary } from './portalKnowledgeSummary'

const space = (
  tenantId: number,
  stages: string[],
  category: string,
  knowledgeBases: number,
  files: number,
) => ({
  tenant_id: tenantId,
  stages,
  category,
  knowledge_base_count: knowledgeBases,
  file_count: files,
}) as PortalSpace

test('overall totals distinct an overlapping IPD and public space', () => {
  const summary = buildKnowledgeHierarchySummary([
    space(1, ['architecture'], 'public_knowledge', 3, 100),
    space(2, ['design'], 'development_assets', 2, 20),
    space(3, [], 'public_knowledge', 4, 40),
  ], ['architecture', 'design'])

  assert.deepEqual(summary.ipd, { spaces: 2, knowledgeBases: 5, files: 120 })
  assert.deepEqual(summary.publicArea, { spaces: 2, knowledgeBases: 7, files: 140 })
  assert.deepEqual(summary.overall, { spaces: 3, knowledgeBases: 9, files: 160 })
})

test('phase totals include a multi-phase space in each phase but once in IPD', () => {
  const summary = buildKnowledgeHierarchySummary([
    space(1, ['architecture', 'design'], 'development_assets', 3, 100),
    space(2, ['architecture'], 'development_assets', 2, 20),
  ], ['architecture', 'design'])

  assert.deepEqual(summary.phases.architecture, { spaces: 2, knowledgeBases: 5, files: 120 })
  assert.deepEqual(summary.phases.design, { spaces: 1, knowledgeBases: 3, files: 100 })
  assert.deepEqual(summary.ipd, { spaces: 2, knowledgeBases: 5, files: 120 })
})

test('new public spaces are included dynamically and duplicate tenant rows are distinct', () => {
  const summary = buildKnowledgeHierarchySummary([
    space(10, [], 'public_knowledge', 6, 60),
    space(10, [], 'public_knowledge', 6, 60),
    space(11, [], 'public_knowledge', 1, 10),
  ], ['architecture'])

  assert.deepEqual(summary.publicArea, { spaces: 2, knowledgeBases: 7, files: 70 })
  assert.deepEqual(summary.overall, { spaces: 2, knowledgeBases: 7, files: 70 })
})
