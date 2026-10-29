<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import type { SourceRestriction } from '@/api/sourceRestriction';

defineProps<{ status: SourceRestriction | null; unavailable?: boolean; hasPending?: boolean }>();
const { t } = useI18n();
</script>

<template>
  <div v-if="status?.restricted" class="source-restriction-notice" role="status">
    <strong>{{ t(status.source_state === 'revoked' ? 'sourceProcessing.revoked' : 'sourceProcessing.held') }}</strong>
    <p>{{ t('sourceProcessing.historyUnverified') }}</p>
    <p>{{ t('sourceProcessing.nextAction') }}</p>
  </div>
  <div v-else-if="(unavailable || status?.source_state === 'unverified') && hasPending" class="source-restriction-notice" role="status">
    {{ t('sourceProcessing.unavailable') }}
  </div>
</template>

<style scoped>
.source-restriction-notice { margin: 12px 0; padding: 12px 16px; color: var(--td-warning-color); background: var(--td-warning-color-1); border-radius: 8px; }
p { margin: 4px 0 0; }
</style>
