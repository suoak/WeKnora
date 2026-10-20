import { defineConfig, mergeConfig, type Plugin } from 'vite'
import { fileURLToPath } from 'node:url'
import main from '../../vite.config'
if (!process.env.RC24_BUILD_DIR || process.env.RC24_QA_BUILD !== '1') {
  throw new Error('An explicit isolated RC24_QA_BUILD=1 and RC24_BUILD_DIR are required')
}
const qaEntry: Plugin = {
  name: 'isolated-editor-qa-entry',
  generateBundle(_options, bundle) {
    const entry = Object.values(bundle).find(item => item.type === 'chunk' && item.name === 'component')
    if (!entry || entry.type !== 'chunk') throw new Error('QA component entry missing')
    const css = new Set<string>()
    const visited = new Set<string>()
    const visit = (name: string) => {
      if (visited.has(name)) return
      visited.add(name)
      const chunk = bundle[name]
      if (!chunk || chunk.type !== 'chunk') return
      for (const file of (chunk as any).viteMetadata?.importedCss ?? []) css.add(file)
      for (const dependency of chunk.imports) visit(dependency)
    }
    visit(entry.fileName)
    this.emitFile({ type: 'asset', fileName: 'tests/agent-editor/component.html', source:
      `<!doctype html><html><head><meta charset="UTF-8">${[...css].map(file => `<link rel="stylesheet" href="/${file}">`).join('')}</head><body><div id="app"></div><script type="module" src="/${entry.fileName}"></script></body></html>` })
  },
}
export default mergeConfig(main,defineConfig({
  plugins:[qaEntry],
  build:{outDir:process.env.RC24_BUILD_DIR,emptyOutDir:false,rollupOptions:{input:{
    component:fileURLToPath(new URL('./qa-component.ts',import.meta.url)),
  }}},
}))
