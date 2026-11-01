export function documentPreviewErrorKey(error: unknown): string {
  const value = error && typeof error === 'object' ? error as Record<string, any> : {};
  const status = value.status ?? value.response?.status;
  if (value.kind === 'timeout' || value.code === 'ECONNABORTED' || value.code === 'ETIMEDOUT') {
    return 'error.requestTimeout';
  }
  if (status === 401 || status === 403) return 'knowledgeBase.documentReadForbidden';
  if (status === 404 || status === 410) return 'loadState.notFoundDescription';
  return 'preview.loadFailed';
}
