import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { resolveInviteRegistrationState } from './inviteRegistrationState'

const loginSource = readFileSync(new URL('./Login.vue', import.meta.url), 'utf8')

test('valid invitation opens registration without enabling public signup in invite-only mode', () => {
  assert.deepEqual(resolveInviteRegistrationState('invite_only'), {
    registrationEnabled: false,
    isRegisterMode: true,
  })
})

test('valid invitation keeps self-service registration enabled when configured', () => {
  assert.deepEqual(resolveInviteRegistrationState('self_serve'), {
    registrationEnabled: true,
    isRegisterMode: true,
  })
})

test('login page applies invitation state and keeps a return path to registration', () => {
  assert.match(loginSource, /resolveInviteRegistrationState\(cfg\.registration_mode\)/)
  assert.match(loginSource, /isRegisterMode\.value = inviteState\.isRegisterMode/)
  assert.match(loginSource, /v-if="registrationEnabled \|\| inviteLookup"/)
})
