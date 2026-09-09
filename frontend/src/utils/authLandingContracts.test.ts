import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = (relative: string) => readFileSync(new URL(relative, import.meta.url), 'utf8')

test('root, login, password login, and OIDC share the Portal landing policy', () => {
  const router = source('../router/index.ts')
  const login = source('../views/auth/Login.vue')
  const app = source('../App.vue')

  assert.match(router, /path:\s*["']\/["'][\s\S]*?resolveRootLanding/)
  assert.match(router, /to\.path === ['"]\/login['"][\s\S]*?resolvePostAuthLanding/)
  assert.match(login, /router\.replace\(resolveLoginLanding\(\)\)/)
  assert.match(app, /persistOIDCLoginResponse[\s\S]*?resolvePostAuthLanding/)
})

test('protected routes are captured before login and 401 redirects preserve the current URL', () => {
  const router = source('../router/index.ts')
  const authRefresh = source('./authRefresh.ts')
  assert.match(router, /rememberAuthReturnTarget\(to\.fullPath\)[\s\S]*?next\(['"]\/login['"]\)/)
  assert.match(authRefresh, /rememberAuthReturnTarget\(`\$\{window\.location\.pathname\}\$\{window\.location\.search\}\$\{window\.location\.hash\}`\)/)
})

test('workspace invites keep their explicit workspace landing for password, registration, and OIDC', () => {
  const login = source('../views/auth/Login.vue')
  const app = source('../App.vue')
  assert.match(login, /acceptAndEnter[\s\S]*?router\.replace\(['"]\/platform\/knowledge-bases['"]\)/)
  assert.match(login, /registerByInvite[\s\S]*?persistLoginResponse\(response, true\)[\s\S]*?router\.replace\(['"]\/platform\/knowledge-bases['"]\)/)
  assert.match(app, /pendingInviteToken[\s\S]*?router\.replace\(['"]\/platform\/knowledge-bases['"]\)/)
})

test('Portal remains tenantless-capable and admin is never the default landing', () => {
  const router = source('../router/index.ts')
  assert.match(router, /path:\s*["']\/portal["'][\s\S]*?requiresTenant:\s*false/)
  assert.match(router, /path:\s*["']\/portal\/admin["'][\s\S]*?requiresSystemAdmin:\s*true/)
  assert.doesNotMatch(source('./authRedirect.ts'), /\/portal\/admin/)
})

test('tenant switch still enters the selected workspace KB list', () => {
  const tenantSwitch = source('./tenantSwitch.ts')
  assert.match(tenantSwitch, /SAFE_FALLBACK_PATH\s*=\s*['"]\/platform\/knowledge-bases['"]/)
  assert.match(tenantSwitch, /window\.location\.href\s*=\s*tenantSwitchTargetPath/)
})
