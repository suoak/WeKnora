import assert from 'node:assert/strict'
import test from 'node:test'
import type { PortalSpace } from '@/api/portal'
import {
  PUBLIC_KNOWLEDGE_PREVIEW_LIMIT,
  publicKnowledgeSpaces,
  visiblePublicKnowledgeSpaces,
} from './publicKnowledgeSpaces'

const space = (tenantId: number, name: string, category = 'public_knowledge') => ({
  tenant_id: tenantId,
  display_name: name,
  category,
}) as PortalSpace

test('public knowledge membership uses Portal category rather than display name', () => {
  const spaces = [
    space(1, '项目库空间'),
    space(2, '未来新增的 AI 知识空间'),
    space(3, '名称包含公共但属于 IPD', 'development_assets'),
  ]

  assert.deepEqual(publicKnowledgeSpaces(spaces).map((item) => item.tenant_id), [1, 2])
})

test('public knowledge preview preserves configured API order and expands beyond six', () => {
  const spaces = Array.from({ length: 8 }, (_, index) => space(index + 1, `Space ${index + 1}`))

  assert.equal(visiblePublicKnowledgeSpaces(spaces, false).length, PUBLIC_KNOWLEDGE_PREVIEW_LIMIT)
  assert.deepEqual(visiblePublicKnowledgeSpaces(spaces, false).map((item) => item.tenant_id), [1, 2, 3, 4, 5, 6])
  assert.equal(visiblePublicKnowledgeSpaces(spaces, true).length, 8)
})
