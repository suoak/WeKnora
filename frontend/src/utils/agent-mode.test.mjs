import assert from 'node:assert/strict'
import test from 'node:test'

const BUILTIN_QUICK_ANSWER_ID = 'builtin-quick-answer'
const BUILTIN_SMART_REASONING_ID = 'builtin-smart-reasoning'

function isQuickAnswerAgentId(agentId) {
  return (agentId || BUILTIN_QUICK_ANSWER_ID) === BUILTIN_QUICK_ANSWER_ID
}

function isAgentStreamAgentId(agentId, isAgentEnabled) {
  const id = agentId || BUILTIN_QUICK_ANSWER_ID
  if (id === BUILTIN_QUICK_ANSWER_ID) return false
  if (id === BUILTIN_SMART_REASONING_ID) return true
  return isAgentEnabled
}

function reconcileBuiltinAgentMode(settings) {
  const agentId = settings.selectedAgentId
  if (!agentId || agentId === BUILTIN_QUICK_ANSWER_ID) {
    settings.selectedAgentId = BUILTIN_SMART_REASONING_ID
    settings.isAgentEnabled = true
    return true
  }
  if (agentId === BUILTIN_SMART_REASONING_ID && !settings.isAgentEnabled) {
    settings.isAgentEnabled = true
    return true
  }
  return false
}

test('isQuickAnswerAgentId treats builtin quick-answer as RAG mode', () => {
  assert.equal(isQuickAnswerAgentId('builtin-quick-answer'), true)
  assert.equal(isQuickAnswerAgentId(undefined), true)
  assert.equal(isQuickAnswerAgentId('builtin-smart-reasoning'), false)
})

test('isAgentStreamAgentId prefers selectedAgentId for builtins', () => {
  assert.equal(
    isAgentStreamAgentId('builtin-quick-answer', true),
    false,
    'stale isAgentEnabled must not flip quick-answer into agent stream',
  )
  assert.equal(isAgentStreamAgentId('builtin-smart-reasoning', false), true)
  assert.equal(isAgentStreamAgentId('custom-agent', true), true)
  assert.equal(isAgentStreamAgentId('custom-agent', false), false)
})

test('reconcileBuiltinAgentMode retires persisted quick-answer selections', () => {
  const quick = { selectedAgentId: 'builtin-quick-answer', isAgentEnabled: true }
  assert.equal(reconcileBuiltinAgentMode(quick), true)
  assert.equal(quick.selectedAgentId, BUILTIN_SMART_REASONING_ID)
  assert.equal(quick.isAgentEnabled, true)

  const missing = { isAgentEnabled: false }
  assert.equal(reconcileBuiltinAgentMode(missing), true)
  assert.equal(missing.selectedAgentId, BUILTIN_SMART_REASONING_ID)
  assert.equal(missing.isAgentEnabled, true)

  const reasoning = { selectedAgentId: 'builtin-smart-reasoning', isAgentEnabled: false }
  assert.equal(reconcileBuiltinAgentMode(reasoning), true)
  assert.equal(reasoning.isAgentEnabled, true)

  const custom = { selectedAgentId: 'custom-agent', isAgentEnabled: true }
  assert.equal(reconcileBuiltinAgentMode(custom), false)
})
