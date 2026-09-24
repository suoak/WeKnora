import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { VALIDATE_CREDENTIALS_TIMEOUT_MS } from './requestPolicy'

test('credential validation has a dedicated 60 second browser timeout', () => {
  assert.equal(VALIDATE_CREDENTIALS_TIMEOUT_MS, 60_000)

  const datasourceApi = readFileSync(new URL('./index.ts', import.meta.url), 'utf8')
  assert.match(
    datasourceApi,
    /validateCredentials[\s\S]*?timeout:\s*VALIDATE_CREDENTIALS_TIMEOUT_MS/,
  )
})
