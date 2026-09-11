<template>
  <section v-if="summary.total > 0" class="processing-summary" aria-live="polite">
    <div class="processing-summary__intro">
      <span class="processing-summary__title">{{ $t('knowledgeBase.processingSummary.title', { total: summary.total }) }}</span>
      <span class="processing-summary__hint">{{ $t('knowledgeBase.processingSummary.hint') }}</span>
    </div>
    <div class="processing-summary__counts">
      <span class="processing-summary__count is-completed">
        <i aria-hidden="true" />{{ $t('knowledgeBase.processingSummary.completed', { count: summary.completed }) }}
      </span>
      <span class="processing-summary__count is-processing">
        <i aria-hidden="true" />{{ $t('knowledgeBase.processingSummary.processing', { count: summary.processing }) }}
      </span>
      <span class="processing-summary__count is-failed">
        <i aria-hidden="true" />{{ $t('knowledgeBase.processingSummary.failed', { count: summary.failed }) }}
      </span>
      <span v-if="summary.cancelled" class="processing-summary__count is-cancelled">
        <i aria-hidden="true" />{{ $t('knowledgeBase.processingSummary.cancelled', { count: summary.cancelled }) }}
      </span>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'

import { summarizeKnowledgeProcessing, type KnowledgeProcessingItem } from '@/utils/knowledgeProcessingPresentation'

const props = defineProps<{ items: KnowledgeProcessingItem[] }>()
const summary = computed(() => summarizeKnowledgeProcessing(props.items))
</script>

<style scoped lang="less">
.processing-summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin: 0 0 12px;
  padding: 10px 12px;
  border: 1px solid var(--td-component-stroke);
  border-radius: 8px;
  background: var(--td-bg-color-secondarycontainer);
}

.processing-summary__intro {
  min-width: 0;
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.processing-summary__title {
  color: var(--td-text-color-primary);
  font-size: 13px;
  font-weight: 600;
  white-space: nowrap;
}

.processing-summary__hint {
  overflow: hidden;
  color: var(--td-text-color-secondary);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.processing-summary__counts {
  display: flex;
  align-items: center;
  gap: 14px;
  flex-shrink: 0;
}

.processing-summary__count {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--td-text-color-secondary);
  font-size: 12px;
  white-space: nowrap;

  i {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--td-text-color-placeholder);
  }

  &.is-completed i { background: var(--td-success-color); }
  &.is-processing i { background: var(--td-brand-color); }
  &.is-failed i { background: var(--td-error-color); }
  &.is-cancelled i { background: var(--td-text-color-placeholder); }
}

@media (max-width: 768px) {
  .processing-summary {
    align-items: flex-start;
    flex-direction: column;
    gap: 8px;
  }

  .processing-summary__hint { display: none; }
  .processing-summary__counts { width: 100%; justify-content: space-between; gap: 8px; }
}
</style>
