export type LoadErrorKind = 'network' | 'forbidden' | 'verification' | 'notReady' | 'notFound' | 'generic'

export function classifyLoadError(error: unknown): LoadErrorKind {
  const candidate = error as {
    response?: { status?: number; data?: { error?: { message?: unknown; details?: { reason?: unknown } }; message?: unknown } }
    status?: number
    code?: string
    message?: unknown
    error?: { message?: unknown; details?: { reason?: unknown } }
  } | null
  const status = candidate?.response?.status ?? candidate?.status

  if (status === 403) {
    const reason = candidate?.error?.details?.reason ?? candidate?.response?.data?.error?.details?.reason
    if (reason === 'personal_published_snapshot_unverified') return 'notReady'
    const message = candidate?.error?.message ?? candidate?.response?.data?.error?.message
      ?? candidate?.response?.data?.message ?? candidate?.message
    if (typeof message === 'string' && new Set([
      'Document access requires verification',
      'Feishu source access requires verification',
      'personal_exact_document_unverified',
    ]).has(message.trim())) return 'verification'
    return 'forbidden'
  }
  if (status === 404) return 'notFound'
  if (!status || candidate?.code === 'ERR_NETWORK') return 'network'
  return 'generic'
}
