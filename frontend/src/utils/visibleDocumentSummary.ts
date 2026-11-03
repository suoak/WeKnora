export interface SummaryTarget {
  id: string
  kbId: string
  scope: string
  version: string
  eligible: boolean
}

export type SummaryState = { status: 'idle' | 'loading' | 'ready' | 'empty' | 'unavailable'; description: string }
export type SummaryWork<T> = (signal: AbortSignal) => Promise<T>
type Job<T> = { work: SummaryWork<T>; controller: AbortController; started: boolean; canceled: boolean;
  resolve: (value: T) => void; reject: (error: unknown) => void }

// This pool contains pending work, not summaries or authorization decisions.
// Aborting an old request does not release its slot until the request settles.
export function createSummaryRequestPool(limit = 2) {
  if (!Number.isInteger(limit) || limit < 1) throw new Error('invalid_summary_concurrency')
  let active = 0
  const queued: Array<Job<any>> = []
  const pump = () => {
    while (active < limit && queued.length) {
      const job = queued.shift()!
      if (job.canceled) continue
      job.started = true
      active++
      void Promise.resolve().then(() => {
        if (job.canceled) throw new Error('summary_request_canceled')
        return job.work(job.controller.signal)
      }).then(job.resolve, job.reject)
        .finally(() => { active--; pump() })
    }
  }
  return {
    run<T>(work: SummaryWork<T>) {
      const controller = new AbortController()
      let job!: Job<T>
      const promise = new Promise<T>((resolve, reject) => {
        job = { work, controller, started: false, canceled: false, resolve, reject }
        queued.push(job)
      })
      pump()
      return { promise, cancel() {
        if (job.canceled) return
        job.canceled = true
        controller.abort()
        if (!job.started) job.reject(new Error('summary_request_canceled'))
      } }
    },
  }
}

export const visibleSummaryRequests = createSummaryRequestPool(2)

function checkedDescription(response: any, target: SummaryTarget): string {
  const row = response?.data
  if (response?.success !== true || row?.id !== target.id || row?.knowledge_base_id !== target.kbId ||
      row?.channel !== 'feishu_personal' || row?.metadata_only === true || row?.parse_status !== 'completed' ||
      typeof row?.description !== 'string') throw new Error('summary_content_response_unverified')
  return row.description
}

// A session belongs to one mounted, currently visible card. Leaving the
// viewport, changing identity/workspace/version or disposal discards its text.
// Returning to the card ALWAYS obtains a new server-authorized content read.
export function createVisibleSummarySession(
  fetcher: (target: SummaryTarget, signal: AbortSignal) => Promise<unknown>,
  changed: (state: SummaryState) => void,
  pool = visibleSummaryRequests,
) {
  let target: SummaryTarget | undefined
  let visible = false
  let generation = 0
  let request: ReturnType<typeof pool.run> | undefined
  let state: SummaryState = { status: 'idle', description: '' }
  const set = (next: SummaryState) => { state = next; changed(next) }
  const clear = () => {
    generation++
    request?.cancel()
    request = undefined
    set({ status: 'idle', description: '' })
  }
  const load = () => {
    if (!visible || !target?.eligible || state.status !== 'idle') return
    const expected = { ...target }
    const stamp = ++generation
    set({ status: 'loading', description: '' })
    const pending = pool.run(signal => fetcher(expected, signal).then(value => checkedDescription(value, expected)))
    request = pending
    void pending.promise.then(description => {
      if (generation !== stamp || !visible) return
      set({ status: description.trim() ? 'ready' : 'empty', description })
    }, () => {
      if (generation !== stamp || !visible) return
      // A denied, timed-out or malformed response is NEVER an empty summary.
      set({ status: 'unavailable', description: '' })
    }).finally(() => { if (request === pending) request = undefined })
  }
  return {
    target(next: SummaryTarget) {
      if (JSON.stringify(target) === JSON.stringify(next)) return
      clear()
      target = { ...next }
      load()
    },
    visible(next: boolean) {
      if (visible === next) return
      visible = next
      if (!next) clear()
      else load()
    },
    retry() { clear(); load() },
    dispose() { visible = false; target = undefined; clear() },
  }
}
