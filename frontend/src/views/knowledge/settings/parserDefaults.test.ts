import assert from 'node:assert/strict'
import test from 'node:test'

import {
  excelFirstRowAsHeaderValue,
  pickDefaultParserEngineName,
} from './parserDefaults'

const engines = [
  { name: 'anydoc', available: true },
  { name: 'builtin', available: true },
]

test('Excel defaults to builtin even when AnyDoc is available', () => {
  assert.equal(pickDefaultParserEngineName(engines, ['xlsx', 'xls']), 'builtin')
  assert.equal(pickDefaultParserEngineName(engines, ['xlsx']), 'builtin')
  assert.equal(pickDefaultParserEngineName(engines, ['xlsm']), 'builtin')
  assert.equal(
    pickDefaultParserEngineName([
      { name: 'anydoc', available: true },
      { name: 'builtin', available: false },
    ], ['xlsx']),
    'builtin',
  )
})

test('non-Excel complex formats retain the existing AnyDoc preference', () => {
  assert.equal(pickDefaultParserEngineName(engines, ['docx', 'doc']), 'anydoc')
})

test('Excel header mode defaults true but preserves explicit false', () => {
  assert.equal(excelFirstRowAsHeaderValue(undefined), true)
  assert.equal(excelFirstRowAsHeaderValue(true), true)
  assert.equal(excelFirstRowAsHeaderValue(false), false)
})
