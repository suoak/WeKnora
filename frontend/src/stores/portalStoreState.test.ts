import assert from 'node:assert/strict'
import test from 'node:test'
import { createPortalStoreState, PORTAL_REFRESH_MS } from './portalStoreState'
import type { PortalSpace } from '../api/portal'

const space = (count = 2): PortalSpace => ({ tenant_id: 1, display_name: 'One', description: '', category: 'ntos', responsible_team: '', contact: '', stages: ['design'], featured: false, knowledge_base_count: 1, file_count: count, document_entry_count: count, access_state: 'accessible', current_role: 'viewer', can_request_access: false, access_request_pending: false, membership_suspended: false, interaction_action: 'enter' })
const deferred = <T>() => { let resolve!: (value: T) => void; let reject!: (cause: unknown) => void; const promise = new Promise<T>((a, b) => { resolve = a; reject = b }); return { promise, resolve, reject } }
function fixture() {
  let now = 1000, session = 'alice', calls = 0, stageCalls = 0
  let read = async () => ({ success: true, data: [space()] })
  let afterWrite = () => {}
  const store = createPortalStoreState({
    listPortalStages: async () => { stageCalls++; return { success: true, data: [] } },
    listPortalSpaces: () => { calls++; return read() },
    listMyPortalSpaces: async () => ({ success: true, data: [] }),
    createPortalAccessRequest: async () => { afterWrite(); return { success: true, data: {} as any } },
  }, () => session, key => key, () => {}, () => now)
  return { store, calls: () => calls, stageCalls: () => stageCalls, read: (fn: typeof read) => { read = fn }, afterWrite: (fn: () => void) => { afterWrite = fn }, advance: (ms: number) => { now += ms }, switchSession: () => { session = 'bob'; store.clear() }, now: () => now }
}

test('first load uses skeleton, successful empty snapshots stay loaded; passive triggers share freshness', async () => {
  const f = fixture(), d = deferred<{ success: boolean; data: PortalSpace[] }>()
  f.read(() => d.promise)
  const task = f.store.initialize()
  assert.equal(f.store.initialLoading.value, true)
  assert.equal(f.store.initialize(), task)
  d.resolve({ success: true, data: [] }); await task
  assert.equal(f.store.initialLoading.value, false)
  assert.equal(f.store.initialized.value, true)
  for (let i = 0; i < 20; i++) await f.store.initialize()
  assert.equal(f.calls(), 1); assert.equal(f.stageCalls(), 1)
  f.advance(PORTAL_REFRESH_MS)
  const next = f.store.initialize()
  assert.equal(f.store.initialLoading.value, false)
  assert.equal(f.store.refreshing.value, true)
  await next
  assert.equal(f.calls(), 2)
})

test('slow/unchanged background refresh preserves snapshot identity and failures preserve valid content', async () => {
  const f = fixture(); await f.store.initialize()
  const snapshot = f.store.overviewSpaces.value, stages = f.store.stages.value
  const d = deferred<{ success: boolean; data: PortalSpace[] }>(); f.read(() => d.promise)
  f.advance(PORTAL_REFRESH_MS)
  const task = f.store.initialize()
  assert.equal(f.store.initialLoading.value, false)
  assert.equal(f.store.overviewSpaces.value, snapshot)
  d.resolve({ success: true, data: [space()] }); await task
  assert.equal(f.store.overviewSpaces.value, snapshot); assert.equal(f.store.stages.value, stages)
  f.read(async () => { throw new Error('offline') }); f.advance(PORTAL_REFRESH_MS)
  await f.store.initialize()
  assert.equal(f.store.overviewSpaces.value, snapshot)
  assert.equal(f.store.overviewError.value, false)
  for (const delay of [5000, 15000, 30000, PORTAL_REFRESH_MS, PORTAL_REFRESH_MS]) {
    assert.equal(f.store.nextRefreshAt.value, f.now() + delay)
    const calls = f.calls(); await f.store.initialize(); assert.equal(f.calls(), calls)
    f.advance(delay); await f.store.initialize()
  }
  f.read(async () => ({ success: true, data: [space(3)] }))
  await f.store.initialize(true)
  assert.equal(f.store.overviewSpaces.value[0].document_entry_count, 3)
  assert.equal(f.store.nextRefreshAt.value, f.now() + PORTAL_REFRESH_MS)
})

test('asset invalidation queues one fresh read, discards older response, and combines duplicate calls', async () => {
  const f = fixture(); await f.store.initialize()
  const old = deferred<{ success: boolean; data: PortalSpace[] }>()
  f.read(() => old.promise); f.advance(PORTAL_REFRESH_MS)
  const first = f.store.initialize()
  f.store.invalidate()
  f.read(async () => ({ success: true, data: [space(4)] }))
  const second = f.store.initialize()
  assert.equal(f.store.initialize(), second)
  old.resolve({ success: true, data: [space(99)] })
  await Promise.all([first, second])
  assert.equal(f.calls(), 3)
  assert.equal(f.store.overviewSpaces.value[0].document_entry_count, 4)
})

test('session change clears synchronously and late responses cannot restore old authorization', async () => {
  const f = fixture(); await f.store.initialize()
  const old = deferred<{ success: boolean; data: PortalSpace[] }>()
  f.read(() => old.promise); const task = f.store.initialize(true)
  f.switchSession()
  assert.deepEqual(f.store.overviewSpaces.value, []); assert.deepEqual(f.store.spaces.value, [])
  old.resolve({ success: true, data: [space(99)] }); await task
  assert.deepEqual(f.store.overviewSpaces.value, [])
  f.read(async () => ({ success: true, data: [] })); await f.store.initialize()
  assert.equal(f.store.initialized.value, true)
})

test('confirmed permission denial discards valid snapshots rather than serving stale authorization', async () => {
  for (const status of [401, 403]) {
    const f = fixture(); await f.store.initialize()
    f.read(async () => { throw { status } }); await f.store.initialize(true)
    assert.deepEqual(f.store.overviewSpaces.value, []); assert.deepEqual(f.store.spaces.value, [])
    assert.equal(f.store.overviewError.value, true)
    assert.equal(f.store.nextRefreshAt.value, f.now() + PORTAL_REFRESH_MS)
  }
})

test('successful access request marks pending even when the follow-up read is offline', async () => {
  const f = fixture(); await f.store.initialize()
  f.afterWrite(f.store.invalidate)
  f.read(async () => { throw new Error('offline') })
  await f.store.requestAccess(1, ' reason ')
  assert.equal(f.store.overviewSpaces.value[0].access_request_pending, true)
  assert.equal(f.store.requestState.value[1], 'idle')
  assert.equal(f.store.overviewError.value, false)
})

test('filtered reads deduplicate and a late filter result cannot overwrite the newest selection', async () => {
  const f = fixture(); await f.store.initialize()
  const old = deferred<{ success: boolean; data: PortalSpace[] }>()
  f.store.search.value = 'old'; f.read(() => old.promise)
  const task = f.store.loadSpaces()
  assert.equal(f.store.loadSpaces(), task)
  f.store.search.value = 'new'; f.read(async () => ({ success: true, data: [space(7)] }))
  await f.store.loadSpaces()
  old.resolve({ success: true, data: [space(99)] }); await task
  assert.equal(f.store.spaces.value[0].document_entry_count, 7)
  assert.equal(f.calls(), 3)
})
