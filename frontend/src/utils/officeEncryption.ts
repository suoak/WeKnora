/** Bounded local preflight. Unknown/invalid never mean password encrypted. */
export type OfficeState = 'encrypted' | 'drm' | 'readable' | 'protected' | 'invalid' | 'unknown' | 'not_office'
export const OFFICE_ENCRYPTED_CODE = 2400
export const officeEncryptedMessage = '该 Office 文件设置了打开密码，无法上传。请在 Office 或 WPS 中移除打开密码后重试。'
export const officeDRMMessage = '该文件受 DRM 权限管理保护，知识库无法解析。请在有权限的原系统中导出未受保护的副本后再上传。'
export const officeRestrictionMessage = (state: OfficeState) => state === 'drm' ? officeDRMMessage : officeEncryptedMessage
const cache = new WeakMap<Blob, Promise<OfficeState>>()
const u16 = (p: Uint8Array, i: number) => new DataView(p.buffer, p.byteOffset, p.byteLength).getUint16(i, true)
const u32 = (p: Uint8Array, i: number) => new DataView(p.buffer, p.byteOffset, p.byteLength).getUint32(i, true)
const endChain = 0xfffffffe
interface Entry { name: string; kind: number; start: number; size: number; left: number; right: number; child: number }

export function inspectOffice(file: Blob & { name?: string }): Promise<OfficeState> {
  let pending = cache.get(file)
  if (!pending) { pending = detect(file); cache.set(file, pending) }
  return pending
}

