import assert from 'node:assert/strict'
import { readdirSync, readFileSync } from 'node:fs'
import { join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'
import test from 'node:test'
import { auditVueVisibility } from './vueVisibilityAudit.mjs'

test('detects mutually exclusive v-show/v-if ancestors, including conjunctions', () => {
  const source = `<template><div v-show="tab === 'intent'"><div v-if="enabled && tab === 'fallback'">fallback</div></div></template>`
  const conflicts = auditVueVisibility(source)
  assert.equal(conflicts.length, 1)
  assert.equal(conflicts[0].ancestor.value, 'intent')
  assert.equal(conflicts[0].condition.value, 'fallback')
})

test('OR alternatives and equal constraints do not produce false conflicts', () => {
  assert.deepEqual(auditVueVisibility(`<template><div v-if="mode === 'remote' || enabled"><div v-if="mode === 'local'" /></div></template>`), [])
  assert.deepEqual(auditVueVisibility(`<template><div v-show="tab === 'system'"><div v-if="tab === 'system'" /></div><img src="test.png"></template>`), [])
})

test('all frontend Vue templates have no contradictory hidden ancestor constraints', () => {
  const root = fileURLToPath(new URL('../../src/', import.meta.url))
  const findings = []
  function walk(directory) {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      const path = join(directory, entry.name)
      if (entry.isDirectory()) walk(path)
      else if (path.endsWith('.vue')) findings.push(...auditVueVisibility(readFileSync(path, 'utf8'), relative(root, path)))
    }
  }
  walk(root)
  assert.deepEqual(findings, [], JSON.stringify(findings, null, 2))
})
