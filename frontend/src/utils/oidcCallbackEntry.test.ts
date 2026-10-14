import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { oidcCallbackEntry } from './oidcCallbackEntry'

test('OIDC errors and completions enter Login before root Portal navigation', () => {
  for (const hash of ['#oidc_error=login_failed', '#oidc_error=identity_contract_pending', '#oidc_complete=1']) {
    assert.equal(oidcCallbackEntry('/', hash), `/login${hash}`)
  }
  assert.equal(oidcCallbackEntry('/', ''), null)
  assert.equal(oidcCallbackEntry('/', '#unrelated=1'), null)
  assert.equal(oidcCallbackEntry('/login', '#oidc_error=invalid_state'), null)
  assert.equal(oidcCallbackEntry('/portal', '#oidc_error=login_failed'), null)
})

test('callback entry navigation precedes router installation and first paint', () => {
  const main = readFileSync(new URL('../main.ts', import.meta.url), 'utf8')
  assert.ok(main.indexOf('await router.replace(callbackEntry)') < main.indexOf('app.use(router)'))
  assert.ok(main.indexOf('app.use(pinia)') < main.indexOf('await router.replace(callbackEntry)'))
  assert.ok(main.indexOf('await router.isReady()') < main.indexOf('app.mount('))
})
