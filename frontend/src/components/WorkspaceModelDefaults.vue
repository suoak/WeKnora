<template>
  <section class="workspace-model-defaults">
    <h3>{{ t('workspaceDefaults.title') }}</h3>
    <p>{{ t('workspaceDefaults.description') }}</p>
    <p v-if="failed" role="alert">{{ t('workspaceDefaults.loadFailed') }}
      <t-button variant="text" @click="load">{{ t('common.retry') }}</t-button>
    </p>
    <div class="slots">
      <label v-for="row in rows" :key="row.key">
        <span>{{ t(`modelSettings.defaultPolicy.${row.role}`) }}</span>
        <t-select v-model="draft[row.key]" :disabled="!canEdit || !ready || saving"
          :options="options(row.type)" :placeholder="t('workspaceDefaults.inherit')" clearable />
        <span v-if="draft[row.key] && !options(row.type).some(m => m.value === draft[row.key])" role="status">
          {{ t('modelSettings.defaultPolicy.invalidConfigured') }}
        </span>
      </label>
    </div>
    <p>{{ t('modelSettings.defaultPolicy.embeddingSafety') }}</p>
    <t-button v-if="canEdit" :disabled="!ready" :loading="saving" @click="save">{{ t('common.save') }}</t-button>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { useAuthStore } from '@/stores/auth'
import type { ModelConfig } from '@/api/model'
import { emptyWorkspaceDefaults, getWorkspaceModelDefaults, saveWorkspaceModelDefaults, type WorkspaceModelDefaults } from '@/api/model/workspaceDefaults'

const props = defineProps<{ models: ModelConfig[] }>()
const emit = defineEmits<{ saved: [] }>()
const auth = useAuthStore()
const { t } = useI18n()
const ready = ref(false), failed = ref(false), saving = ref(false)
const draft = ref(emptyWorkspaceDefaults()), previous = ref(emptyWorkspaceDefaults())
const canEdit = computed(() => auth.isSystemAdmin || auth.hasRole('admin'))
const rows: Array<{ key: keyof WorkspaceModelDefaults; role: string; type: ModelConfig['type'] }> = [
  { key: 'chat_model_id', role: 'chat', type: 'KnowledgeQA' },
  { key: 'utility_model_id', role: 'summary', type: 'KnowledgeQA' },
  { key: 'embedding_model_id', role: 'embedding', type: 'Embedding' },
  { key: 'rerank_model_id', role: 'rerank', type: 'Rerank' },
  { key: 'vlm_model_id', role: 'vlm', type: 'VLLM' },
  { key: 'asr_model_id', role: 'asr', type: 'ASR' },
]
const options = (type: ModelConfig['type']) => props.models
  .filter(m => m.status === 'active' && (m.type === type || (type === 'VLLM' && m.type === 'KnowledgeQA' && m.parameters?.supports_vision)))
  .map(m => ({ label: m.display_name || m.name, value: m.id! }))
let generation = 0
async function load() {
  const current = ++generation
  ready.value = false
  failed.value = false
  draft.value = emptyWorkspaceDefaults()
  try {
    const result = await getWorkspaceModelDefaults()
    if (current !== generation) return
    previous.value = { ...result.data }
    draft.value = { ...result.data }
    ready.value = true
  } catch {
    if (current === generation) failed.value = true
  }
}
async function save() {
  if (!ready.value || !canEdit.value || saving.value) return
  const current = generation
  saving.value = true
  try {
    const normalized = Object.fromEntries(Object.entries(draft.value).map(([key, value]) => [key, value || ''])) as WorkspaceModelDefaults
    const result = await saveWorkspaceModelDefaults(normalized, previous.value)
    if (current !== generation) return
    previous.value = { ...result.data }
    draft.value = { ...result.data }
    emit('saved')
    MessagePlugin.success(t('modelSettings.defaultPolicy.saved'))
  } catch (error: any) {
    if (current === generation) MessagePlugin.error(error?.message || t('modelSettings.defaultPolicy.saveFailed'))
  } finally { saving.value = false }
}
watch(() => auth.currentTenantId, load, { immediate: true })
</script>

<style scoped>
.workspace-model-defaults { padding: 20px; margin: 16px 0; border: 1px solid var(--td-component-border); border-radius: 8px; }
.slots { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 16px; }
label { display: grid; gap: 8px; }
p { color: var(--td-text-color-secondary); }
[role='status'], [role='alert'] { color: var(--td-warning-color); }
</style>
