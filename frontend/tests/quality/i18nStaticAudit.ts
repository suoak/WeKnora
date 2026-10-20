import { readdirSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { join, relative } from 'node:path'
import ts from 'typescript'
import { parse as parseSfc } from '@vue/compiler-sfc'
import { parse as parseDom, NodeTypes } from '@vue/compiler-dom'
import { LOCALE_BUNDLES, getLocaleValueAtPath } from '../../src/i18n/localeKeyAudit'
import { feishuMessages } from '../../src/i18n/feishuPersonal'

const LOCAL_BUNDLES: Record<string, typeof feishuMessages> = {
  '@/i18n/feishuPersonal:feishuMessages': feishuMessages,
}
export type Usage = { key: string; file: string; line: number; params: string[]; bundle?: string }

// Resolve the actual useI18n local binding, including `t: ft`. Local keys must
// exist in that imported bundle; a same-named global key cannot mask a typo.
export function localTranslationScopes(source: string): Map<string, string> {
  const ast = ts.createSourceFile('component.ts', source, ts.ScriptTarget.Latest, true)
  const imports = new Map<string, string>(), scopes = new Map<string, string>()
  for (const node of ast.statements) {
    if (!ts.isImportDeclaration(node) || !ts.isStringLiteral(node.moduleSpecifier)) continue
    const bindings = node.importClause?.namedBindings
    if (bindings && ts.isNamedImports(bindings)) for (const item of bindings.elements) {
      imports.set(item.name.text, `${node.moduleSpecifier.text}:${(item.propertyName || item.name).text}`)
    }
  }
  function visit(node: ts.Node) {
    if (ts.isVariableDeclaration(node) && ts.isObjectBindingPattern(node.name) && node.initializer &&
        ts.isCallExpression(node.initializer) && node.initializer.expression.getText(ast) === 'useI18n') {
      const opts = node.initializer.arguments[0]
      if (opts && ts.isObjectLiteralExpression(opts)) {
        const props = new Map(opts.properties.filter(ts.isPropertyAssignment).map(p => [p.name.getText(ast), p.initializer]))
        const scope = props.get('useScope'), messages = props.get('messages')
        if (scope && ts.isStringLiteral(scope) && scope.text === 'local') {
          const bundle = messages && ts.isIdentifier(messages) ? imports.get(messages.text) : undefined
          for (const binding of node.name.elements) if ((binding.propertyName || binding.name).getText(ast) === 't') {
            scopes.set(binding.name.getText(ast), bundle || 'unresolved-local-bundle')
          }
        }
      }
    }
    ts.forEachChild(node, visit)
  }
  visit(ast)
  return scopes
}

export function scanExpression(source: string, file: string, offset = 0, scopes = new Map<string, string>()): Usage[] {
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
    if (ts.isCallExpression(node) && (/^(?:t|\$t|i18n\.global\.t)$/.test(node.expression.getText(ast)) || scopes.has(node.expression.getText(ast)))) {
      function literalKeys(key: ts.Node | undefined): string[] {
        if (!key) return []
        if (ts.isStringLiteral(key) || ts.isNoSubstitutionTemplateLiteral(key)) return [key.text]
        if (ts.isConditionalExpression(key)) return [...literalKeys(key.whenTrue), ...literalKeys(key.whenFalse)]
        return []
      }
      for (const key of literalKeys(node.arguments[0])) {
        const params = node.arguments[1]
        calls.push({ key, file, bundle: scopes.get(node.expression.getText(ast)), line: offset + ast.getLineAndCharacterOfPosition(node.getStart(ast)).line + 1,
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
      const scopes = localTranslationScopes([descriptor.script?.content, descriptor.scriptSetup?.content].filter(Boolean).join('\n'))
      for (const block of [descriptor.script, descriptor.scriptSetup]) if (block) calls.push(...scanExpression(block.content, file, block.loc.start.line - 1, scopes))
      if (descriptor.template) {
        function visit(node: any) {
          if (node.type === NodeTypes.INTERPOLATION) calls.push(...scanExpression(node.content.content, file, descriptor.template!.loc.start.line + node.loc.start.line - 2, scopes))
          for (const prop of node.props || []) if (prop.type === NodeTypes.DIRECTIVE && prop.exp) calls.push(...scanExpression(prop.exp.content, file, descriptor.template!.loc.start.line + prop.loc.start.line - 2, scopes))
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
    const bundles = call.bundle ? LOCAL_BUNDLES[call.bundle] : LOCALE_BUNDLES
    if (bundles && (call.bundle
      ? Object.values(bundles).every(bundle => typeof getLocaleValueAtPath(bundle, call.key) === 'string')
      : Object.values(bundles).some(bundle => typeof getLocaleValueAtPath(bundle, call.key) === 'string'))) continue
    byKey.set(call.key, [...byKey.get(call.key) || [], call])
  }
  return [...byKey].map(([key, calls]) => ({ key, calls }))
}
