import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createSummaryRequestPool, createVisibleSummarySession, type SummaryState,
  type SummaryTarget } from './visibleDocumentSummary.ts'

const tick = () => new Promise<void>(resolve => setImmediate(resolve))
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
const target = (overrides: Partial<SummaryTarget> = {}): SummaryTarget => ({
  id: 'document', kbId: 'kb', scope: 'user:space', version: 'v1', eligible: true, ...overrides,
})
const response = (description: string, changes: Record<string, unknown> = {}) => ({
  success: true, data: { id: 'document', knowledge_base_id: 'kb', channel: 'feishu_personal',
    parse_status: 'completed', description, ...changes },
})

test('summary pool limits concurrent requests and discards canceled offscreen queued work', async () => {
  const pool = createSummaryRequestPool(2)
  const pending = [deferred<string>(), deferred<string>(), deferred<string>(), deferred<string>()]
  const started: number[] = []
  const jobs = pending.map((value, index) => pool.run(async () => {
    started.push(index)
    return value.promise
  }))
  const settled = Promise.allSettled(jobs.map(job => job.promise))
  await tick()
  assert.deepEqual(started, [0, 1])
  jobs[2].cancel()
  pending[0].resolve('first')
  await tick()
  assert.deepEqual(started, [0, 1, 3])
  pending[1].resolve('second')
  pending[3].resolve('fourth')
  const results = await settled
  assert.equal(results[2].status, 'rejected')
})

test('canceling a running request cannot open extra slots before it settles', async () => {
  const pool = createSummaryRequestPool(1)
  const first = deferred<string>()
  let signal: AbortSignal | undefined
  let nextStarted = false
  const old = pool.run(s => { signal = s; return first.promise })
  await tick()
  old.cancel()
  const next = pool.run(async () => { nextStarted = true; return 'next' })
  assert.equal(signal?.aborted, true)
  await tick()
  assert.equal(nextStarted, false)
  first.resolve('late')
  await old.promise
  assert.equal(await next.promise, 'next')
})

test('only eligible visible documents fetch; leaving discards text and returning rechecks', async () => {
  let calls = 0
  let state: SummaryState = { status: 'idle', description: '' }
  const session = createVisibleSummarySession(async () => { calls++; return response('Actual summary') },
    next => { state = next }, createSummaryRequestPool(1))
  session.target(target({ eligible: false }))
  session.visible(true)
  await tick()
  assert.equal(calls, 0)
  session.target(target())
  await tick()
  assert.equal(calls, 1)
  assert.equal(state.description, 'Actual summary')
  session.target(target())
  await tick()
  assert.equal(calls, 1, 'status polling must not refetch unchanged visible cards')
  session.visible(false)
  assert.equal(state.description, '')
  session.visible(true)
  await tick()
  assert.equal(calls, 2, 'no cross-visibility authorization cache')
  session.dispose()
  assert.equal(state.description, '')
})

test('workspace/user change clears text immediately and rejects late old-user responses', async () => {
  const old = deferred<unknown>()
  let calls = 0
  let state: SummaryState = { status: 'idle', description: '' }
  const session = createVisibleSummarySession(async () => {
    calls++
    return calls === 1 ? old.promise : response('New authorized summary')
  }, next => { state = next }, createSummaryRequestPool(1))
  session.target(target())
  session.visible(true)
  await tick()
  session.target(target({ scope: 'different-user:different-space' }))
  assert.equal(state.description, '')
  old.resolve(response('Old private summary'))
  await tick()
  await tick()
  assert.equal(state.description, 'New authorized summary')
  session.dispose()
})

test('denials, timeouts, mismatched documents and metadata responses never become empty summaries', async () => {
  for (const value of [
    { success: false, error: { code: 403 } }, response('SECRET', { id: 'another-document' }),
    response('SECRET', { knowledge_base_id: 'another-kb' }), response('SECRET', { metadata_only: true }),
    response('SECRET', { parse_status: 'processing' }), response('SECRET', { channel: 'unknown' }),
    { success: true, data: { id: 'document', knowledge_base_id: 'kb', channel: 'feishu_personal', parse_status: 'completed' } },
    new Error('request timeout'),
  ]) {
    let state: SummaryState = { status: 'idle', description: '' }
    const session = createVisibleSummarySession(async () => {
      if (value instanceof Error) throw value
      return value
    }, next => { state = next }, createSummaryRequestPool(1))
    session.target(target())
    session.visible(true)
    await tick()
    assert.equal(state.status, 'unavailable')
    assert.equal(state.description, '')
    session.dispose()
  }
})

test('only a successful authorized empty body is shown as truly missing summary', async () => {
  let state: SummaryState = { status: 'idle', description: '' }
  const session = createVisibleSummarySession(async () => response(''), next => { state = next },
    createSummaryRequestPool(1))
  session.target(target())
  session.visible(true)
  await tick()
  assert.equal(state.status, 'empty')
  session.dispose()
})
