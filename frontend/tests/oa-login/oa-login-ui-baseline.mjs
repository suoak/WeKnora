// Compare real Vue renders against the currently deployed 431226c3b UI.
// Source is held in memory while the local-only baseline server runs.
import { readFile, writeFile } from 'node:fs/promises'
import { execFileSync, spawnSync } from 'node:child_process'

const baseline = '431226c3b1bd5338ec06c1bd0520e637d5fed398'
const paths = ['src/views/auth/Login.vue', ...['zh-CN', 'en-US', 'ja-JP', 'ko-KR', 'ru-RU'].map(locale => `src/i18n/locales/${locale}.ts`)]
const current = new Map()
const build = commit => {
  const result = spawnSync(process.execPath, ['--max-old-space-size=4096', 'node_modules/vite/bin/vite.js', 'build'], {
    stdio: 'inherit', env: { ...process.env, GOMAXPROCS: '2', RAYON_NUM_THREADS: '1', VITE_IS_DOCKER: 'true', VITE_FRONTEND_COMMIT: commit },
  })
  if (result.status !== 0) throw new Error('UI production build failed')
}
const run = phase => {
  const result = spawnSync(process.execPath, ['tests/oa-login/oa-login-ui.mjs', phase], { stdio: 'inherit', env: process.env })
  if (result.status !== 0) throw new Error(`UI ${phase} checks failed`)
}
try {
  for (const path of paths) {
    current.set(path, await readFile(path))
    await writeFile(path, execFileSync('git', ['show', `${baseline}:frontend/${path}`]))
  }
  build(baseline)
  run('before')
} finally {
  for (const [path, contents] of current) await writeFile(path, contents)
}
build(execFileSync('git', ['rev-parse', 'HEAD'], { encoding: 'utf8' }).trim())
run('after')
