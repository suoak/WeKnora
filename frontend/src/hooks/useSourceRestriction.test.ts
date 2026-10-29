import assert from 'node:assert/strict';
import { test } from 'node:test';
import { effectScope, nextTick, ref } from 'vue';
import { useSourceRestriction } from './useSourceRestriction.ts';
import type { SourceRestriction } from '../api/sourceRestriction.ts';
import { isSourceBlockedProcessing } from '../utils/sourceRestriction.ts';

const held: SourceRestriction = { restricted: true, source_state: 'hold', historical_processing_state: 'unverified' };
const available: SourceRestriction = { ...held, restricted: false, source_state: 'available' };

test('out of order responses and tenant switches cannot reuse a previous source verdict', async () => {
  const requests: { id: string; resolve: (value: SourceRestriction) => void }[] = [];
  const kb = ref('first'); const tenant = ref(1); const scope = effectScope();
  const state = scope.run(() => useSourceRestriction(kb, tenant, (id) => new Promise(resolve => requests.push({ id, resolve }))))!;
  kb.value = 'second'; await nextTick();
  requests[1].resolve(available); await nextTick();
  requests[0].resolve(held); await nextTick();
  assert.equal(state.status.value?.source_state, 'available');
  tenant.value = 2; await nextTick();
  assert.equal(state.status.value, null);
  requests[2].resolve(held); await nextTick();
  assert.equal(state.restricted.value, true);
  const pending = state.refresh();
  scope.stop(); requests[3].resolve(available); await pending;
  assert.equal(state.restricted.value, true, 'disposed requests must not change state');
});

test('refresh errors retain a confirmed restriction and never claim an execution failed', async () => {
  const scope = effectScope(); let fail = false;
  const state = scope.run(() => useSourceRestriction(ref('kb'), ref(1), async () => {
    if (fail) throw new Error('network');
    return held;
  }))!;
  await nextTick(); fail = true; await state.refresh();
  assert.equal(state.restricted.value, true);
  assert.equal(state.unavailable.value, true);
  assert.equal(state.status.value?.historical_processing_state, 'unverified');
  scope.stop();
});

test('source admission does not rewrite completed or failed task outcomes', () => {
  for (const status of ['pending', 'processing', 'finalizing']) {
    assert.equal(isSourceBlockedProcessing(status, true), true);
    assert.equal(isSourceBlockedProcessing(status, false), false);
  }
  for (const status of ['completed', 'failed', 'cancelled', undefined]) {
    assert.equal(isSourceBlockedProcessing(status, true), false);
  }
});
