import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const read = name => readFileSync(new URL(name, import.meta.url), 'utf8')
test('QA builds require opt-in, fixture authentication and loopback; product input stays separate', () => {
  const config = read('./frozen-build.config.ts'), entry = read('./qa-component.ts')
  assert.match(config, /RC24_QA_BUILD !== '1'/)
  assert.match(entry, /import\.meta\.env\.MODE != 'rc24-qa'/)
  assert.match(entry, /location\.hostname != '127\.0\.0\.1'/)
  assert.match(entry, /localStorage\.getItem\('weknora_token'\) != 'local-fixture-token'/)
  assert.doesNotMatch(entry, /localStorage\.setItem/)
  assert.doesNotMatch(read('../../vite.config.ts'), /qa-component|component\.html|tests\/agent-editor/)
})
test('image context excludes all QA and evidence; default Gate uses immutable static builds and normal animations', () => {
  const ignored = read('../../.dockerignore').split(/\r?\n/)
  for (const name of ['tests', 'test-results', 'playwright-report', '**/*.test.*', '**/*.spec.*']) assert.ok(ignored.includes(name), name)
  assert.equal(JSON.parse(read('../../package.json')).scripts['test:agent-editor'], 'node tests/agent-editor/run-gate.mjs')
  const config = read('./final-gate.config.ts')
  assert.match(config, /workers:1/)
  assert.match(config, /reducedMotion:'no-preference'/)
  assert.doesNotMatch(config, /webServer\s*:/)
  assert.match(read('./stable-server.mjs'), /API must be intercepted by the test fixture/)
})
