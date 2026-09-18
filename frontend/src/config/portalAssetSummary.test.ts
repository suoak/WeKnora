import assert from 'node:assert/strict'
import test from 'node:test'
import { formatAssetSummary, formatSpaceAssets } from './portalAssetSummary'

const translate = (key: string, values: Record<string, string | number>) => {
  if (key === 'portalMap.stageInventory') return `${values.spaces} 空间 · ${values.kb} 知识库 · ${values.files} 文件`
  return `${values.kb} 库 · ${values.files} 文件`
}

test('formats shared header assets without changing their values', () => {
  assert.equal(formatAssetSummary({ spaces: 15, knowledgeBases: 23, files: 275 }, translate), '15 空间 · 23 知识库 · 275 文件')
})

test('formats compact space assets separately from header summaries', () => {
  assert.equal(formatSpaceAssets({ knowledgeBases: 5, files: 76 }, translate), '5 库 · 76 文件')
  assert.equal(formatSpaceAssets({ knowledgeBases: 0, files: 0 }, translate), '0 库 · 0 文件')
})
