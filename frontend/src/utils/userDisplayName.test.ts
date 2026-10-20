import assert from 'node:assert/strict'
import test from 'node:test'
import { getUserDisplayName, getWorkspaceDisplayName } from './userDisplayName'

const generated = 'oa-12345678-1234-4234-a234-123456789abc'

for (const [name, user, expected] of [
  ['generated OA uses company email prefix', { username: generated, email: 'employee@company.test' }, 'employee'],
  ['email prefix retains dots and hyphens', { username: generated, email: 'staff.one-two@company.test' }, 'staff.one-two'],
  ['surrounding email whitespace', { username: generated, email: ' employee@company.test ' }, 'employee'],
  ['custom username remains', { username: 'custom name', email: 'employee@company.test' }, 'custom name'],
  ['similar custom prefix remains', { username: 'oa-team', email: 'employee@company.test' }, 'oa-team'],
  ['missing email keeps fallback', { username: generated }, generated],
  ['empty local part keeps fallback', { username: generated, email: '@company.test' }, generated],
  ['malformed email keeps fallback', { username: generated, email: 'one@two@company.test' }, generated],
  ['missing user is safe', undefined, ''],
] as const) {
  test(name, () => assert.equal(getUserDisplayName(user), expected))
}

test('own generated workspace label follows email prefix without mutating identity', () => {
  const user = { username: generated, email: 'employee@company.test' }
  assert.equal(getWorkspaceDisplayName(generated, user, true), 'employee')
  assert.equal(getWorkspaceDisplayName(`${generated}'s Workspace`, user, true), "employee's Workspace")
  assert.deepEqual(user, { username: generated, email: 'employee@company.test' })
})

test('other and custom workspace labels remain unchanged', () => {
  const user = { username: generated, email: 'employee@company.test' }
  assert.equal(getWorkspaceDisplayName(generated, user, false), generated)
  assert.equal(getWorkspaceDisplayName('Business workspace', user, true), 'Business workspace')
  assert.equal(getWorkspaceDisplayName('oa-00000000-1234-4234-a234-123456789abc', user, true), 'oa-00000000-1234-4234-a234-123456789abc')
})
