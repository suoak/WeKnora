export function buildPortalSpacesPath(params: { q?: string; stage?: string; category?: string } = {}) {
  const query = new URLSearchParams()
  if (params.q) query.set('q', params.q)
  if (params.stage && params.stage !== 'all') query.set('stage', params.stage)
  if (params.category) query.set('category', params.category)
  return `/api/v1/portal/spaces${query.size ? `?${query}` : ''}`
}

export const portalAccessRequestBody = (reason: string) => ({ reason })
