<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useI18n } from 'vue-i18n';
import { useAuthStore } from '@/stores/auth';
import { getKnowledgeDetails } from '@/api/knowledge-base';
import { formatReferenceSnippet } from '@/utils/referenceSources';
import { createVisibleSummarySession, type SummaryState } from '@/utils/visibleDocumentSummary';
import { personalDocumentMessages } from '@/i18n/personalDocumentPresentation';

const props = defineProps<{ kbId: string; paused?: boolean; item: {
  id: string; channel?: string; metadata_only?: boolean; parse_status: string;
  updated_at?: string; summary_status?: string; description?: string;
} }>();
const auth = useAuthStore();
const { t } = useI18n({ useScope: 'local', messages: personalDocumentMessages });
const anchor = ref<HTMLElement>();
const state = ref<SummaryState>({ status: 'idle', description: '' });
const summary = computed(() => formatReferenceSnippet(state.value.description));
const session = createVisibleSummarySession(
  (target, signal) => getKnowledgeDetails(target.id, { signal }),
  next => { state.value = next; },
);
watch(() => [auth.currentUserId, auth.currentTenantId, auth.currentTenantRole, props.kbId,
  props.item.id, props.item.updated_at, props.item.summary_status, props.item.description,
  props.item.parse_status, props.item.metadata_only, props.item.channel, props.paused], () => {
  session.target({ id: props.item.id, kbId: props.kbId,
    scope: JSON.stringify([auth.currentUserId, auth.currentTenantId, auth.currentTenantRole]),
    version: JSON.stringify([props.item.updated_at, props.item.summary_status, props.item.description]),
    eligible: !props.paused && props.item.channel === 'feishu_personal' && props.item.metadata_only === true &&
      props.item.parse_status === 'completed',
  });
}, { immediate: true, flush: 'sync' });
let observer: IntersectionObserver | undefined;
const supported = ref(true);
onMounted(() => {
  if (typeof IntersectionObserver !== 'undefined' && anchor.value) {
    observer = new IntersectionObserver(entries => {
      for (const entry of entries) session.visible(entry.isIntersecting);
    }, { threshold: 0.01 });
    observer.observe(anchor.value);
  } else {
    supported.value = false;
  }
});
function retry() {
  session.retry();
}
onBeforeUnmount(() => { observer?.disconnect(); session.dispose(); });
</script>

<template>
  <span ref="anchor" class="authorized-document-summary" :aria-busy="state.status === 'loading'">
    <template v-if="!supported">{{ t('openToRead') }}</template>
    <template v-else-if="state.status === 'ready'">{{ summary }}</template>
    <template v-else-if="state.status === 'empty'">{{ t('empty') }}</template>
    <template v-else-if="state.status === 'loading'">{{ t('loading') }}</template>
    <template v-else>
      {{ state.status === 'unavailable' ? t('unavailable') : t('waiting') }}
      <button type="button" class="summary-retry" @click.stop="retry">{{ t('retry') }}</button>
    </template>
  </span>
</template>

<style scoped>
.authorized-document-summary { display: block; min-height: 1.4em; }
.summary-retry { margin-left: 6px; padding: 0; border: 0; background: transparent;
  color: var(--td-brand-color); cursor: pointer; font: inherit; }
</style>
