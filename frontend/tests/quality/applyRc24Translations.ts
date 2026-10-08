// Targeted insertion: never regenerate or replace an existing locale value.
import ts from 'typescript'
import { readFileSync, writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { escapeLocaleString, serializeLocaleValue } from '../../src/i18n/localeSerialize'
const names = ['en-US', 'zh-CN', 'ja-JP', 'ko-KR', 'ru-RU']
const entries: Record<string,string[]> = JSON.parse(readFileSync(fileURLToPath(new URL('./rc24-translations.json', import.meta.url)), 'utf8'))
for (const [index, locale] of names.entries()) {
  const file = fileURLToPath(new URL(`../../src/i18n/locales/${locale}.ts`, import.meta.url))
  const source = readFileSync(file, 'utf8').replace(/(\w+): enUS\.\1,/g, (match,name) => Object.keys(entries).some(key => key.startsWith(name+'.')) ? `${name}: { ...enUS.${name} },` : match)
  const ast = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true)
  const exported = ast.statements.find(ts.isExportAssignment)!
  let expression = exported.expression
  if (ts.isIdentifier(expression)) {
    const name = expression.text
    expression = ast.statements.filter(ts.isVariableStatement).flatMap(s => [...s.declarationList.declarations]).find(d => d.name.getText(ast) === name)!.initializer!
  }
  if (!ts.isObjectLiteralExpression(expression)) throw new Error(`Unsupported locale root: ${locale}`)
  const root = expression
  const patches = new Map<number, Record<string,any>>()
  for (const [path, values] of Object.entries(entries)) {
    const parts = path.split('.');let object = root
    for (let i = 0; i < parts.length; i++) {
      const property = object.properties.find(p => p.name && (ts.isIdentifier(p.name) || ts.isStringLiteral(p.name)) && p.name.text === parts[i]) as ts.PropertyAssignment | undefined
      if (property) {
        if (i === parts.length - 1) {
          if (!ts.isStringLiteral(property.initializer) || property.initializer.text !== values[index]) throw new Error(`Refusing to overwrite ${locale}:${path}`)
          break // Idempotent resume after interrupted generation.
        }
        if (!ts.isObjectLiteralExpression(property.initializer)) throw new Error(`Invalid parent: ${path}`)
        object = property.initializer;continue
      }
      const position = object.getEnd() - 1
      const tree = patches.get(position) || {};let node = tree
      for (let j = i; j < parts.length - 1; j++) node = node[parts[j]] ||= {}
      node[parts.at(-1)!] = values[index];patches.set(position, tree);break
    }
  }
  let result = source
  for (const [position, tree] of [...patches].sort((a,b) => b[0]-a[0])) {
    const before = source.slice(0, position).trimEnd()
    const prefix = before.endsWith(',') || before.endsWith('{') ? '' : ','
    const content = Object.entries(tree).map(([key,value]) => `  ${key}: ${typeof value === 'string' ? escapeLocaleString(value) : serializeLocaleValue(value, 1)},`).join('\n')
    result = result.slice(0,position) + prefix + '\n' + content + '\n' + result.slice(position)
  }
  writeFileSync(file, result.replace(/\n[ \t]+\n/g, '\n\n'), 'utf8')
}
console.log(`Added ${Object.keys(entries).length} keys in each of five locales`)
