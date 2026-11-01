export function personalDocumentAction(channel?: string, status?: string) {
  if (channel !== 'feishu_personal') return undefined
  if (['pending', 'processing', 'finalizing'].includes(status || '')) return undefined
  return status === 'failed' ? 'retryParse' : 'syncSource'
}

export function personalReauthorizationAllowed(error: unknown): boolean {
  if (!error || typeof error !== 'object') return false
  const value = error as { status?: number; error?: { details?: Record<string, unknown> } }
  return value.status === 403 &&
    value.error?.details?.reason === 'personal_exact_source_owner_reauthorization_required' &&
    value.error.details.reauthorize_allowed === true
}

export function personalAuthorizationTarget(value: string): string {
  const target = new URL(value)
  if (target.origin !== 'https://accounts.feishu.cn' || target.username || target.password || target.hash) {
    throw new Error('invalid_authorization_origin')
  }
  return target.href
}

export function personalFailureReason(message?: string) {
  const text = (message || '').slice(0, 4096).toLowerCase()
  if (text.includes('security_rejected')) return 'safetyFailure'
  if (/reconnect|unauthoriz|permission|grant/.test(text)) return 'permissionFailure'
  if (/deadline|timeout|timed out/.test(text)) return 'timeoutFailure'
  if (/unverified|fence|revision|binding|manifest|hash/.test(text)) return 'verificationFailure'
  return 'processingFailure'
}
