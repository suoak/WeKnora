// Advisory UI validation only. The server remains authoritative for URL,
// canonical resource identity, permissions and safe publication.
export function normalizeFeishuLinks(text: string): {valid:string[];invalid:string[]} {
 const valid = new Set<string>(), invalid = new Set<string>()
 for (const line of text.split(/\r?\n/)) {
  const raw = line.trim(); if (!raw) continue
  try {
   const u = new URL(raw)
   if (u.protocol !== 'https:' || u.username || u.password || u.port ||
     !(u.hostname === 'feishu.cn' || u.hostname.endsWith('.feishu.cn')) ||
     !/^\/(docx|doc|sheets|base|wiki|file|slides|mindnotes|mindnote)\/[A-Za-z0-9_-]{1,256}\/?$/.test(u.pathname)) throw new Error('invalid')
   u.search = ''; u.hash = ''; valid.add(u.href)
  } catch { invalid.add(raw) }
 }
 return {valid:[...valid],invalid:[...invalid]}
}
