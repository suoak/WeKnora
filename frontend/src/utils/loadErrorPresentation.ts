export type LoadErrorKind = 'network' | 'forbidden' | 'notFound' | 'generic'

export function classifyLoadError(error: unknown): LoadErrorKind {
  const candidate = error as {
    response?: { status?: number }
    status?: number
    code?: string
  } | null
  const status = candidate?.response?.status ?? candidate?.status

  if (status === 403) return 'forbidden'
  if (status === 404) return 'notFound'
  if (!status || candidate?.code === 'ERR_NETWORK') return 'network'
  return 'generic'
}
