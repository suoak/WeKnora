// Consume a root OIDC result on the login route before the router's initial
// navigation can resolve '/' to Portal. Keep the fragment for App.vue.
export function oidcCallbackEntry(pathname: string, hash: string): string | null {
  if (pathname !== '/') return null
  const params = new URLSearchParams(hash.startsWith('#') ? hash.slice(1) : hash)
  if (!params.has('oidc_error') && !params.has('oidc_complete')) return null
  return `/login${hash.startsWith('#') ? hash : `#${hash}`}`
}
