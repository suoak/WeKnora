<template>
  <div>
    <div v-if="loading" role="status" :aria-label="t('common.loading')">
      <t-loading size="small" :text="t('common.loading')" />
    </div>
    <t-alert v-for="issue in issues" :key="issue.resource" theme="warning" :message="issue.message">
      <template #operation><t-button variant="text" :disabled="loading" @click="emit('retry')">{{ t('loadState.retry') }}</t-button></template>
    </t-alert>
    <t-alert v-if="emptyMessage && !loading && !issues.length" theme="info" :message="emptyMessage" />
    <t-alert v-if="failed" theme="error" :message="t('agentEditor.resourceState.panelFailed', { name: title })" role="alert">
      <template #operation><t-button variant="text" @click="retryRender">{{ t('loadState.retry') }}</t-button></template>
    </t-alert>
    <PanelContent v-else :key="revision"><slot /></PanelContent>
  </div>
</template>

<script setup lang="ts">
import { defineComponent, onErrorCaptured, ref } from 'vue'
import { useI18n } from 'vue-i18n'

withDefaults(defineProps<{
  title: string
  loading?: boolean
  issues?: Array<{ resource: string; message: string }>
  emptyMessage?: string
}>(), { loading: false, issues: () => [], emptyMessage: '' })
const emit = defineEmits<{ retry: [] }>()
const { t } = useI18n()
const failed = ref(false)
const revision = ref(0)
// Render the slot in a child so this boundary also catches errors in inline panel expressions.
const PanelContent = defineComponent({ setup: (_, { slots }) => () => slots.default?.() })
onErrorCaptured((error, _instance, info) => {
  console.error('Agent editor panel failed', info, error)
  failed.value = true
  return false
})
function retryRender() {
  failed.value = false
  revision.value++
  emit('retry')
}
</script>
