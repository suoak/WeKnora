import { parse as parseSfc } from '@vue/compiler-sfc'
import { parse as parseTemplate } from '@vue/compiler-dom'
import ts from 'typescript'

// Only infer mandatory equality constraints: an OR branch cannot constrain its siblings.
function constraints(expression) {
  const ast = ts.createSourceFile('condition.ts', `(${expression})`, ts.ScriptTarget.Latest, true)
  function read(node) {
    if (ts.isParenthesizedExpression(node)) return read(node.expression)
    if (!ts.isBinaryExpression(node)) return []
    if (node.operatorToken.kind === ts.SyntaxKind.AmpersandAmpersandToken) return [...read(node.left), ...read(node.right)]
    if (![ts.SyntaxKind.EqualsEqualsEqualsToken, ts.SyntaxKind.EqualsEqualsToken].includes(node.operatorToken.kind)) return []
    if (!ts.isStringLiteral(node.right) && !ts.isNumericLiteral(node.right)) return []
    const variable = node.left.getText(ast)
    if (!/^[\w.]+$/.test(variable)) return []
    return [{ variable, value: node.right.text }]
  }
  return ast.statements[0] && ts.isExpressionStatement(ast.statements[0]) ? read(ast.statements[0].expression) : []
}

export function auditVueVisibility(source, file = 'component.vue') {
  const { descriptor, errors } = parseSfc(source, { filename: file })
  if (errors.length) throw new Error(`${file}: ${errors.join('\n')}`)
  if (!descriptor.template) return []
  const conflicts = []
  function visit(node, ancestors = []) {
    const conditions = node.type === 1 ? node.props.filter(p => p.type === 7 && ['if', 'show'].includes(p.name) && p.exp)
      .flatMap(p => constraints(p.exp.content).map(c => ({ ...c, expression: p.exp.content, line: descriptor.template.loc.start.line + node.loc.start.line - 1 }))) : []
    for (const condition of conditions) for (const ancestor of ancestors) {
      if (condition.variable === ancestor.variable && condition.value !== ancestor.value) conflicts.push({ file, ancestor, condition })
    }
    for (const child of node.children || []) visit(child, [...ancestors, ...conditions])
  }
  visit(parseTemplate(descriptor.template.content))
  return conflicts
}
