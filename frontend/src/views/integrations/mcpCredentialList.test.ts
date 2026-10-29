import test from 'node:test'
import assert from 'node:assert/strict'
import type { MCPAccessKey } from '../../api/mcpAccessKeys'
import { credentialListState, clampCredentialPage } from './mcpCredentialList.ts'

const key = (status = 'active', expires_at?: string) => ({ status, expires_at } as MCPAccessKey)
test('small lists hide controls and summarize the actual lifecycle state', () => {
  for (const count of [0, 1, 2, 5]) {
    const state = credentialListState(Array.from({ length: count }, () => key()))
    assert.equal(state.hasMany, false)
    assert.equal(state.multipleStates, false)
    assert.equal(state.summary, `共 ${count} 个 Key${count ? '，均已启用' : ''}`)
    assert.ok(state.filters.every(filter => filter.count > 0))
  }
})
test('many keys only need state tabs when more than one real state is present', () => {
  const uniform = credentialListState(Array.from({ length: 6 }, () => key()))
  assert.equal(uniform.hasMany, true)
  assert.equal(uniform.multipleStates, false)
  const rows = [key(), key('active', '2020-01-01T00:00:00Z'), key('revoked'), key(), key(), key()]
  const state = credentialListState(rows, Date.parse('2026-10-29T00:00:00Z'))
  assert.equal(state.multipleStates, true)
  assert.deepEqual(state.filters.map(filter => [filter.value, filter.count]), [['all', 6], ['active', 4], ['expired', 1], ['revoked', 1]])
  assert.equal(state.summary, '共 6 个 Key，4 个已启用')
  assert.deepEqual(credentialListState([key('revoked')]).filters.map(filter => filter.value), ['all', 'revoked'])
  assert.equal(credentialListState([key('expired')]).summary, '共 1 个 Key，均已过期')
})
test('page correction handles empty, single page and results shrinking after filtering', () => {
  for (const count of [0, 1, 2, 5]) assert.equal(clampCredentialPage(9, count), 1)
  assert.equal(clampCredentialPage(9, 6), 2)
  assert.equal(clampCredentialPage(4, 19), 4)
  assert.equal(clampCredentialPage(-1, 19), 1)
})
