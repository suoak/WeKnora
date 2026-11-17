<script setup lang="ts">
import { isEncryptedParseFailure } from '@/utils/knowledgeParseFailure';

defineProps<{
  item: { parse_status?: string; metadata?: Record<string, unknown> };
}>();
</script>

<template>
  <div v-if="isEncryptedParseFailure(item)" class="knowledge-parse-failure-notice" role="note">
    <strong>{{ $t('knowledgeBase.encryptedDocumentReason') }}</strong>
    <span>{{ $t('knowledgeBase.encryptedDocumentAction') }}</span>
  </div>
</template>

<style scoped>
.knowledge-parse-failure-notice {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
  color: var(--td-error-color);
  font-size: 12px;
  line-height: 1.6;
  white-space: normal;
  overflow-wrap: anywhere;
}
.knowledge-parse-failure-notice strong { font-weight: 500; }
.knowledge-parse-failure-notice span { color: var(--td-text-color-secondary); }
</style>
