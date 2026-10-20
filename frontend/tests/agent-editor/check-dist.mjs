import { readFileSync, readdirSync, statSync } from 'node:fs'
import { resolve, relative, dirname, extname } from 'node:path'
import { createHash } from 'node:crypto'

export function checkDist(root) {
  const walk = dir => readdirSync(dir).flatMap(name => {
    const path = resolve(dir, name)
    return statSync(path).isDirectory() ? walk(path) : [path]
  })
  const files = walk(root), missing = [], references = []
  const names = new Set(files.map(path => relative(root, path).replaceAll('\\', '/')))
  const reference = (source, value) => {
    if (/^(?:data:|https?:|\/\/|#)/.test(value)) return
    const target = value.startsWith('/') ? resolve(root, '.' + value) : resolve(dirname(source), value.split(/[?#]/)[0])
    const name = relative(root, target).replaceAll('\\', '/')
    references.push({ source: relative(root, source), value })
    if (!names.has(name)) missing.push(references.at(-1))
  }
  let js = ''
  for (const path of files) {
    if (!['.html', '.js', '.css'].includes(extname(path))) continue
    const text = readFileSync(path, 'utf8')
    if (path.endsWith('.js')) {
      js += '\n' + text
      for (const pattern of [/\b(?:import|from)\s*["'](\.[^"']+)["']/g, /\bimport\(\s*["'](\.[^"']+)["']/g, /new URL\(["'](\.[^"']+)["']\s*,\s*import\.meta\.url/g]) {
        for (const match of text.matchAll(pattern)) reference(path, match[1])
      }
      for (const match of text.matchAll(/["'](assets\/[^"']+\.(?:js|css))["']/g)) reference(resolve(root, 'index.html'), match[1])
    }
    if (path.endsWith('.html')) for (const match of text.matchAll(/(?:src|href)=["']([^"']+)["']/g)) {
      if (/\.(?:js|css|ico|svg|png)(?:[?#]|$)/.test(match[1])) reference(path, match[1])
    }
    if (path.endsWith('.css')) for (const match of text.matchAll(/url\(["']?([^"')]+)["']?\)/g)) reference(path, match[1])
  }
  const localeKeys = ['recentlyUpdated', 'earliestUpdated', 'recentlyCreated', 'earliestCreated', 'nameAscending', 'nameDescending', 'documentCount', 'filters', 'clearFilters']
  const checks = {
    mainAndEmbed: names.has('index.html') && names.has('embed.html'),
    noDiagnosticEntries: ![...names].some(name => /(?:tests\/|component\.html|qa-component|manual|\.map$)/.test(name)),
    noFixtureBootstrap: !/(?:local-fixture-token|editorHarness|rc24Export)/.test(js),
    fiveLanguageDefinitions: localeKeys.every(key => [...js.matchAll(new RegExp('(?<![A-Za-z])' + key + ':', 'g'))].length >= 5),
    nonempty: files.every(path => statSync(path).size > 0),
  }
  return { checks, missing, references: references.length, files: files.map(path => ({
    path: relative(root, path).replaceAll('\\', '/'), bytes: statSync(path).size,
    sha256: createHash('sha256').update(readFileSync(path)).digest('hex'),
  })) }
}