async function detect(file: Blob & { name?: string }): Promise<OfficeState> {
  let left = 4 << 20, steps = 0
  const deadline = Date.now() + 750
  const office = /\.(docx|xlsx|xlsm|pptx|docm|pptm|doc|xls|ppt|wps|et|dps)$/i.test(file.name ?? '')
  const read = async (off: number, n: number) => {
    if (++steps > 8192 || n > left || Date.now() > deadline) throw new Error('budget')
    if (off < 0 || off + n > file.size) throw new Error('invalid')
    left -= n
    // A stalled FileReader must not hold the upload indefinitely.
    let timer: ReturnType<typeof setTimeout> | undefined
    try {
      const buf = await Promise.race([file.slice(off, off + n).arrayBuffer(), new Promise<never>((_, reject) => {
        timer = setTimeout(() => reject(new Error('budget')), Math.max(1, deadline - Date.now()))
      })])
      return new Uint8Array(buf)
    } finally { clearTimeout(timer) }
  }
  try {
    if (file.size < 8) return office ? 'invalid' : 'not_office'
    const head = await read(0, 8)
    if (head.every((v, i) => v === [208, 207, 17, 224, 161, 177, 26, 225][i])) {
      const h = await read(0, 512), ver = u16(h, 26), shift = u16(h, 30)
      if (u16(h, 28) !== 65534 || !((ver === 3 && shift === 9) || (ver === 4 && shift === 12)) || u16(h, 32) !== 6 || u32(h, 56) !== 4096) return 'invalid'
      const ss = 1 << shift, nf = u32(h, 44), nd = u32(h, 72), nm = u32(h, 64)
      if (nf > 4096 || nd > 64 || nm > 1024) return 'unknown'
      const sectors = new Map<number, Uint8Array>()
      const sector = async (id: number) => {
        const cached = sectors.get(id)
        if (cached) return cached
        const data = await read((id + 1) * ss, ss)
        if (sectors.size < 128) sectors.set(id, data)
        return data
      }
      const fats: number[] = []
      for (let i = 76; i < 512 && fats.length < nf; i += 4) fats.push(u32(h, i))
      let id = u32(h, 68)
      for (let i = 0; i < nd && fats.length < nf; i++) {
        const p = await sector(id)
        for (let j = 0; j < ss - 4 && fats.length < nf; j += 4) fats.push(u32(p, j))
        id = u32(p, ss - 4)
      }
      if (fats.length !== nf) return 'invalid'
      const next = async (id: number) => {
        const fat = fats[Math.floor(id / (ss / 4))]
        if (fat === undefined) throw new Error('invalid')
        return u32(await sector(fat), id % (ss / 4) * 4)
      }
      const chain = async (start: number, limit: number) => {
        const ids: number[] = [], seen = new Set<number>()
        for (let id = start; id !== endChain; id = await next(id)) {
          if (seen.has(id) || ids.length >= limit) throw new Error('invalid')
          seen.add(id); ids.push(id)
        }
        return ids
      }
      const dirs: Entry[] = []
      for (const id of await chain(u32(h, 48), 256)) {
        const p = await sector(id)
        for (let j = 0; j < ss; j += 128) {
          const q = p.subarray(j, j + 128), kind = q[66]!, n = u16(q, 64)
          if (![0, 1, 2, 5].includes(kind) || (kind && (n < 2 || n > 64 || n % 2))) return 'invalid'
          let name = ''
          if (kind) for (let k = 0; k < n - 2; k += 2) name += String.fromCharCode(u16(q, k))
          dirs.push({ name, kind, start: u32(q, 116), size: u32(q, 120) + (ver === 4 ? u32(q, 124) * 4294967296 : 0), left: u32(q, 68), right: u32(q, 72), child: u32(q, 76) })
        }
      }
      if (dirs[0]?.kind !== 5) return 'invalid'
      const streams = new Map<string, Entry>(), todo = [dirs[0].child], seen = new Set<number>()
      while (todo.length) {
        const id = todo.pop()!
        if (id === 0xffffffff) continue
        const d = dirs[id]
        if (!d || seen.has(id)) return 'invalid'
        seen.add(id); todo.push(d.left, d.right)
        if (d.kind === 2) { if (streams.has(d.name)) return 'invalid'; streams.set(d.name, d) }
      }
      const mini = nm ? await chain(u32(h, 60), 1024) : []
      const root = nm ? await chain(dirs[0].start, 4096) : []
      const prefix = async (d: Entry, n: number) => {
        n = Math.min(n, d.size)
        const out = new Uint8Array(n), seen = new Set<number>()
        let pos = 0, id = d.start
        while (pos < n) {
          if (seen.has(id)) throw new Error('invalid')
          seen.add(id)
          let p: Uint8Array
          if (d.size < 4096) {
            const rid = root[Math.floor(id * 64 / ss)], mid = mini[Math.floor(id / (ss / 4))]
            if (rid === undefined || mid === undefined) throw new Error('invalid')
            p = await read((rid + 1) * ss + id % (ss / 64) * 64, 64)
            id = u32(await sector(mid), id % (ss / 4) * 4)
          } else { p = await sector(id); id = await next(id) }
          const take = Math.min(p.length, n - pos); out.set(p.subarray(0, take), pos); pos += take
        }
        return out
      }
      const info = streams.get('EncryptionInfo'), pkg = streams.get('EncryptedPackage')
      if (pkg && !info && pkg.size >= 8) {
        const findPath = (names: string[]): Entry | undefined => {
          let parent = dirs[0]!
          for (let i = 0; i < names.length; i++) {
            const todo = [parent.child], seen = new Set<number>(), matches: Entry[] = []
            while (todo.length) {
              const id = todo.pop()!
              if (id === 0xffffffff) continue
              const d = dirs[id]
              if (!d || seen.has(id)) return undefined
              seen.add(id); todo.push(d.left, d.right)
              if (d.name === names[i] && d.kind === (i === names.length - 1 ? 2 : 1)) matches.push(d)
            }
            if (matches.length !== 1) return undefined
            parent = matches[0]!
          }
          return parent
        }
        const definition = findPath(['\x06DataSpaces', 'DataSpaceInfo', 'DRMEncryptedDataSpace'])
        const primary = findPath(['\x06DataSpaces', 'TransformInfo', 'DRMEncryptedTransform', '\x06Primary'])
        if (definition && primary && definition.size >= 56) {
          const p = await prefix(definition, 56), q = await prefix(primary, 512)
          const text = (v: Uint8Array) => {
            let s = ''
            for (let i = 0; i + 1 < v.length; i += 2) s += String.fromCharCode(u16(v, i))
            return s
          }
          if (u32(p, 0) === 8 && u32(p, 4) === 1 && u32(p, 8) === 42 &&
              text(p.subarray(12, 54)) === 'DRMEncryptedTransform' && text(q).includes('Microsoft.Metadata.DRMTransform')) return 'drm'
        }
      }
      if (info && pkg && info.size >= 8 && pkg.size >= 8) {
        const p = await prefix(info, 8)
        return [2, 3, 4].includes(u16(p, 0)) && [2, 3, 4].includes(u16(p, 2)) ? 'encrypted' : 'unknown'
      }
      if (info || pkg) return 'invalid'
      const word = streams.get('WordDocument')
      if (word) {
        const p = await prefix(word, 32)
        if (p.length < 32 || u16(p, 0) !== 0xa5ec) return 'invalid'
        return u16(p, 10) & 0x100 ? 'encrypted' : u16(p, 10) & 0x800 ? 'protected' : 'readable'
      }
      const book = streams.get('Workbook') ?? streams.get('Book')
      if (book) {
        const p = await prefix(book, 65536)
        if (p.length < 8 || ![0x809, 0x409, 0x209, 9].includes(u16(p, 0))) return 'unknown'
        let protectedFile = false
        for (let i = 0; i + 4 <= p.length;) {
          const tag = u16(p, i), n = u16(p, i + 2)
          if (i + 4 + n > p.length) return 'unknown'
          if (tag === 0x2f && n >= 4) return 'encrypted'
          if ([0x12, 0x19].includes(tag) && n >= 2 && u16(p, i + 4)) protectedFile = true
          if (tag === 10) return protectedFile ? 'protected' : 'readable'
          i += 4 + n
        }
        return 'unknown'
      }
      const user = streams.get('Current User')
      if (user) {
        const p = await prefix(user, 20)
        if (p.length < 20 || u16(p, 2) !== 0xff6 || u32(p, 8) !== 20) return 'invalid'
        return u32(p, 12) === 0xf3d1c4df ? 'encrypted' : u32(p, 12) === 0xe391c05f ? 'readable' : 'unknown'
      }
      return 'unknown'
    }
    if (u32(head, 0) === 0x04034b50) {
      const tail = await read(Math.max(0, file.size - 65557), Math.min(file.size, 65557))
      let end = -1
      for (let i = tail.length - 22; i >= 0; i--) if (u32(tail, i) === 0x06054b50 && i + 22 + u16(tail, i + 20) === tail.length) { end = i; break }
      if (end < 0) return 'invalid'
      if (u16(tail, end + 4) || u16(tail, end + 6)) return 'unknown'
      const count = u16(tail, end + 10), size = u32(tail, end + 12), off = u32(tail, end + 16)
      if (count > 8192 || size > 1 << 20) return 'unknown'
      const cd = await read(off, size)
      let pos = 0, known = false, encrypted = false
      const protectionParts: { off: number; compressed: number; size: number; method: number }[] = []
      for (let i = 0; i < count; i++) {
        if (pos + 46 > cd.length || u32(cd, pos) !== 0x02014b50) return 'invalid'
        const n = u16(cd, pos + 28), extra = u16(cd, pos + 30), comment = u16(cd, pos + 32)
        if (pos + 46 + n + extra + comment > cd.length) return 'invalid'
        const name = new TextDecoder().decode(cd.subarray(pos + 46, pos + 46 + n))
        if (u16(cd, pos + 8) & 1) encrypted = true
        if (['word/document.xml', 'xl/workbook.xml', 'ppt/presentation.xml'].includes(name)) known = true
        if (['word/settings.xml', 'xl/workbook.xml'].includes(name) || /^xl\/worksheets\/[^/]+\.xml$/.test(name)) {
          protectionParts.push({ off: u32(cd, pos + 42), compressed: u32(cd, pos + 20), size: u32(cd, pos + 24), method: u16(cd, pos + 10) })
        }
        pos += 46 + n + extra + comment
      }
      if (encrypted && (office || known)) return 'encrypted'
      let inflated = 0, protectedFile = false
      for (const part of known ? protectionParts : []) {
        inflated += part.size
        if (part.size > 256 << 10 || part.compressed > 256 << 10 || inflated > 1 << 20) return 'unknown'
        const local = await read(part.off, 30)
        if (u32(local, 0) !== 0x04034b50) return 'invalid'
        let data = await read(part.off + 30 + u16(local, 26) + u16(local, 28), part.compressed)
        if (part.method === 8) {
          // Optional classification only: unsupported browser inflation never
          // turns protection into a password rejection.
          try {
            const stream = new Blob([data]).stream().pipeThrough(new DecompressionStream('deflate-raw'))
            const reader = stream.getReader(), chunks: Uint8Array[] = []
            let length = 0
            try {
              while (true) {
                if (Date.now() > deadline) throw new Error('budget')
                const chunk = await reader.read()
                if (chunk.done) break
                length += chunk.value.length
                if (length > 256 << 10) throw new Error('budget')
                chunks.push(chunk.value)
              }
            } finally { void reader.cancel().catch(() => undefined) }
            data = new Uint8Array(length)
            let offset = 0
            for (const chunk of chunks) { data.set(chunk, offset); offset += chunk.length }
          } catch { return 'unknown' }
        } else if (part.method !== 0) return 'unknown'
        const xml = new TextDecoder().decode(data)
        if (/documentProtection|writeProtection|sheetProtection|workbookProtection|fileSharing/.test(xml)) protectedFile = true
      }
      if (protectedFile) return 'protected'
      return known ? 'readable' : office ? 'unknown' : 'not_office'
    }
    return office ? 'unknown' : 'not_office'
  } catch (e) { return e instanceof Error && e.message === 'budget' ? 'unknown' : 'invalid' }
}

export async function assertOfficeUploadable(file: File): Promise<void> {
  const state = await inspectOffice(file)
  if (state === 'encrypted' || state === 'drm') {
    const message = officeRestrictionMessage(state)
    throw Object.assign(new Error(message), { code: OFFICE_ENCRYPTED_CODE, message })
  }
}

/** Sequential bounded reads also keep large selections from multiplying memory. */
export async function preflightOfficeFiles(files: File[]) {
  const accepted: File[] = [], rejected: File[] = []
  const messages = new Set<string>()
  for (const file of files) {
    const state = await inspectOffice(file)
    if (state === 'encrypted' || state === 'drm') { rejected.push(file); messages.add(officeRestrictionMessage(state)) }
    else accepted.push(file)
  }
  return { accepted, rejected, rejectionMessage: [...messages].join(' ') }
}
