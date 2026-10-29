import { computed, onScopeDispose, ref, watch, type Ref } from 'vue';
import type { SourceRestriction } from '../api/sourceRestriction';

export function useSourceRestriction(
  kbId: Ref<string>, tenantId: Readonly<Ref<unknown>>,
  fetchStatus: (kbId: string) => Promise<SourceRestriction> = async (id) =>
    (await import('../api/sourceRestriction')).getSourceRestriction(id),
) {
  const status = ref<SourceRestriction | null>(null);
  const unavailable = ref(false);
  let generation = 0;
  const scope = () => `${String(tenantId.value)}:${kbId.value}`;
  const refresh = async () => {
    const request = ++generation;
    const expected = scope();
    if (!kbId.value || !tenantId.value) return;
    try {
      const result = await fetchStatus(kbId.value);
      if (request !== generation || scope() !== expected) return;
      status.value = result;
      unavailable.value = false;
    } catch {
      if (request !== generation || scope() !== expected) return;
      // A refresh failure does not invalidate a previously confirmed deny.
      // Scope changes clear the verdict in the watcher below.
      unavailable.value = true;
    }
  };
  const stop = watch([kbId, tenantId], () => {
    ++generation;
    status.value = null;
    unavailable.value = false;
    void refresh();
  }, { immediate: true });
  const timer = typeof window === 'undefined' ? undefined : setInterval(() => void refresh(), 15_000);
  onScopeDispose(() => {
    ++generation;
    stop();
    if (timer !== undefined) clearInterval(timer);
  });
  return { status, unavailable, restricted: computed(() => status.value?.restricted === true), refresh };
}
