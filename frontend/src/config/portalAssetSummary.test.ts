import assert from 'node:assert/strict'
import test from 'node:test'
import { formatAssetSummary, formatSpaceAssets } from './portalAssetSummary'

const translate = (key: string, values: Record<string, string | number>) => {
  if (key === 'portalMap.stageInventory') return `${values.spaces} 空间 · ${values.kb} 个知识库 · ${values.documents} 篇文档`
  return `${values.kb} 个知识库 · ${values.documents} 篇文档`
}

test('formats shared header assets without changing their values', () => {
  assert.equal(formatAssetSummary({ spaces: 15, knowledgeBases: 23, documents: 275 }, translate), '15 空间 · 23 个知识库 · 275 篇文档')
})

test('formats compact space assets separately from header summaries', () => {
  assert.equal(formatSpaceAssets({ knowledgeBases: 5, documents: 76 }, translate), '5 个知识库 · 76 篇文档')
  assert.equal(formatSpaceAssets({ knowledgeBases: 0, documents: 0 }, translate), '0 个知识库 · 0 篇文档')
})
