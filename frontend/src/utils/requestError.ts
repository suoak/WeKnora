export type AxiosTransportErrorKind = 'timeout' | 'canceled' | 'network'

/** Classify Axios failures that have no HTTP response. */
export function classifyAxiosTransportError(error: any): AxiosTransportErrorKind {
  const code = error?.code
  if (code === 'ERR_CANCELED') return 'canceled'
  if (code === 'ECONNABORTED' || code === 'ETIMEDOUT') return 'timeout'
  return 'network'
}
