import test from 'node:test'
import assert from 'node:assert/strict'
import { withPersonalMCPBuiltinCapabilities } from './personalMcpCapabilities.ts'

test('empty and old Personal capability selections retain mandatory tools without altering optional access', () => {
  assert.deepEqual(withPersonalMCPBuiltinCapabilities(), ['retrieve', 'add_content'])
  const input = ['enterprise_retrieve', 'retrieve'] as const
  const output = withPersonalMCPBuiltinCapabilities(input)
  assert.deepEqual(output, ['enterprise_retrieve', 'retrieve', 'add_content'])
  assert.deepEqual(input, ['enterprise_retrieve', 'retrieve'])
  assert.deepEqual(withPersonalMCPBuiltinCapabilities(output), output)
  assert.deepEqual(withPersonalMCPBuiltinCapabilities(['chat', 'read_agents']), ['chat', 'read_agents', 'retrieve', 'add_content'])
})
