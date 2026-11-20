import assert from 'node:assert/strict'
import test from 'node:test'
import { isAssetMutation, invalidatePortalAssets, resetPortalSession, portalAssetRevision, portalSessionRevision } from './portalEvents'

test('successful asset transport events cover create/upload/delete and KB/data-source mutations', () => {
  for (const [method, path] of [
    ['post', '/api/v1/knowledge/file'], ['post', '/api/v1/knowledge/manual'],
    ['delete', '/api/v1/knowledge/a'], ['put', '/api/v1/knowledge-bases/a'],
    ['post', '/api/v1/data-sources/a/sync'],
  ]) assert.equal(isAssetMutation(method, path), true, path)
  for (const method of ['get', 'head', 'options']) assert.equal(isAssetMutation(method, '/api/v1/knowledge-bases'), false)
  assert.equal(isAssetMutation('post', '/api/v1/chat'), false)
})

test('session reset invalidates asset generations as well as session ownership', () => {
  const session = portalSessionRevision.value, assets = portalAssetRevision.value
  invalidatePortalAssets()
  assert.equal(portalAssetRevision.value, assets + 1)
  resetPortalSession()
  assert.equal(portalSessionRevision.value, session + 1)
  assert.equal(portalAssetRevision.value, assets + 2)
})
