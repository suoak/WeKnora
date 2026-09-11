import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { resolve } from 'node:path'

const frontendRoot = resolve(import.meta.dirname, '../..')
const repositoryRoot = resolve(frontendRoot, '..')

test('the production deployment renders a 100 MiB file limit with HTTP envelope slack', () => {
  const compose = readFileSync(resolve(repositoryRoot, 'docker-compose.yml'), 'utf8')
  const entrypoint = readFileSync(resolve(frontendRoot, 'docker-entrypoint.sh'), 'utf8')
  const nginxTemplate = readFileSync(resolve(frontendRoot, 'nginx.conf'), 'utf8')

  assert.match(compose, /MAX_FILE_SIZE_MB=\$\{MAX_FILE_SIZE_MB:-100\}/)
  assert.match(entrypoint, /FILE_MB=\$\{MAX_FILE_SIZE_MB:-100\}/)
  assert.match(entrypoint, /\*\[!0-9\]\*\) FILE_MB=100/)
  assert.match(entrypoint, /HTTP_BODY_MB=\$\(expr "\$FILE_MB" \+ 1\)/)
  assert.match(entrypoint, /export MAX_FILE_SIZE=\$\{HTTP_BODY_MB\}M/)

  const configuredFileLimitMiB = 100
  const nginxBodyLimit = `${configuredFileLimitMiB + 1}M`
  const rendered = nginxTemplate.replaceAll('${MAX_FILE_SIZE}', nginxBodyLimit)

  assert.match(rendered, /client_max_body_size 101M;/)
})
