import { readdirSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { join, relative } from 'node:path'
import ts from 'typescript'
import { parse as parseSfc } from '@vue/compiler-sfc'
import { parse as parseDom, NodeTypes } from '@vue/compiler-dom'
import { LOCALE_BUNDLES, getLocaleValueAtPath } from '../../src/i18n/localeKeyAudit'

export type Usage = { key: string; file: string; line: number; params: string[] }
export function scanExpression(source: string, file: string, offset = 0): Usage[] {
  const ast = ts.createSourceFile(file + '.ts', source, ts.ScriptTarget.Latest, true)
  const calls: Usage[] = []
  function walk(node: ts.Node) {
    if (file === 'utils/artifactLibrary.ts' && ts.isTypeAliasDeclaration(node) && ts.isUnionTypeNode(node.type)) {
      const prefix = node.name.text === 'ArtifactCategory' ? 'artifactLibrary.categories.' : node.name.text === 'ArtifactDateGroup' ? 'artifactLibrary.groups.' : ''
      if (prefix) for (const type of node.type.types) if (ts.isLiteralTypeNode(type) && ts.isStringLiteral(type.literal)) calls.push({key:prefix+type.literal.text,file,line:ast.getLineAndCharacterOfPosition(type.getStart(ast)).line+1,params:[]})
    }
    if (ts.isPropertyAssignment(node) && node.name && ts.isStringLiteral(node.initializer)) {
      const name = node.name.getText(ast).replace(/^['"]|['"]$/g, '')
      const value = node.initializer.text
      const knownPrefix = name === 'labelKey' ? ({'utils/resourceSorting.ts':'resourceSort.','views/knowledge/settings/chunkingSamples.ts':'knowledgeEditor.chunking.debug.'} as Record<string,string>)[file] || '' : ''
      if (knownPrefix || /^(labelKey|titleKey|hintKey|i18nKey|descriptionKey|messageKey|errorKey|title|description|label|help|action)$/.test(name) && /^[A-Za-z]\w*(?:\.[\w-]+)+$/.test(value)) {
        calls.push({key:knownPrefix+value,file,line:offset+ast.getLineAndCharacterOfPosition(node.getStart(ast)).line+1,params:[]})
      }
    }
    if (ts.isCallExpression(node) && /^(?:t|\$t|i18n\.global\.t)$/.test(node.expression.getText(ast))) {
      function literalKeys(key: ts.Node | undefined): string[] {
        if (!key) return []
        if (ts.isStringLiteral(key) || ts.isNoSubstitutionTemplateLiteral(key)) return [key.text]
        if (ts.isConditionalExpression(key)) return [...literalKeys(key.whenTrue), ...literalKeys(key.whenFalse)]
        return []
      }
      for (const key of literalKeys(node.arguments[0])) {
        const params = node.arguments[1]
        calls.push({ key, file, line: offset + ast.getLineAndCharacterOfPosition(node.getStart(ast)).line + 1,
          params: params && ts.isObjectLiteralExpression(params) ? params.properties.map(p => p.name?.getText(ast).replace(/^['"]|['"]$/g, '') || '') : [] })
      }
    }
    ts.forEachChild(node, walk)
  }
  walk(ast)
  return calls
}
export function scanSources(): Usage[] {
  const root = fileURLToPath(new URL('../../src/', import.meta.url)), calls: Usage[] = []
  function walk(dir: string) {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const path = join(dir, entry.name), file = relative(root, path).replaceAll('\\', '/')
      if (entry.isDirectory()) { if (!file.startsWith('i18n')) walk(path); continue }
      if (!/\.(vue|ts|js)$/.test(file) || /\.(test|spec)\./.test(file)) continue
      const source = readFileSync(path, 'utf8')
      if (!file.endsWith('.vue')) { calls.push(...scanExpression(source, file)); continue }
      const { descriptor } = parseSfc(source)
      for (const block of [descriptor.script, descriptor.scriptSetup]) if (block) calls.push(...scanExpression(block.content, file, block.loc.start.line - 1))
      if (descriptor.template) {
        function visit(node: any) {
          if (node.type === NodeTypes.INTERPOLATION) calls.push(...scanExpression(node.content.content, file, descriptor.template!.loc.start.line + node.loc.start.line - 2))
          for (const prop of node.props || []) if (prop.type === NodeTypes.DIRECTIVE && prop.exp) calls.push(...scanExpression(prop.exp.content, file, descriptor.template!.loc.start.line + prop.loc.start.line - 2))
          for (const child of node.children || []) visit(child)
        }
        visit(parseDom(descriptor.template.content))
      }
    }
  }
  walk(root)
  return calls
}
export function missingKeys(calls = scanSources()) {
  const byKey = new Map<string, Usage[]>()
  for (const call of calls) {
    // EmbedInputField uses the separately shipped embed bundle, verified separately.
    if (call.file === 'components/EmbedInputField.vue' || call.file === 'composables/useEmbedChatSession.ts') continue
    if (Object.values(LOCALE_BUNDLES).some(bundle => typeof getLocaleValueAtPath(bundle, call.key) === 'string')) continue
    byKey.set(call.key, [...byKey.get(call.key) || [], call])
  }
  return [...byKey].map(([key, calls]) => ({ key, calls }))
}
