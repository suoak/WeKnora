// Failed imports may have no title yet. Link only to a recognized source URL;
// never render user-provided schemes, credentials, query parameters or fragments.
export function originalDocumentURL(value: string): string {
  try {
    const url = new URL(value)
    if (url.protocol !== 'https:' || url.username || url.password || url.port ||
        !(url.hostname === 'feishu.cn' || url.hostname.endsWith('.feishu.cn')) ||
        !/^\/(docx|docs|doc|sheets|wiki|base|file)\/[a-zA-Z0-9]+\/?$/.test(url.pathname)) return ''
    return url.origin + url.pathname
  } catch {
    return ''
  }
}
