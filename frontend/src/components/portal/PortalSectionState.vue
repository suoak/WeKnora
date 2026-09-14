<template>
  <div class="portal-section-state" :class="{ compact }" role="status">
    <t-icon name="error-circle" />
    <span>{{ message || t('loadState.genericTitle') }}</span>
    <button type="button" @click="$emit('retry')">{{ t('loadState.retry') }}</button>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

withDefaults(defineProps<{ message?: string; compact?: boolean }>(), {
  message: '',
  compact: false,
})
defineEmits<{ retry: [] }>()
const { t } = useI18n()
</script>

<style scoped lang="less">
.portal-section-state{min-height:116px;display:flex;align-items:center;justify-content:center;gap:8px;border:1px dashed var(--td-component-border);border-radius:10px;background:var(--td-bg-color-container);color:var(--td-text-color-secondary);font-size:12px}.portal-section-state>.t-icon{color:var(--td-text-color-placeholder);font-size:16px}.portal-section-state button{padding:4px 8px;border:0;border-radius:6px;background:transparent;color:var(--td-brand-color);cursor:pointer;font:inherit}.portal-section-state button:hover{background:var(--td-brand-color-light)}.portal-section-state button:focus-visible{outline:2px solid var(--td-brand-color-focus);outline-offset:1px}.portal-section-state.compact{min-height:48px;justify-content:flex-start;padding:0 12px}
</style>
