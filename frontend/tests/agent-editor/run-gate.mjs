// Serial, immutable-source Gate. All builds/evidence stay outside the checkout.
import { spawn, spawnSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { mkdirSync, readFileSync, writeFileSync, existsSync, openSync, closeSync, unlinkSync, copyFileSync } from 'node:fs'
import { resolve, dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { tmpdir } from 'node:os'
import net from 'node:net'
import { checkDist } from './check-dist.mjs'

const frontend = resolve(dirname(fileURLToPath(import.meta.url)), '../..')
const repository = resolve(frontend, '..')
const run = resolve(process.env.RC24_RUN_DIR || join(tmpdir(), 'knowhub-rc24-' + new Date().toISOString().replace(/[:.]/g, '-')))
if (run.startsWith(repository + '/') || run.startsWith(repository + '\\') || run === repository) throw new Error('Evidence/build directory must be outside the checkout')
mkdirSync(run, { recursive: true })
const lock = join(tmpdir(), 'knowhub-rc24-gate.lock')
const lockFd = openSync(lock, 'wx') // A second Gate must not overlap this Gate.
writeFileSync(lockFd, JSON.stringify({ pid: process.pid, run }))
const node = process.execPath
const env = { ...process.env, VITE_IS_DOCKER: 'true', RC24_QA_BUILD: '1', RC24_BUILD_DIR: join(run, 'qa-dist') }
if (process.platform === 'win32') {
  env.WEKNORA_TEST_BASH ||= 'E:/Git/bin/bash.exe'
  env.WEKNORA_TEST_POSIX_SHELL ||= 'E:/Git/usr/bin/sh.exe'
}
const output = (name, value) => writeFileSync(join(run, name + '.json'), JSON.stringify(value, null, 2))
const git = args => {
  const result = spawnSync('git', args, { cwd: repository, encoding: 'utf8' })
  if (result.status !== 0) throw new Error(result.stderr)
  return result.stdout.trim()
}
const listFiles = () => {
  const result = spawnSync('rg', ['--files', '--hidden'], { cwd: frontend, encoding: 'utf8' })
  if (result.status !== 0) throw new Error(result.stderr)
  return result.stdout.trim().split(/\r?\n/).filter(Boolean).sort()
}
const fingerprint = () => Object.fromEntries(listFiles().map(name => [name, createHash('sha256').update(readFileSync(resolve(frontend, name))).digest('hex')]))
const source = fingerprint(), stages = []
output('frozen-source', source)
for (const name of Object.keys(source)) {
  const target = resolve(run, 'frozen-source', name)
  mkdirSync(dirname(target), { recursive: true })
  copyFileSync(resolve(frontend, name), target)
}
const verify = stage => {
  const current = fingerprint()
  const changes = [...new Set([...Object.keys(source), ...Object.keys(current)])].filter(name => source[name] !== current[name])
  output(stage + '-source-check', { changes, files: Object.keys(source).length })
  if (changes.length) throw new Error('Source changed during Gate: ' + changes.join(', '))
}
const execute = async (name, args, extraEnv = {}) => {
  verify(name + '-start')
  console.log('Starting ' + name + ' (log: ' + join(run, name + '.log') + ')')
  const start = new Date().toISOString()
  const fd = openSync(join(run, name + '.log'), 'w')
  const child = spawn(node, args, { cwd: frontend, env: { ...env, ...extraEnv }, stdio: ['ignore', fd, fd] })
  const code = await new Promise((accept, reject) => { child.on('error', reject); child.on('exit', accept) })
  closeSync(fd)
  stages.push({ name, command: [node, ...args], start, end: new Date().toISOString(), code })
  output('stages', stages)
  verify(name + '-end')
  if (code !== 0) throw new Error(name + ' failed; see saved log')
}
const freePort = () => new Promise((accept, reject) => {
  const probe = net.createServer()
  probe.once('error', reject)
  probe.listen(4187, '127.0.0.1', () => probe.close(accept))
})
const serve = async (root, action) => {
  await freePort()
  const fd = openSync(join(run, root.endsWith('qa-dist') ? 'qa-server.log' : 'release-server.log'), 'w')
  const child = spawn(node, ['tests/agent-editor/stable-server.mjs', root], { cwd: frontend, stdio: ['ignore', fd, fd] })
  try {
    const deadline = Date.now() + 10000
    while (true) {
      try { const response = await fetch('http://127.0.0.1:4187/index.html'); if (response.ok) break } catch {}
      if (Date.now() > deadline) throw new Error('Static server did not become ready')
      await new Promise(accept => setTimeout(accept, 50)) // Server readiness only; no UI/animation sleeps.
    }
    const blocked = await fetch('http://127.0.0.1:4187/api/v1/agents')
    if (blocked.status !== 503) throw new Error('API isolation failed')
    await action()
  } finally {
    child.kill()
    await new Promise(accept => child.exitCode !== null ? accept() : child.once('exit', accept))
    closeSync(fd)
  }
}
try {
  const edge = 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'
  if (process.platform !== 'win32' || !existsSync(edge)) throw new Error('This Gate requires local Windows Edge')
  output('environment', { node: process.version, head: git(['rev-parse', 'HEAD']), branch: git(['branch', '--show-current']), edge, normalAnimations: true, workers: 1, dependencyLockSha256: source['package-lock.json'] })
  const unitFiles = Object.keys(source).filter(name => /\.test\.(?:ts|mjs|js)$/.test(name))
  output('unit-files', unitFiles)
  await execute('unit', ['--import', 'tsx', '--test', ...unitFiles])
  await execute('type-check', ['node_modules/vue-tsc/bin/vue-tsc.js', '--build'])
  await execute('qa-build', ['node_modules/vite/bin/vite.js', 'build', '--mode', 'rc24-qa', '--config', 'tests/agent-editor/frozen-build.config.ts'])
  await serve(join(run, 'qa-dist'), () => execute('edge-39', ['node_modules/@playwright/test/cli.js', 'test', '--config', 'tests/agent-editor/final-gate.config.ts', '--max-failures=1'], { RC24_EVIDENCE_DIR: join(run, 'edge-39') }))
  await execute('release-build', ['node_modules/vite/bin/vite.js', 'build', '--outDir', join(run, 'release-dist'), '--emptyOutDir', 'false'])
  await serve(join(run, 'release-dist'), () => execute('edge-25', ['node_modules/@playwright/test/cli.js', 'test', '--config', 'tests/agent-editor/final-gate.config.ts', 'close-cycle.spec.ts', 'navigation.spec.ts', 'editor.spec.ts', 'knowledge-gate.spec.ts', '--grep-invert', 'quick answer exposes retrieval', '--max-failures=1'], { RC24_EVIDENCE_DIR: join(run, 'edge-25') }))
  const dist = checkDist(join(run, 'release-dist'))
  output('release-dist-check', dist)
  if (dist.missing.length || !Object.values(dist.checks).every(Boolean)) throw new Error('Release dist check failed')
  git(['diff', '--check'])
  verify('final')
  output('result', { gate: 'PASS', scope: 'RC24 frontend isolated API, not production API or KnowHub25 integration', run, stages })
  console.log('PASS: ' + run)
} catch (error) {
  output('result', { gate: 'BLOCKED', error: String(error), run, stages })
  console.error(error)
  process.exitCode = 1
} finally {
  closeSync(lockFd)
  unlinkSync(lock) // Only this process's temporary lock; evidence and workspace files remain.
}
