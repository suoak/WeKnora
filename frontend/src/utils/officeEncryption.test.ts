import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { File } from 'node:buffer'
import { inspectOffice, preflightOfficeFiles, assertOfficeUploadable, officeDRMMessage } from './officeEncryption.ts'
const root = new URL('../../../internal/officeguard/testdata/', import.meta.url)
const cases: { name: string; frontend: string }[] = JSON.parse(readFileSync(new URL('manifest.json', root), 'utf8'))
const fixture = (name: string) => new File([readFileSync(new URL(name, root))], name) as unknown as globalThis.File
for (const tc of cases) test(tc.name, async () => assert.equal(await inspectOffice(fixture(tc.name)), tc.frontend))
const vendor: { fixtures: typeof cases } = JSON.parse(readFileSync(new URL('vendor/provenance.json', root), 'utf8'))
for (const tc of vendor.fixtures) test(tc.name, async () => assert.equal(await inspectOffice(fixture(tc.name)), tc.frontend))
test('batch skips only encrypted, preserves order and usable protection', async () => {
  const files = ['normal.docx', 'encrypted.xlsx', 'sheet-protected.xlsx', 'broken.docx'].map(fixture)
  const result = await preflightOfficeFiles(files)
  assert.deepEqual(result.accepted.map(f => f.name), ['normal.docx', 'sheet-protected.xlsx', 'broken.docx'])
  assert.deepEqual(result.rejected.map(f => f.name), ['encrypted.xlsx'])
})
test('API boundary rejects with stable business code before network admission', async () => {
  let calls = 0
  const upload = async (file: globalThis.File) => { await assertOfficeUploadable(file); calls++ }
  await assert.rejects(upload(fixture('encrypted.docx')), { code: 2400 })
  await upload(fixture('normal.docx'))
  assert.equal(calls, 1)
})
test('stalled local read times out', async () => {
  const blob = { name: 'a.docx', size: 512, slice: () => ({ arrayBuffer: () => new Promise(() => {}) }) } as unknown as globalThis.File
  assert.equal(await inspectOffice(blob), 'unknown')
})
test('DRM uses a distinct explanation, rejects before HTTP, and skips only restricted siblings', async () => {
  await assert.rejects(assertOfficeUploadable(fixture('drm.xlsx')), { code: 2400, message: officeDRMMessage })
  const result = await preflightOfficeFiles(['normal.docx', 'drm.xlsx', 'encrypted.xlsx', 'sheet-protected.xlsx'].map(fixture))
  assert.deepEqual(result.accepted.map(f => f.name), ['normal.docx', 'sheet-protected.xlsx'])
  assert.deepEqual(result.rejected.map(f => f.name), ['drm.xlsx', 'encrypted.xlsx'])
  assert.match(result.rejectionMessage, /DRM/)
  assert.match(result.rejectionMessage, /打开密码/)
})
